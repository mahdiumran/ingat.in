package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/workitems"
)

// escalateTicketRequest adalah isi permintaan eskalasi Daily Task menjadi tiket.
type escalateTicketRequest struct {
	// TicketType: "incident" (default) atau "request".
	TicketType string `json:"ticket_type"`
	Title      string `json:"title"`
	// Note: catatan penanganan/deskripsi tugas yang bermasalah.
	Note string `json:"note"`
	// Prioritas opsional; default mengikuti prioritas task.
	Priority string `json:"priority"`
	// Extension tiket opsional (kategori, dampak, urgensi, dll).
	Ticket *ticketDetailsRequest `json:"ticket"`
}

// handleEscalateTicket membuat tiket (incident/request) dari sebuah Daily Task
// yang berstatus "bermasalah", lalu menautkannya ke task tersebut secara
// idempotent (satu task hanya menghasilkan satu tiket).
//
// POST /api/items/{id}/escalate-ticket
func (s *Server) handleEscalateTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req escalateTicketRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if item.ItemType != models.ItemDailyTask {
		writeErr(w, http.StatusBadRequest, "hanya Daily Task yang dapat dieskalasi menjadi tiket")
		return
	}
	// F31: daily task harus terlihat oleh pengguna (tim yang sama / milik sendiri).
	if !s.canViewItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengeskalasi task ini")
		return
	}

	// Jenis tiket hanya incident atau request.
	ticketType := strings.TrimSpace(strings.ToLower(req.TicketType))
	if ticketType == "" {
		ticketType = models.ItemIncident
	}
	if ticketType != models.ItemIncident && ticketType != models.ItemRequest {
		writeErr(w, http.StatusBadRequest, "ticket_type harus 'incident' atau 'request'")
		return
	}

	actor := currentUsername(r)
	if !s.canEscalateDailyTask(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengeskalasi task ini")
		return
	}

	// Idempotensi: bila sudah pernah dieskalasi, kembalikan tiket yang ada.
	if existing, err := s.store.GetDailyTicketLink(r.Context(), item.ID); err != nil {
		writeInternalError(w, err)
		return
	} else if existing != uuid.Nil {
		if t, err := s.store.GetWorkItem(r.Context(), existing); err == nil {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = item.Title
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = item.Priority
	}
	if !isKnownPriority(priority) {
		priority = "normal"
	}
	note := strings.TrimSpace(req.Note)

	var orgID *uuid.UUID
	if org, err := s.store.GetDefaultOrganization(r.Context()); err == nil {
		orgID = &org.ID
	}

	// Dampak/urgensi default bila extension tidak dikirim.
	ticketReq := workItemCreateRequest{ItemType: ticketType, Ticket: req.Ticket}
	if ticketReq.Ticket == nil {
		ticketReq.Ticket = &ticketDetailsRequest{}
	}
	if ticketReq.Ticket.Impact == "" {
		ticketReq.Ticket.Impact = models.LevelMedium
	}
	if ticketReq.Ticket.Urgency == "" {
		ticketReq.Ticket.Urgency = models.LevelMedium
	}
	if err := validateTicketLevels(ticketReq.Ticket.Impact, ticketReq.Ticket.Urgency); err != nil {
		writeErr(w, http.StatusBadRequest, cleanErrMsg(err))
		return
	}

	var created *models.WorkItem
	now := time.Now().UTC()
	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(r.Context(), tx, ticketType, now)
		if err != nil {
			return err
		}

		// Warisi konteks dari task: deskripsi = catatan + deskripsi task,
		// tim/target/owner/parent tetap terhubung ke task sumber.
		desc := note
		if item.Description != "" {
			if desc != "" {
				desc += "\n\n"
			}
			desc += item.Description
		}

		parentID := item.ID
		wi, err := s.store.CreateWorkItem(r.Context(), tx, repository.CreateWorkItemParams{
			RefNo:             ref,
			ItemType:          ticketType,
			Title:             title,
			Description:       desc,
			Priority:          priority,
			Status:            workitems.WorkflowFor(ticketType).InitialState,
			OwnerUsername:     item.OwnerUsername,
			RequesterUsername: item.RequesterUsername,
			TeamID:            item.TeamID,
			OrganizationID:    orgID,
			TargetID:          item.TargetID,
			Source:            "daily_task",
			ParentID:          &parentID,
			DeviceRef:         item.DeviceRef,
			ServiceRef:        item.ServiceRef,
			CustomerRef:       item.CustomerRef,
			Tags:              workitems.NormalizeTags(item.Tags),
			CreatedBy:         actor,
		})
		if err != nil {
			return err
		}
		created = wi

		if err := s.createExtension(r, tx, wi.ID, ticketReq); err != nil {
			return err
		}
		if err := s.store.CreateInitialSLACycle(r.Context(), tx, wi.ID, wi.CreatedAt); err != nil {
			return err
		}
		if err := s.store.LinkDailyTicket(r.Context(), tx, item.ID, wi.ID, actor); err != nil {
			return err
		}

		// Tandai task selesai dengan hasil "bermasalah" — hanya bila belum done.
		if item.Status != "done" {
			if err := s.store.UpdateWorkItemFields(r.Context(), tx, item.ID, map[string]any{
				"status":              "done",
				"updated_by_username": actor,
				"closed_at":           now,
				"resolved_at":         now,
			}); err != nil {
				return err
			}
		}
		if err := s.store.SetTaskCompletionResult(r.Context(), tx, item.ID, note, models.DailyResultBermasalah); err != nil {
			return err
		}

		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: item.ID.String(),
			EventType:  workitems.EventEscalated,
			Actor:      actor,
			ToValue:    ticketType,
			Detail:     map[string]any{"ticket_ref": ref, "ticket_id": wi.ID.String(), "note": note},
		})
	})
	if err != nil {
		if isValidationErr(err) {
			writeErr(w, http.StatusBadRequest, cleanErrMsg(err))
			return
		}
		writeInternalError(w, err)
		return
	}

	full, err := s.store.GetWorkItem(r.Context(), created.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.enqueueCreated(r, full)
	s.syncSheet(r, full, "create")

	s.audit(r, "", "item.escalate_ticket", "work_item", full.ID.String(), map[string]any{
		"daily_task_ref": item.RefNo, "ticket_ref": full.RefNo, "ticket_type": ticketType,
	}, true)

	// Kirim notifikasi khusus "tiket dari daily task" setelah audit (best-effort).
	s.notifyTicketFromDaily(r, full, item)

	writeJSON(w, http.StatusCreated, full)
}

