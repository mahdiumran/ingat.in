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
//   - Status terminal tidak pernah dimajukan otomatis.
func ticketClaimStatus(itemType, current string) string {
	target := "in_progress"
	if itemType == models.ItemChange {
		target = "implementing"
	}
	// Hanya tipe tiket yang punya perilaku "mengerjakan" otomatis.
	if itemType != models.ItemIncident && itemType != models.ItemRequest && itemType != models.ItemChange {
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
	if wasUnclaimed && claimsSelf && item.IsTicket() {
		nextStatus = ticketClaimStatus(item.ItemType, item.Status)
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

	// Keputusan produk ("team ikut menangani"): mengambil item untuk diri
	// sendiri juga mencatat seluruh anggota tim sebagai penangan tambahan,
	// sehingga rekan setim dapat memperbarui/menutup item yang sama.
	if claimsSelf {
		s.addTeamCollaborators(r, item, actor)
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

	// Keputusan produk: saat operator IKUT SENDIRI, seluruh anggota timnya
	// ikut tercatat sebagai penangan ("team ikut menangani"). Admin yang
	// menambahkan orang lain tidak memicu ekspansi tim.
	var teamAdded []string
	if isSelfJoin {
		teamAdded = s.addTeamCollaborators(r, item, actor)
	}

	list, err := s.store.ListCollaborators(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if teamAdded == nil {
		teamAdded = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "inserted": inserted, "team_added": teamAdded, "collaborators": list,
	})
}

// addTeamCollaborators mencatat SELURUH anggota tim user sebagai penangan
// tambahan item ("team ikut menangani").
//
// Dipakai hanya pada jalur self-join ("Ikuti"/"Ikut RFS"/"Kerjakan") agar
// rekan setim otomatis ikut tercatat dan berhak memperbarui item. Best-effort:
// kegagalan lookup tim tidak menggagalkan aksi utama. Actor sendiri dilewati
// karena sudah ditambahkan pemanggil.
func (s *Server) addTeamCollaborators(r *http.Request, item *models.WorkItem, actor string) []string {
	u := userFrom(r)
	if u == nil || item == nil || u.TeamID == nil {
		return nil
	}
	// Hanya tiket & Aktivasi/EWO yang memakai konsep "ikut menangani".
	if !item.IsTicket() && item.ItemType != models.ItemRFS {
		return nil
	}
	members, err := s.store.ListTeamMemberUsernames(r.Context(), *u.TeamID)
	if err != nil {
		return nil
	}
	added := make([]string, 0, len(members))
	for _, name := range members {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, actor) {
			continue
		}
		inserted, err := s.store.AddCollaborator(r.Context(), item.ID, name, actor)
		if err != nil {
			continue
		}
		if inserted {
			added = append(added, name)
		}
	}
	if len(added) == 0 {
		return nil
	}
	_ = s.store.AppendEventSimple(r.Context(), item.ID, workitems.EventAssigned, map[string]any{
		"team_id":       u.TeamID.String(),
		"collaborators": added,
	})
	s.audit(r, actor, "item.collaborator_team_add", "work_item", item.ID.String(),
		map[string]any{"ref_no": item.RefNo, "team_id": u.TeamID.String(), "usernames": added}, true)
	return added
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
