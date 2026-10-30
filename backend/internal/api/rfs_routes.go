package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   F23 — Aktivasi/EWO (item_type = rfs): cancel & delete ber-alasan.
   --------------------------------------------------------------------------- */

type rfsCancelRequest struct {
	Reason string `json:"reason"`
}

type rfsDeleteRequest struct {
	Reason string `json:"reason"`
}

// rfsCloseRequest adalah body POST /api/items/{id}/rfs-close.
type rfsCloseRequest struct {
	// Reason opsional; dicatat pada event + audit penutupan.
	Reason string `json:"reason"`
}

// rfsStartRequest adalah body POST /api/items/{id}/rfs-start.
type rfsStartRequest struct {
	// Note opsional; dicatat pada event mulai dikerjakan.
	Note string `json:"note"`
	// Reason wajib bila jadwal RFS (expire_at) belum tiba: aktivasi lebih awal
	// adalah pengecualian yang harus dipertanggungjawabkan (mis. pelanggan
	// meminta layanan aktif sebelum tenggat).
	Reason string `json:"reason"`
}

// rfsSchedulePending melaporkan apakah RFS belum mencapai jadwal (expire_at).
//
// Kebijakan produk: jadwal BUKAN penghalang mutlak. RFS boleh dikerjakan
// lebih awal (mis. pelanggan minta aktivasi sebelum tenggat) asalkan alasan
// diisi; alasan itu dicatat pada event + audit. Fungsi ini hanya memberi tahu
// bahwa pengecualian diperlukan, dan mengembalikan jadwal dalam zona WIB.
//
// Status di luar state awal (sudah dikerjakan) tidak pernah dianggap "pending"
// agar pekerjaan yang berjalan tidak tersangkut bila jadwal diubah.
func rfsSchedulePending(item *models.WorkItem, now time.Time) (time.Time, bool) {
	if item == nil || item.ItemType != models.ItemRFS {
		return time.Time{}, false
	}
	// `planned` adalah status legacy (sebelum migrasi 00032) yang tetap
	// diperlakukan sebagai "belum mulai".
	if item.Status != "new" && item.Status != "planned" {
		return time.Time{}, false
	}
	if item.ExpireAt == nil {
		return time.Time{}, false
	}
	if !now.Before(*item.ExpireAt) {
		return time.Time{}, false
	}
	return item.ExpireAt.In(wibLocation), true
}

// rfsEarlyStartMessage menyusun pesan saat jadwal belum tiba dan alasan belum
// diisi. Pesan menegaskan bahwa aktivasi lebih awal TETAP BOLEH, asal beralasan.
func rfsEarlyStartMessage(at time.Time) string {
	return "RFS dijadwalkan " + at.Format("02 Jan 2006 15:04 MST") +
		". Aktivasi lebih awal tetap dapat dilakukan — isi alasan lebih dulu " +
		"(mis. permintaan pelanggan) agar tercatat pada riwayat"
}