// notifyTicketFromDaily mengantrikan notifikasi tiket yang lahir dari daily task.
// Best-effort: kegagalan enqueue dilaporkan ke log, tidak menggagalkan respons.
func (s *Server) notifyTicketFromDaily(r *http.Request, ticket *models.WorkItem, task *models.WorkItem) {
	targetID := ticket.TargetID
	if targetID == nil {
		def, err := s.store.DefaultTargetID(r.Context())
		if err != nil {
			return
		}
		targetID = def
	}
	payload := notify.Payload{
		RefNo:       ticket.RefNo,
		Title:       ticket.Title,
		ItemType:    ticket.ItemType,
		Priority:    ticket.Priority,
		Status:      ticket.Status,
		Owner:       ticket.OwnerUsername,
		Requester:   ticket.RequesterUsername,
		CreatedBy:   ticket.CreatedBy,
		CreatedAt:   notify.FormatWIB(&ticket.CreatedAt),
		Description: ticket.Description,
		ParentRef:   task.RefNo,
	}
	if _, err := s.notifier.Outbox().Enqueue(r.Context(), notify.EnqueueParams{
		EventKey:    "ticket_from_daily:" + ticket.ID.String(),
		WorkItemID:  &ticket.ID,
		SourceType:  "work_item",
		TemplateKey: notify.TemplateTicketFromDaily,
		Severity:    models.SeverityWarning,
		TargetID:    *targetID,
		Payload:     payload,
	}); err != nil {
		log.Printf("api: gagal mengantrikan notifikasi tiket dari daily task %s: %v", ticket.RefNo, err)
	}
}

// canEscalateDailyTask melaporkan apakah user boleh mengeskalasi Daily Task
// menjadi tiket gangguan (F24).
//
// Aturan (selain admin):
//   - pembuat atau owner task; ATAU
//   - anggota tim yang sama dengan task (users.team_id == work_items.team_id); ATAU
//   - role yang sama dengan pembuat/owner task ("peran operasional yang sama").
func (s *Server) canEscalateDailyTask(r *http.Request, user *models.User, item *models.WorkItem) bool {
	if canManageItem(user, item) {
		return true
	}
	if user == nil || item == nil {
		return false
	}

	// Tim yang sama.
	if user.TeamID != nil && item.TeamID != nil && *user.TeamID == *item.TeamID {
		return true
	}

	// Role yang sama dengan pembuat atau owner task.
	if user.Role != "" {
		for _, uname := range []string{item.CreatedBy, item.OwnerUsername} {
			uname = strings.TrimSpace(uname)
			if uname == "" {
				continue
			}
			other, err := s.store.GetUserByUsername(r.Context(), uname)
			if err != nil || other == nil {
				continue
			}
			if other.Role == user.Role {
				return true
			}
		}
	}
	return false
}
