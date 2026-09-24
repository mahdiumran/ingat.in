package api

import (
	"net/http"
	"strings"

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
