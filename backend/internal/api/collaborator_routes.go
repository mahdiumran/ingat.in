package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   F20 — Assign & penangan tambahan ("ikut menangani")
   --------------------------------------------------------------------------- */

// assignRequest adalah body POST /api/items/{id}/assign.
type assignRequest struct {
	OwnerUsername string `json:"owner_username"`
	// F25: alasan wajib saat admin mengganti owner yang sudah terisi (force).
	Reason string `json:"reason"`
	// Force menandai penggantian paksa owner yang sudah terisi (admin).
	Force bool `json:"force"`
}

// collaboratorRequest adalah body POST /api/items/{id}/collaborators.
type collaboratorRequest struct {
	Username string `json:"username"`
}

// ticketClaimStatus menentukan status berikutnya saat sebuah tiket diambil
// (self-claim) dan belum punya owner.
//
// Aturan (hanya untuk transisi yang sah menurut workflow):
//   - new/assigned (incident & request) -> in_progress.
//   - scheduled (change) -> implementing. Draft/review/approved tidak melompat
//     karena transisinya tidak diizinkan workflow.
//   - pending_customer/resolved (incident & request) -> in_progress, karena
//     tiket tanpa owner yang diambil kembali harus jalan lagi.
//   - new/postponed (rfs) -> in_progress: Aktivasi/EWO yang mulai dikerjakan
//     tidak boleh tertinggal di status awal.
//   - Status terminal tidak pernah dimajukan otomatis.
func ticketClaimStatus(itemType, current string) string {
	target := "in_progress"
	if itemType == models.ItemChange {
		target = "implementing"
	}
	// Hanya tipe yang punya perilaku "mengerjakan" otomatis.
	if itemType != models.ItemIncident && itemType != models.ItemRequest &&
		itemType != models.ItemChange && itemType != models.ItemRFS {
		return ""
	}
	// CanTransition menganggap from == to selalu sah; status yang sudah di
	// tujuan tidak perlu diubah.
	if current == target {
		return ""
	}
	wf := workitems.WorkflowFor(itemType)
	if wf == nil || !wf.CanTransition(current, target) {
		return ""
	}
	// Jangan pernah memicu transisi penutupan secara otomatis saat claim.
	if result := wf.EvaluateTransitions(current, target); result.IsClosing || result.IsReopen {
		return ""
	}
	return target
}

