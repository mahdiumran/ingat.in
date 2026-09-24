package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

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
	// F31: task & daily_task hanya dapat diubah oleh tim yang sama.
	if !s.canWriteItemTeam(r, userFrom(r), item) {
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

	if err := s.store.SetWorkItemOwner(r.Context(), item.ID, target, actor); err != nil {
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
		}, true)

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
	// F31: task & daily_task hanya dapat diubah oleh tim yang sama.
	if !s.canWriteItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengubah item ini")
		return
	}

	var req collaboratorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	actor := currentUsername(r)
	target := strings.TrimSpace(req.Username)
	if target == "" {
		target = actor // default: diri sendiri (self-claim)
	}
	if !canManageItem(userFrom(r), item) && !strings.EqualFold(target, actor) {
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

	list, err := s.store.ListCollaborators(r.Context(), item.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "inserted": inserted, "collaborators": list})
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
	if !s.canWriteItemTeam(r, userFrom(r), item) {
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
