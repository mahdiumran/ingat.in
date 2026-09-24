package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   F20 — Force Unlock (admin)
   --------------------------------------------------------------------------- */

// handleForceUnlock membuka kembali tiket yang sudah ditutup (closed/completed/
// fulfilled/done) TANPA mengikuti aturan transisi.
//
// POST /api/items/{id}/force-unlock  (admin saja)
//
// Efek: status berpindah ke status tujuan (default "in_progress"), siklus SLA
// baru dibuka, reopen_count naik, event "reopened" + "unlocked" dicatat,
// notifikasi pembukaan kembali dikirim ke target.
func (s *Server) handleForceUnlock(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req statusChangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	if req.Note == "" {
		writeErr(w, http.StatusBadRequest, "alasan wajib diisi")
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if !item.IsTicket() {
		writeErr(w, http.StatusBadRequest, "force unlock hanya berlaku untuk tiket")
		return
	}

	wf := workitems.WorkflowFor(item.ItemType)
	if wf == nil {
		writeErr(w, http.StatusBadRequest, "workflow untuk tipe ini tidak ditemukan")
		return
	}
	// Hanya boleh dari status terminal.
	if !isTerminalState(wf, item.Status) {
		writeErr(w, http.StatusConflict, "force unlock hanya untuk tiket yang sudah ditutup/completed")
		return
	}

	target := strings.TrimSpace(req.Status)
	if target == "" {
		target = "in_progress"
	}
	if target == "closed" {
		writeErr(w, http.StatusBadRequest, "status tujuan tidak boleh closed")
		return
	}
	if !wf.IsValidState(target) {
		writeErr(w, http.StatusBadRequest, "status tujuan tidak sah untuk tipe ini")
		return
	}

	actor := currentUsername(r)
	now := time.Now().UTC()

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		fields := map[string]any{
			"status":              target,
			"closed_at":           nil,
			"resolved_at":         nil,
			"updated_by_username": actor,
		}
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, fields); err != nil {
			return err
		}
		// Buka siklus SLA baru + naikkan penghitung reopen.
		if _, err := s.store.OpenNewSLACycle(r.Context(), tx, id, now); err != nil {
			return err
		}
		if err := s.store.IncrementReopenCount(r.Context(), tx, id); err != nil {
			return err
		}
		if err := workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventReopened,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    target,
			Detail:     map[string]any{"note": req.Note, "forced": true},
		}); err != nil {
			return err
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  "unlocked",
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    target,
			Detail:     map[string]any{"note": req.Note},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, actor, "item.force_unlock", "work_item", id.String(),
		map[string]any{"ref_no": item.RefNo, "from": item.Status, "to": target, "note": req.Note}, true)

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// Notifikasi pembukaan kembali.
	s.enqueueReopened(r, updated, req.Note)

	writeJSON(w, http.StatusOK, updated)
}

// enqueueReopened mengirim notifikasi bahwa tiket dibuka kembali.
func (s *Server) enqueueReopened(r *http.Request, item *models.WorkItem, note string) {
	targetID := item.TargetID
	if targetID == nil {
		def, err := s.store.DefaultTargetID(r.Context())
		if err != nil {
			return
		}
		targetID = def
	}
	payload := notify.Payload{
		RefNo:       item.RefNo,
		Title:       item.Title,
		ItemType:    item.ItemType,
		Priority:    item.Priority,
		Status:      item.Status,
		Owner:       item.OwnerUsername,
		Requester:   item.RequesterUsername,
		CreatedBy:   item.CreatedBy,
		DueAt:       notify.FormatWIB(item.DueAt),
		CreatedAt:   notify.FormatWIB(&item.CreatedAt),
		Description: item.Description,
		Notes:       note,
		DeviceRef:   item.DeviceRef,
		SubjectName: item.Title,
		Category:    item.ItemType,
	}
	// event_key unik per reopen (pakai waktu) agar tiap pembukaan mengirim pesan.
	_, _ = s.notifier.Outbox().Enqueue(r.Context(), notify.EnqueueParams{
		EventKey:    "reopened:" + item.ID.String() + ":" + time.Now().UTC().Format("20060102150405"),
		WorkItemID:  &item.ID,
		SourceType:  "work_item",
		TemplateKey: notify.TemplateTicketReopened,
		Severity:    models.SeverityWarning,
		TargetID:    *targetID,
		Payload:     payload,
	})
}