// handleAssignItem menetapkan owner (penanggung jawab utama) sebuah tiket.
//
// POST /api/items/{id}/assign
//
// F25 — owner lock:
//   - Owner masih kosong: siapa saja yang boleh menulis dapat mengambil
//     (self-claim) atau ditetapkan oleh admin/pembuat/owner.
//   - Owner sudah terisi: HANYA admin (super) yang boleh mengganti, dan
//     alasan wajib diisi. Non-admin tidak dapat mengganti/melepas owner.
func (s *Server) handleAssignItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadItemForAttachment(w, r)
	if !ok {
		return
	}
	if !canWriteRole(userFrom(r)) {
		writeErr(w, http.StatusForbidden, "viewer tidak dapat mengubah penanggung jawab")
		return
	}
	// Fix Item 6: self-claim tiket/RFS tanpa owner selalu boleh (lintas tim).
	// Guard tim tetap berlaku untuk kasus lain (ganti owner milik orang lain).
	u := userFrom(r)
	if !canSelfClaimItem(u, item) && !s.canJoinItemTeam(r, u, item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengubah item ini")
		return
	}

	var req assignRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.OwnerUsername)
	if target == "" {
		writeErr(w, http.StatusBadRequest, "owner_username wajib diisi")
		return
	}

	actor := currentUsername(r)
	user := userFrom(r)
	isAdmin := user != nil && user.Role == models.RoleAdmin

	// F25: owner lock — owner sudah terisi.
	if strings.TrimSpace(item.OwnerUsername) != "" && !strings.EqualFold(item.OwnerUsername, target) {
		if !isAdmin {
			writeErr(w, http.StatusForbidden,
				"penanggung jawab sudah terisi; hanya admin yang dapat menggantinya")
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			writeErr(w, http.StatusBadRequest, "alasan wajib diisi saat mengganti penanggung jawab")
			return
		}
	} else if !canManageItem(user, item) && !strings.EqualFold(target, actor) {
		// Owner kosong: NOC non-admin hanya boleh self-claim.
		writeErr(w, http.StatusForbidden, "hanya admin/pembuat/owner atau diri sendiri yang dapat ditetapkan")
		return
	}

	wasUnclaimed := strings.TrimSpace(item.OwnerUsername) == ""
	claimsSelf := strings.EqualFold(target, actor)

	// Ambil tiket tanpa owner lalu langsung mengerjakan: status ikut maju ke
	// in_progress agar NOC tidak perlu langkah tambahan. Hanya berlaku untuk
	// transisi yang sah menurut workflow (change memakai status implementing;
	// tiket dari state lain tidak dipaksa pindah).
	nextStatus := ""
	if wasUnclaimed && claimsSelf && (item.IsTicket() || item.ItemType == models.ItemRFS) {
		nextStatus = ticketClaimStatus(item.ItemType, item.Status)
		// RFS sebelum jadwal: klaim hanya mencatat owner/penangan. Pengerjaan
		// lebih awal wajib beralasan, dan jalur ini tidak membawa alasan —
		// penangan memakai tombol "Mulai Kerjakan" (POST /rfs-start).
		if nextStatus != "" && item.ItemType == models.ItemRFS {
			if _, pending := rfsSchedulePending(item, time.Now().UTC()); pending {
				nextStatus = ""
			}
		}
	}

	now := time.Now().UTC()
	err := s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.SetWorkItemOwner(r.Context(), item.ID, target, actor); err != nil {
			return err
		}
		// Penekan tombol "Kerjakan" (self-claim) otomatis tercatat sebagai
		// penangan tambahan agar terkendali oleh aturan sembunyi tombol "Ikuti"
		// dan tampil pada daftar "Ikut Menangani". Idempotent.
		if claimsSelf {
			if _, err := s.store.AddCollaborator(r.Context(), item.ID, target, actor); err != nil {
				return err
			}
		}
		if nextStatus != "" {
			wf := workitems.WorkflowFor(item.ItemType)
			fields := map[string]any{
				"status": nextStatus, "updated_by_username": actor,
			}
			if item.FirstResponseAt == nil && nextStatus != wf.InitialState {
				fields["first_response_at"] = now
			}
			if err := s.store.UpdateWorkItemFields(r.Context(), tx, item.ID, fields); err != nil {
				return err
			}
			if item.IsTicket() {
				result := wf.EvaluateTransitions(item.Status, nextStatus)
				if err := s.updateSLACycleOnStatus(r.Context(), tx, item, nextStatus, result, actor, now, wf.InitialState); err != nil {
					return err
				}
			}
			if err := workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
				WorkItemID: item.ID.String(),
				EventType:  workitems.EventStatusChanged,
				Actor:      actor,
				FromValue:  item.Status,
				ToValue:    nextStatus,
				Detail:     map[string]any{"note": "Diambil dan dikerjakan"},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	_ = s.store.AppendEventSimple(r.Context(), item.ID, workitems.EventAssigned, map[string]any{
		"owner_username": target,
		"reason":         strings.TrimSpace(req.Reason),
		"force":          isAdmin && strings.TrimSpace(item.OwnerUsername) != "" && !strings.EqualFold(item.OwnerUsername, target),
	})
	s.audit(r, actor, "item.assign", "work_item", item.ID.String(),
		map[string]any{
			"ref_no":         item.RefNo,
			"owner_username": target,
			"previous":       item.OwnerUsername,
			"reason":         strings.TrimSpace(req.Reason),
			"auto_status":    nextStatus,
		}, true)
	if nextStatus != "" {
		s.audit(r, actor, "item.status_change", "work_item", item.ID.String(), map[string]any{
			"ref_no": item.RefNo, "from": item.Status, "to": nextStatus, "auto": "claim",
		}, true)
		// F44: ringkasan tiket ikut diperbarui setelah diambil (status maju).
		s.maybeSendTicketSummary(r.Context(), now)
	}

	updated, err := s.store.GetWorkItem(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleAddCollaborator menambahkan penangan tambahan ke sebuah tiket.
//
// POST /api/items/{id}/collaborators
//
// Aturan: admin/owner/pembuat boleh menambah siapa pun; NOC lain hanya boleh
// menambahkan dirinya sendiri.
func (s *Server) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadItemForAttachment(w, r)
	if !ok {
		return
	}
	if !canWriteRole(userFrom(r)) {
		writeErr(w, http.StatusForbidden, "viewer tidak dapat menambah penangan")
		return
	}
	// Fix Item 6: ikut menangani tiket/RFS (self-join collaborator) selalu
	// boleh lintas tim, termasuk saat item sudah punya owner (keputusan
	// produk: NOC tetap boleh ikut penanganan). Guard tim tetap berlaku bila
	// targetnya orang lain.
	var req collaboratorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	actor := currentUsername(r)
	target := strings.TrimSpace(req.Username)
	if target == "" {
		target = actor // default: diri sendiri (self-claim)
	}
	isSelfJoin := strings.EqualFold(target, actor)
	if !isSelfJoin {
		u := userFrom(r)
		if !canJoinHandling(u, item) && !s.canJoinItemTeam(r, u, item) {
			writeErr(w, http.StatusForbidden, "tidak berhak mengubah item ini")
			return
		}
	}
	if !canManageItem(userFrom(r), item) && !isSelfJoin {
		writeErr(w, http.StatusForbidden, "hanya dapat menambahkan diri sendiri")
		return
	}

	inserted, err := s.store.AddCollaborator(r.Context(), item.ID, target, actor)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if inserted {
		_ = s.store.AppendEventSimple(r.Context(), item.ID, workitems.EventAssigned, map[string]any{
			"collaborator": target,
		})
		s.audit(r, actor, "item.collaborator_add", "work_item", item.ID.String(),
			map[string]any{"ref_no": item.RefNo, "username": target}, true)
	}

	// Tiket/RFS yang masih di state awal otomatis maju ke "in progress" saat
	// ada penangan yang mulai mengerjakan (self-join). Tanpa ini status tetap
	// new/planned walau sudah ada yang menangani.
	joinedStatus := ""
	if isSelfJoin {
		joinedStatus = s.advanceStatusOnJoin(r, item, actor)
	}

	// Penangan tambahan HANYA user yang benar-benar menekan ikut/kerjakan.
	// Ekspansi otomatis rekan setim dihapus (permintaan produk): daftar
	// penangan berisi owner + user NOC yang ikut secara eksplisit.
	list, err := s.store.ListCollaborators(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "inserted": inserted, "status": joinedStatus, "collaborators": list,
	})
}