// handleRFSStart memulai pengerjaan Aktivasi/EWO: status `new` menjadi
// `in_progress` (bukan langsung activated).
//
// POST /api/items/{id}/rfs-start
//
// Jadwal bukan penghalang: bila expire_at belum tiba, pengerjaan lebih awal
// tetap diizinkan asalkan `reason` diisi (mis. pelanggan minta aktivasi lebih
// cepat). Alasan tersebut dicatat pada event + audit sebagai pengecualian,
// sehingga status "in progress sebelum jadwal" tidak pernah terjadi diam-diam.
//
// Idempoten: RFS yang sudah in_progress/in_progress_field dikembalikan apa
// adanya tanpa mengubah apa pun.
func (s *Server) handleRFSStart(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	var req rfsStartRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if item.ItemType != models.ItemRFS {
		writeErr(w, http.StatusBadRequest, "aksi ini hanya berlaku untuk Aktivasi/EWO (RFS)")
		return
	}
	if !s.canModifyItem(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak memulai RFS ini")
		return
	}
	// Sudah dikerjakan: tidak ada yang perlu diubah.
	if item.Status == "in_progress" || item.Status == "in_progress_field" {
		writeJSON(w, http.StatusOK, item)
		return
	}
	if item.Status == "activated" || item.Status == "cancelled" {
		writeErr(w, http.StatusConflict, "RFS sudah ditutup atau dibatalkan")
		return
	}

	now := time.Now().UTC()
	reason := strings.TrimSpace(req.Reason)
	// Jadwal belum tiba: boleh lanjut, tetapi alasan wajib agar pengecualian
	// tercatat. Tanpa alasan → 400 dengan pesan yang menjelaskan pilihannya.
	early := false
	if at, pending := rfsSchedulePending(item, now); pending {
		if reason == "" {
			writeErr(w, http.StatusBadRequest, rfsEarlyStartMessage(at))
			return
		}
		early = true
	}

	wf := workitems.WorkflowFor(models.ItemRFS)
	if wf == nil || !wf.CanTransition(item.Status, "in_progress") {
		writeErr(w, http.StatusConflict,
			"transisi status dari "+item.Status+" ke in_progress tidak diizinkan")
		return
	}

	actor := currentUsername(r)
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "Mulai dikerjakan"
	}

	fields := map[string]any{
		"status":              "in_progress",
		"updated_by_username": actor,
	}
	if item.FirstResponseAt == nil && item.Status != wf.InitialState {
		fields["first_response_at"] = now
	}

	detail := map[string]any{"note": note}
	if early {
		// Tandai eksplisit agar laporan/riwayat dapat membedakan pengerjaan
		// sebelum jadwal dari pengerjaan normal.
		detail["early_start"] = true
		detail["reason"] = reason
		if item.ExpireAt != nil {
			detail["schedule"] = item.ExpireAt.In(wibLocation).Format(time.RFC3339)
		}
	}

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, fields); err != nil {
			return err
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventStatusChanged,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    "in_progress",
			Detail:     detail,
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	auditDetail := map[string]any{
		"ref_no": item.RefNo, "from": item.Status, "to": "in_progress", "note": note,
	}
	if early {
		auditDetail["early_start"] = true
		auditDetail["reason"] = reason
	}
	s.audit(r, actor, "rfs.start", "work_item", id.String(), auditDetail, true)
	s.syncSheet(r, item, "status_changed")
	s.maybeSendRFSSummary(r.Context(), now)

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// canCancelRFS melaporkan apakah user boleh membatalkan RFS.
//
// Aturan: admin (super) & manager (tim admin) selalu boleh; role sales hanya
// boleh membatalkan RFS yang ia buat sendiri.
func (s *Server) canCancelRFS(r *http.Request, item *models.WorkItem) bool {
	u := userFrom(r)
	if u == nil || item == nil {
		return false
	}
	if u.Role == models.RoleAdmin || s.store.RoleIsSuper(r.Context(), u.Role) {
		return true
	}
	if u.Role == models.RoleManager {
		return true
	}
	// Sales: hanya pembuat RFS tersebut.
	if u.Role == models.RoleSales {
		return strings.EqualFold(strings.TrimSpace(item.CreatedBy), strings.TrimSpace(u.Username))
	}
	return false
}

// handleRFSCancel membatalkan RFS (status cancelled, tahap cancelled).
//
// POST /api/items/{id}/rfs-cancel
func (s *Server) handleRFSCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	var req rfsCancelRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if item.ItemType != models.ItemRFS {
		writeErr(w, http.StatusBadRequest, "aksi ini hanya berlaku untuk Aktivasi/EWO (RFS)")
		return
	}
	if !s.canCancelRFS(r, item) {
		writeErr(w, http.StatusForbidden, "hanya admin/manager atau sales pembuat yang dapat membatalkan RFS")
		return
	}
	if item.Status == "cancelled" {
		writeErr(w, http.StatusConflict, "RFS sudah dibatalkan")
		return
	}

	actor := currentUsername(r)
	reason := strings.TrimSpace(req.Reason)
	stage := models.RFSStageCancelled

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, map[string]any{
			"status":              "cancelled",
			"updated_by_username": actor,
		}); err != nil {
			return err
		}
		if err := s.store.UpdateRFSDetails(r.Context(), tx, id, repository.UpdateRFSDetailsParams{InstallStage: &stage}); err != nil {
			return err
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventCancelled,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    "cancelled",
			Detail:     map[string]any{"note": reason},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "rfs.cancel", "work_item", id.String(), map[string]any{
		"ref_no": item.RefNo, "reason": reason,
	}, true)
	s.syncSheet(r, item, "status_changed")

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleRFSClose menutup Aktivasi/EWO: status menjadi `activated` (Closed /
// Completed) DAN tahap instalasi menjadi `activated`, dalam satu transaksi.
//
// POST /api/items/{id}/rfs-close
//
// Kenapa endpoint khusus: tombol "Close Ticket" dulu melakukan PATCH tahap lalu
// POST /status. Dengan workflow lama, transisi langsung ke activated dari
// status awal tidak sah menurut workflow sehingga status tetap di status awal
// walau tahap instalasi sudah activated (UI menampilkan "Closed / Completed"
// dari tahap). Endpoint ini menutup lewat jalur sah: new → in_progress
// → activated, lalu memaksa status akhir agar konsisten.
func (s *Server) handleRFSClose(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	var req rfsCloseRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if item.ItemType != models.ItemRFS {
		writeErr(w, http.StatusBadRequest, "aksi ini hanya berlaku untuk Aktivasi/EWO (RFS)")
		return
	}
	// Izin: admin/super, pembuat/owner, atau penangan tambahan (ikut menangani).
	if !s.canModifyItem(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak menutup RFS ini")
		return
	}
	if item.Status == "activated" && item.RFS != nil && item.RFS.InstallStage == models.RFSStageActivated {
		writeJSON(w, http.StatusOK, item)
		return
	}
	if item.Status == "cancelled" {
		writeErr(w, http.StatusConflict, "RFS sudah dibatalkan")
		return
	}

	actor := currentUsername(r)
	now := time.Now().UTC()
	stage := models.RFSStageActivated
	reason := strings.TrimSpace(req.Reason)

	wf := workitems.WorkflowFor(models.ItemRFS)
	from := item.Status

	fields := map[string]any{
		"status":              "activated",
		"updated_by_username": actor,
	}
	if item.ClosedAt == nil {
		fields["closed_at"] = now
	}
	if item.ResolvedAt == nil {
		fields["resolved_at"] = now
	}
	if item.FirstResponseAt == nil && wf != nil && from != wf.InitialState {
		fields["first_response_at"] = now
	}

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, fields); err != nil {
			return err
		}
		// Item legacy mungkin belum punya baris rfs_details; buat bila perlu
		// agar tahap instalasi tetap tersimpan.
		if item.RFS == nil {
			if err := s.store.CreateRFSDetails(r.Context(), tx, id, repository.CreateRFSDetailsParams{
				CustomerName: item.Title,
				InstallStage: models.RFSStageActivated,
			}); err != nil {
				return err
			}
		} else if err := s.store.UpdateRFSDetails(r.Context(), tx, id,
			repository.UpdateRFSDetailsParams{InstallStage: &stage}); err != nil {
			return err
		}
		// Satu event penutupan; nilai "from" mencatat status asal (mis. planned)
		// agar riwayat tetap jujur walau status dilompati.
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventClosed,
			Actor:      actor,
			FromValue:  from,
			ToValue:    "activated",
			Detail: map[string]any{
				"note":          reason,
				"install_stage": models.RFSStageActivated,
			},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, actor, "rfs.close", "work_item", id.String(), map[string]any{
		"ref_no": item.RefNo, "from": from, "to": "activated", "reason": reason,
	}, true)
	s.syncSheet(r, item, "status_changed")
	s.maybeSendRFSSummary(r.Context(), now)

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleRFSDelete menghapus RFS dengan alasan wajib.
//
// POST /api/items/{id}/rfs-delete
//
// Izin: admin (force delete) atau pembuat/owner RFS. Alasan disimpan pada audit
// + event agar penghapusan tetap dapat ditelusuri.
func (s *Server) handleRFSDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	var req rfsDeleteRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = strings.TrimSpace(r.URL.Query().Get("reason"))
	}
	if reason == "" {
		writeErr(w, http.StatusBadRequest, "alasan penghapusan wajib diisi")
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if item.ItemType != models.ItemRFS {
		writeErr(w, http.StatusBadRequest, "aksi ini hanya berlaku untuk Aktivasi/EWO (RFS)")
		return
	}

	u := userFrom(r)
	isSuper := u != nil && (u.Role == models.RoleAdmin || s.store.RoleIsSuper(r.Context(), u.Role))
	if !isSuper && !canManageItem(u, item) {
		writeErr(w, http.StatusForbidden, "hanya admin atau pemilik/pembuat RFS yang dapat menghapus")
		return
	}

	// Catat event alasan sebelum soft-delete.
	actor := currentUsername(r)
	_ = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  "deleted",
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    item.Status,
			Detail:     map[string]any{"reason": reason, "forced": isSuper},
		})
	})

	deleted, err := s.store.SoftDeleteWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	s.audit(r, "", "rfs.delete", "work_item", id.String(), map[string]any{
		"ref_no": item.RefNo, "reason": reason, "deleted_total": deleted, "forced": isSuper,
	}, true)
	s.syncSheetDeleted(r, item)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "reason": reason})
}