// advanceStatusOnJoin memajukan status tiket/RFS ke "sedang dikerjakan" saat
// seorang user ikut menangani. Mengembalikan status baru (kosong bila tidak
// berubah).
//
// Aturan:
//   - hanya tiket (incident/request/change) dan RFS;
//   - hanya bila transisinya sah menurut workflow (mis. RFS new →
//     in_progress, change scheduled → implementing);
//   - RFS yang jadwalnya belum tiba TIDAK dimajukan di sini: "ikut menangani"
//     hanya mencatat penangan. Untuk mulai bekerja lebih awal, penangan
//     menekan "Mulai Kerjakan" yang mewajibkan alasan (tercatat early_start);
//   - status terminal (activated/cancelled/closed/completed) tidak disentuh.
func (s *Server) advanceStatusOnJoin(r *http.Request, item *models.WorkItem, actor string) string {
	if item == nil {
		return ""
	}
	target := ""
	switch item.ItemType {
	case models.ItemIncident, models.ItemRequest:
		target = "in_progress"
	case models.ItemChange:
		target = "implementing"
	case models.ItemRFS:
		target = "in_progress"
	default:
		return ""
	}
	if item.Status == target {
		return ""
	}
	wf := workitems.WorkflowFor(item.ItemType)
	if wf == nil || !wf.CanTransition(item.Status, target) {
		return ""
	}
	// Jangan pernah menutup/membuka item lewat jalur ini.
	if res := wf.EvaluateTransitions(item.Status, target); res.IsClosing || res.IsReopen {
		return ""
	}
	// RFS sebelum jadwal: pengerjaan lebih awal wajib beralasan, dan jalur ini
	// tidak membawa alasan. Status dibiarkan apa adanya; penangan memakai
	// tombol "Mulai Kerjakan" (POST /rfs-start) yang mencatat alasannya.
	if item.ItemType == models.ItemRFS {
		if _, pending := rfsSchedulePending(item, time.Now().UTC()); pending {
			return ""
		}
	}

	now := time.Now().UTC()
	fields := map[string]any{"status": target, "updated_by_username": actor}
	if item.FirstResponseAt == nil && target != wf.InitialState {
		fields["first_response_at"] = now
	}
	err := s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, item.ID, fields); err != nil {
			return err
		}
		if item.IsTicket() {
			res := wf.EvaluateTransitions(item.Status, target)
			if err := s.updateSLACycleOnStatus(r.Context(), tx, item, target, res, actor, now, wf.InitialState); err != nil {
				return err
			}
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: item.ID.String(),
			EventType:  workitems.EventStatusChanged,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    target,
			Detail:     map[string]any{"note": "Mulai dikerjakan (ikut menangani)"},
		})
	})
	if err != nil {
		return ""
	}
	s.audit(r, actor, "item.status_change", "work_item", item.ID.String(), map[string]any{
		"ref_no": item.RefNo, "from": item.Status, "to": target, "auto": "collaborator_join",
	}, true)
	if item.IsTicket() {
		s.maybeSendTicketSummary(r.Context(), now)
	} else if item.ItemType == models.ItemRFS {
		s.maybeSendRFSSummary(r.Context(), now)
	}
	return target
}

// handleRemoveCollaborator menghapus penangan tambahan dari sebuah tiket.
//
// DELETE /api/items/{id}/collaborators/{username}
//
// F25 — aturan diperketat: HANYA admin yang boleh melepas penangan lain.
// Non-admin hanya dapat melepas dirinya sendiri.
func (s *Server) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadItemForAttachment(w, r)
	if !ok {
		return
	}
	if !canWriteRole(userFrom(r)) {
		writeErr(w, http.StatusForbidden, "viewer tidak dapat menghapus penangan")
		return
	}
	// F31: task & daily_task hanya dapat diubah oleh tim yang sama.
	if !s.canJoinItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengubah item ini")
		return
	}

	actor := currentUsername(r)
	target := strings.TrimSpace(chi.URLParam(r, "username"))
	if target == "" {
		writeErr(w, http.StatusBadRequest, "username wajib diisi")
		return
	}
	user := userFrom(r)
	isAdmin := user != nil && user.Role == models.RoleAdmin
	// F25: non-admin hanya boleh melepas diri sendiri.
	if !isAdmin && !strings.EqualFold(target, actor) {
		writeErr(w, http.StatusForbidden, "hanya admin yang dapat melepas penangan lain")
		return
	}

	removed, err := s.store.RemoveCollaborator(r.Context(), item.ID, target)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if removed {
		_ = s.store.AppendEventSimple(r.Context(), item.ID, workitems.EventUnassigned, map[string]any{
			"collaborator": target,
		})
		s.audit(r, actor, "item.collaborator_remove", "work_item", item.ID.String(),
			map[string]any{"ref_no": item.RefNo, "username": target}, true)
	}

	list, err := s.store.ListCollaborators(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "collaborators": list})
}
