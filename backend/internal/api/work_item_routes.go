package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type workItemCreateRequest struct {
	ItemType    string   `json:"item_type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Status      string   `json:"status"`
	Owner       string   `json:"owner_username"`
	Requester   string   `json:"requester_username"`
	TeamID      *string  `json:"team_id"`
	TargetID    *string  `json:"target_id"`
	ParentID    *string  `json:"parent_id"`
	StartAt     *string  `json:"start_at"`
	DueAt       *string  `json:"due_at"`
	ExpireAt    *string  `json:"expire_at"`
	DeviceRef   string   `json:"device_ref"`
	ServiceRef  string   `json:"service_ref"`
	CustomerRef string   `json:"customer_ref"`
	Tags        []string `json:"tags"`

	// Extension
	Task     *taskDetailsRequest     `json:"task"`
	Reminder *reminderDetailsRequest `json:"reminder"`
	RFS      *rfsDetailsRequest      `json:"rfs"`
	Ticket   *ticketDetailsRequest   `json:"ticket"`
}

type taskDetailsRequest struct {
	Checklist       []models.ChecklistItem `json:"checklist"`
	EstimateMinutes *int                   `json:"estimate_minutes"`
	ProgressPct     *int                   `json:"progress_pct"`
	// F24: jenis daily task (master data kind `daily_task_type`).
	DailyTaskType string `json:"daily_task_type"`
}

type reminderDetailsRequest struct {
	Category           string  `json:"category"`
	SubjectName        string  `json:"subject_name"`
	SubjectType        string  `json:"subject_type"`
	EscalationPolicyID *string `json:"escalation_policy_id"`
	RecurrenceRule     *string `json:"recurrence_rule"`
}

type rfsDetailsRequest struct {
	CustomerName   string `json:"customer_name"`
	ServiceID      string `json:"service_id"`
	ServicePackage string `json:"service_package"`
	Bandwidth      string `json:"bandwidth"`
	PicNOC         string `json:"pic_noc"`
	PicSales       string `json:"pic_sales"`
	// F25: PIC berupa tim. Kosong/null mengosongkan.
	PicTeamID    *string `json:"pic_team_id"`
	Site         string  `json:"site"`
	InstallStage string  `json:"install_stage"`
	// F23: catatan penanganan (modal Troubleshoot). Pointer agar PATCH parsial.
	IssueFound      *string `json:"issue_found"`
	Troubleshooting *string `json:"troubleshooting"`
	ActionSolution  *string `json:"action_solution"`
	// F25: data teknis aktivasi (upsert).
	Activation *rfsActivationRequest `json:"activation"`
}

// rfsActivationRequest adalah data teknis aktivasi RFS (F25).
type rfsActivationRequest struct {
	IPAddress     string `json:"ip_address"`
	VLanDetail    string `json:"vlan_detail"`
	InterfacePort string `json:"interface_port"`
	BandwidthTest string `json:"bandwidth_test"`
	PingTest      string `json:"ping_test"`
	PacketLoss    string `json:"packet_loss"`
}

type ticketDetailsRequest struct {
	Category        string `json:"category"`
	Subcategory     string `json:"subcategory"`
	IncidentType    string `json:"incident_type"`
	Impact          string `json:"impact"`
	Urgency         string `json:"urgency"`
	AssignmentGroup string `json:"assignment_group"`
	// F21: catatan penanganan (pointer agar PATCH parsial tidak menghapus).
	IssueFound      *string `json:"issue_found"`
	Troubleshooting *string `json:"troubleshooting"`
	ActionSolution  *string `json:"action_solution"`
}

type workItemUpdateRequest struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Priority    *string  `json:"priority"`
	Stage       *string  `json:"stage"`
	Owner       *string  `json:"owner_username"`
	Requester   *string  `json:"requester_username"`
	TeamID      *string  `json:"team_id"`
	TargetID    *string  `json:"target_id"`
	StartAt     *string  `json:"start_at"`
	DueAt       *string  `json:"due_at"`
	ExpireAt    *string  `json:"expire_at"`
	DeviceRef   *string  `json:"device_ref"`
	ServiceRef  *string  `json:"service_ref"`
	CustomerRef *string  `json:"customer_ref"`
	Tags        []string `json:"tags"`

	RFS      *rfsDetailsRequest      `json:"rfs"`
	Task     *taskDetailsRequest     `json:"task"`
	Ticket   *ticketDetailsRequest   `json:"ticket"`
	Reminder *reminderDetailsRequest `json:"reminder"`
}

type statusChangeRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
	// F24: hasil penyelesaian Daily Task ("normal"/"bermasalah").
	ResultStatus string `json:"result_status"`
}

type commentCreateRequest struct {
	Body       string `json:"body"`
	IsInternal *bool  `json:"is_internal"`
}

/* ---------------------------------------------------------------------------
   Daftar & detail
   --------------------------------------------------------------------------- */

// handleListWorkItems mengembalikan daftar work item dengan filter.
//
// GET /api/items?type=&status=&priority=&owner=&team_id=&q=&limit=&offset=&order=&desc=
func (s *Server) handleListWorkItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := repository.ListWorkItemsParams{
		ItemType: q.Get("type"),
		Status:   q.Get("status"),
		Priority: q.Get("priority"),
		Owner:    q.Get("owner"),
		Search:   q.Get("q"),
		OrderBy:  q.Get("order"),
		Limit:    50,
	}

	if raw := q.Get("team_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "team_id tidak valid")
			return
		}
		params.TeamID = &id
	}
	if raw := q.Get("target_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "target_id tidak valid")
			return
		}
		params.TargetID = &id
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > 500 {
			writeErr(w, http.StatusBadRequest, "limit harus 1..500")
			return
		}
		params.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeErr(w, http.StatusBadRequest, "offset tidak valid")
			return
		}
		params.Offset = v
	}
	params.Descending = q.Get("desc") != "false"

	// F31: pembatasan per tim untuk task & daily_task.
	// Non-admin hanya melihat item timnya (atau miliknya bila tanpa tim).
	if isTeamScopedType(params.ItemType) {
		if u := userFrom(r); u != nil && !s.isSuper(r, u) {
			if u.TeamID != nil {
				params.TeamID = u.TeamID
			} else {
				params.OwnerScopeUsername = u.Username
			}
		}
	}

	// Daily Task: filter satu hari (zona WIB) + carry-over opsional.
	// ?date=YYYY-MM-DD (default: hari ini WIB), ?carry_over=true
	if raw := q.Get("date"); raw != "" || q.Get("carry_over") != "" {
		day, err := parseWIBDay(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "date tidak valid (YYYY-MM-DD)")
			return
		}
		params.DayFrom = &day
		dayEnd := day.Add(24 * time.Hour)
		params.DayTo = &dayEnd
		params.CarryOver = q.Get("carry_over") == "true"
		if params.CarryOver {
			if wf := workitems.WorkflowFor(q.Get("type")); wf != nil {
				// State non-terminal = status yang BUKAN terminal (belum selesai).
				nonTerminal := make([]string, 0, len(wf.States))
				for _, st := range wf.States {
					isTerminal := false
					for _, ts := range wf.TerminalStates {
						if ts == st {
							isTerminal = true
							break
						}
					}
					if !isTerminal {
						nonTerminal = append(nonTerminal, st)
					}
				}
				params.NonTerminalStates = nonTerminal
			}
		}
	}

	items, total, err := s.store.ListWorkItems(r.Context(), params)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// Lengkapi SLA turunan untuk tiket (durasi penanganan).
	now := time.Now().UTC()
	for i := range items {
		items[i].ComputeSLA(now)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  params.Limit,
		"offset": params.Offset,
	})
}

// handleGetWorkItem mengambil satu work item beserta extension & timeline.
//
// GET /api/items/{id}
func (s *Server) handleGetWorkItem(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if !s.canViewItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak melihat item ini")
		return
	}

	events, err := s.store.ListEvents(r.Context(), id, 100)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	comments, err := s.store.ListComments(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	item.ComputeSLA(time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{
		"item":     item,
		"events":   events,
		"comments": comments,
		"workflow": workflowInfo(item.ItemType),
	})
}

/* ---------------------------------------------------------------------------
   Buat & ubah
   --------------------------------------------------------------------------- */

// handleCreateWorkItem membuat work item baru beserta extension-nya.
//
// POST /api/items
func (s *Server) handleCreateWorkItem(w http.ResponseWriter, r *http.Request) {
	var req workItemCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "judul wajib diisi")
		return
	}
	if !isKnownItemType(req.ItemType) {
		writeErr(w, http.StatusBadRequest, "item_type tidak dikenal")
		return
	}

	wf := workitems.WorkflowFor(req.ItemType)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = wf.InitialState
	}
	if !wf.IsValidState(status) {
		writeErr(w, http.StatusBadRequest, "status tidak sah untuk tipe ini")
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	if !isKnownPriority(priority) {
		writeErr(w, http.StatusBadRequest, "priority tidak dikenal")
		return
	}

	teamID, err := parseOptionalUUID(req.TeamID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "team_id tidak valid")
		return
	}
	// F31: task & daily_task dipartisi per tim. Non-admin selalu memakai tim
	// pengguna (input team_id diabaikan); admin boleh memilih tim mana pun.
	if isTeamScopedType(req.ItemType) {
		if u := userFrom(r); u != nil && !s.isSuper(r, u) {
			teamID = u.TeamID
		}
	}
	targetID, err := parseOptionalUUID(req.TargetID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "target_id tidak valid")
		return
	}
	parentID, err := parseOptionalUUID(req.ParentID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "parent_id tidak valid")
		return
	}

	startAt, err := parseOptionalTime(req.StartAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "start_at tidak valid (RFC3339)")
		return
	}
	dueAt, err := parseOptionalTime(req.DueAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "due_at tidak valid (RFC3339)")
		return
	}
	// F21: Daily Task tanpa tenggat → default 23:59 WIB pada tanggal harinya
	// (tanggal diambil dari start_at, atau hari ini bila start_at kosong).
	if dueAt == nil && req.ItemType == models.ItemDailyTask {
		base := time.Now()
		if startAt != nil {
			base = *startAt
		}
		dueAt = defaultDailyTaskDue(base)
	}
	expireAt, err := parseOptionalTime(req.ExpireAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "expire_at tidak valid (RFC3339)")
		return
	}

	// Validasi escalation policy lebih awal agar kesalahan terdeteksi sebelum
	// transaksi dibuka (penyimpanan dilakukan di createExtension).
	if req.Reminder != nil {
		if _, err := parseOptionalUUID(req.Reminder.EscalationPolicyID); err != nil {
			writeErr(w, http.StatusBadRequest, "escalation_policy_id tidak valid")
			return
		}
	}

	// Organisasi default dipakai agar F14 (multi-tenant) cukup mengaktifkan
	// penyaringan tanpa mengubah data lama.
	var orgID *uuid.UUID
	if org, err := s.store.GetDefaultOrganization(r.Context()); err == nil {
		orgID = &org.ID
	}

	actor := currentUsername(r)

	var created *models.WorkItem
	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(r.Context(), tx, req.ItemType, time.Now())
		if err != nil {
			return err
		}

		wi, err := s.store.CreateWorkItem(r.Context(), tx, repository.CreateWorkItemParams{
			RefNo:             ref,
			ItemType:          req.ItemType,
			Title:             req.Title,
			Description:       req.Description,
			Priority:          priority,
			Status:            status,
			OwnerUsername:     req.Owner,
			RequesterUsername: req.Requester,
			TeamID:            teamID,
			OrganizationID:    orgID,
			TargetID:          targetID,
			Source:            "web",
			ParentID:          parentID,
			StartAt:           startAt,
			DueAt:             dueAt,
			ExpireAt:          expireAt,
			DeviceRef:         req.DeviceRef,
			ServiceRef:        req.ServiceRef,
			CustomerRef:       req.CustomerRef,
			Tags:              workitems.NormalizeTags(req.Tags),
			CreatedBy:         actor,
		})
		if err != nil {
			return err
		}
		created = wi

		if err := s.createExtension(r, tx, wi.ID, req); err != nil {
			return err
		}

		// F20: tiket memulai siklus SLA pertama (siklus 0).
		if wi.IsTicket() {
			if err := s.store.CreateInitialSLACycle(r.Context(), tx, wi.ID, wi.CreatedAt); err != nil {
				return err
			}
		}

		// Event 'created' wajib: sumber timeline + audit + perhitungan SLA.
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: wi.ID.String(),
			EventType:  workitems.EventCreated,
			Actor:      actor,
			ToValue:    status,
			Detail:     map[string]any{"item_type": req.ItemType},
		})
	})
	if err != nil {
		// Kesalahan validasi (mis. dampak/urgensi tidak sah) dikembalikan 400.
		if isValidationErr(err) {
			writeErr(w, http.StatusBadRequest, cleanErrMsg(err))
			return
		}
		writeInternalError(w, err)
		return
	}

	// Ambil ulang agar extension hasil insert ikut terisi pada respons.
	full, err := s.store.GetWorkItem(r.Context(), created.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Notifikasi pembuatan item (requirement F5).
	// Kegagalan enqueue TIDAK boleh menggagalkan pembuatan item: item sudah
	// tersimpan dan operator dapat mengirim ulang. Error dicatat ke log.
	s.enqueueCreated(r, full)

	// F18: catat Todo Task ke spreadsheet (best-effort, task saja).
	s.syncSheet(r, full, "create")

	s.audit(r, "", "item.create", "work_item", full.ID.String(), map[string]any{
		"ref_no": full.RefNo, "item_type": full.ItemType,
	}, true)
	writeJSON(w, http.StatusCreated, full)
}

// enqueueCreated mengantrikan notifikasi bahwa sebuah work item baru dibuat.
//
// Kunci event memakai id item sehingga pengiriman bersifat idempoten: memanggil
// ulang tidak akan menggandakan pesan.
func (s *Server) enqueueCreated(r *http.Request, item *models.WorkItem) {
	// F25: resolusi target: item → tim (mis. PIC team RFS) → default.
	targetID, err := s.store.ResolveItemTargetID(r.Context(), item)
	if err != nil || targetID == nil {
		return
	}

	tplKey, severity := createdTemplateFor(item.ItemType)

	payload := notify.Payload{
		RefNo:       item.RefNo,
		Title:       item.Title,
		ItemType:    item.ItemType,
		Priority:    item.Priority,
		Status:      item.Status,
		Stage:       item.Stage,
		Owner:       item.OwnerUsername,
		Requester:   item.RequesterUsername,
		CreatedBy:   item.CreatedBy,
		DueAt:       notify.FormatWIB(item.DueAt),
		ExpireAt:    notify.FormatWIB(item.ExpireAt),
		CreatedAt:   notify.FormatWIB(&item.CreatedAt),
		Description: item.Description,
		DeviceRef:   item.DeviceRef,
		ServiceRef:  item.ServiceRef,
		CustomerRef: item.CustomerRef,
		SubjectName: item.Title,
		Category:    item.ItemType,
	}
	if item.RFS != nil {
		payload.CustomerName = item.RFS.CustomerName
		payload.ServicePackage = item.RFS.ServicePackage
		payload.Bandwidth = item.RFS.Bandwidth
		payload.PicNOC = item.RFS.PicNOC
		payload.PicSales = item.RFS.PicSales
		payload.Site = item.RFS.Site
	}
	if item.Reminder != nil {
		payload.SubjectName = item.Reminder.SubjectName
		payload.Category = item.Reminder.Category
	}

	if _, err := s.notifier.Outbox().Enqueue(r.Context(), notify.EnqueueParams{
		EventKey:    "created:" + item.ID.String(),
		WorkItemID:  &item.ID,
		SourceType:  "work_item",
		TemplateKey: tplKey,
		Severity:    severity,
		TargetID:    *targetID,
		Payload:     payload,
	}); err != nil {
		log.Printf("api: gagal mengantrikan notifikasi pembuatan %s: %v", item.RefNo, err)
	}
}

// syncSheet mengantrekan Todo Task dan Daily Task ke sinkronisasi spreadsheet
// (F18). Best-effort: kegagalan enqueue tidak boleh menggagalkan operasi utama —
// item sudah tersimpan dan worker akan mencoba lagi pada perubahan berikutnya.
func (s *Server) syncSheet(r *http.Request, item *models.WorkItem, action string) {
	if s.sheets == nil || item == nil || !isSheetSyncedType(item.ItemType) {
		return
	}

	op := models.SheetOpAppend
	if action != "create" {
		op = models.SheetOpUpdate
	}

	payload := map[string]any{
		"item_type":      item.ItemType,
		"ref_no":         item.RefNo,
		"title":          item.Title,
		"description":    item.Description,
		"priority":       item.Priority,
		"status":         item.Status,
		"owner":          item.OwnerUsername,
		"created_by":     item.CreatedBy,
		"updated_by":     item.UpdatedByUsername,
		"device_ref":     item.DeviceRef,
		"tags":           strings.Join(item.Tags, ", "),
		"due_at_wib":     notify.FormatWIB(item.DueAt),
		"created_at_wib": notify.FormatWIB(&item.CreatedAt),
		// F27: waktu selesai + aktor penyelesai.
		"completed_at_wib": notify.FormatWIB(item.CompletedAt),
		"completed_by":     item.CompletedBy,
	}
	if item.Task != nil {
		payload["completion_note"] = item.Task.CompletionNote
	}

	if _, err := s.sheets.Enqueue(r.Context(), sheets.EnqueueParams{
		WorkItemID: item.ID,
		RefNo:      item.RefNo,
		Op:         op,
		Action:     action,
		Payload:    payload,
	}); err != nil {
		log.Printf("api: gagal mengantrikan sinkronisasi spreadsheet %s: %v", item.RefNo, err)
	}
}

// isSheetSyncedType melaporkan apakah item_type ikut disinkronkan ke spreadsheet.
//
// Cakupan F18: Todo Task dan Daily Task (pekerjaan operasional NOC). Tiket,
// reminder, dan RFS tidak disinkronkan.
func isSheetSyncedType(itemType string) bool {
	switch itemType {
	case models.ItemTask, models.ItemDailyTask:
		return true
	default:
		return false
	}
}

// createdTemplateFor memilih template notifikasi pembuatan per tipe.
func createdTemplateFor(itemType string) (string, string) {
	switch itemType {
	case models.ItemRFS:
		// F30: pembuatan RFS/EWO memakai template khusus (bukan pengingat).
		return notify.TemplateRFSCreated, models.SeverityInfo
	case models.ItemReminder:
		return notify.TemplateReminderOffset, models.SeverityInfo
	case models.ItemDailyTask:
		return notify.TemplateDailyTaskCreated, models.SeverityInfo
	default:
		return notify.TemplateTodoCreated, models.SeverityInfo
	}
}

// createExtension membuat baris extension sesuai item_type di dalam transaksi.
func (s *Server) createExtension(r *http.Request, tx pgx.Tx, id uuid.UUID, req workItemCreateRequest) error {
	switch req.ItemType {
	case models.ItemTask, models.ItemDailyTask:
		var checklist []models.ChecklistItem
		var estimate *int
		dailyType := ""
		if req.Task != nil {
			checklist = req.Task.Checklist
			estimate = req.Task.EstimateMinutes
			dailyType = strings.TrimSpace(req.Task.DailyTaskType)
		}
		return s.store.CreateTaskDetails(r.Context(), tx, id, checklist, estimate, dailyType)

	case models.ItemReminder:
		d := reminderDetailsRequest{
			Category:    models.ReminderCategoryGeneric,
			SubjectType: models.SubjectTypeNone,
		}
		if req.Reminder != nil {
			d = *req.Reminder
		}
		var policyID *uuid.UUID
		if pid, err := parseOptionalUUID(d.EscalationPolicyID); err == nil {
			policyID = pid
		}
		return s.store.CreateReminderDetails(r.Context(), tx, id,
			d.Category, d.SubjectName, d.SubjectType, policyID, d.RecurrenceRule)

	case models.ItemRFS:
		var d repository.CreateRFSDetailsParams
		if req.RFS != nil {
			var picTeam *uuid.UUID
			if req.RFS.PicTeamID != nil && strings.TrimSpace(*req.RFS.PicTeamID) != "" {
				parsed, perr := uuid.Parse(strings.TrimSpace(*req.RFS.PicTeamID))
				if perr != nil {
					return fmt.Errorf("pic_team_id tidak valid: %w", errValidation)
				}
				picTeam = &parsed
			}
			d = repository.CreateRFSDetailsParams{
				CustomerName:    req.RFS.CustomerName,
				ServiceID:       req.RFS.ServiceID,
				ServicePackage:  req.RFS.ServicePackage,
				Bandwidth:       req.RFS.Bandwidth,
				PicNOC:          req.RFS.PicNOC,
				PicSales:        req.RFS.PicSales,
				PicTeamID:       picTeam,
				SalesUsername:   currentUsername(r),
				Site:            req.RFS.Site,
				InstallStage:    req.RFS.InstallStage,
				IssueFound:      derefStr(req.RFS.IssueFound),
				Troubleshooting: derefStr(req.RFS.Troubleshooting),
				ActionSolution:  derefStr(req.RFS.ActionSolution),
			}
		}
		return s.store.CreateRFSDetails(r.Context(), tx, id, d)

	case models.ItemIncident, models.ItemRequest, models.ItemChange:
		var d repository.CreateTicketDetailsParams
		if req.Ticket != nil {
			if err := validateTicketLevels(req.Ticket.Impact, req.Ticket.Urgency); err != nil {
				return err
			}
			d = repository.CreateTicketDetailsParams{
				Category:        req.Ticket.Category,
				Subcategory:     req.Ticket.Subcategory,
				IncidentType:    req.Ticket.IncidentType,
				Impact:          req.Ticket.Impact,
				Urgency:         req.Ticket.Urgency,
				AssignmentGroup: req.Ticket.AssignmentGroup,
			}
		}
		return s.store.CreateTicketDetails(r.Context(), tx, id, d)
	}
	return nil
}

// handleUpdateWorkItem memperbarui field work item.
//
// PATCH /api/items/{id}
func (s *Server) handleUpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req workItemUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	wf := workitems.WorkflowFor(item.ItemType)

	// Perubahan isi (deskripsi & detail extension) hanya boleh dilakukan oleh
	// pembuat/owner item atau admin. Field operasional lain (prioritas, owner,
	// tenggat, tag, dsb.) tetap dapat diubah oleh operator.
	if (req.Description != nil || req.RFS != nil || req.Task != nil || req.Ticket != nil || req.Reminder != nil) &&
		!s.canWriteItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "hanya pembuat item atau admin yang dapat mengubah deskripsi/detail")
		return
	}

	fields := map[string]any{}
	if req.Title != nil {
		fields["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if req.Priority != nil {
		if !isKnownPriority(*req.Priority) {
			writeErr(w, http.StatusBadRequest, "priority tidak dikenal")
			return
		}
		fields["priority"] = *req.Priority
	}
	if req.Stage != nil {
		fields["stage"] = *req.Stage
	}
	if req.Owner != nil {
		fields["owner_username"] = *req.Owner
	}
	if req.Requester != nil {
		fields["requester_username"] = *req.Requester
	}
	if req.DeviceRef != nil {
		fields["device_ref"] = *req.DeviceRef
	}
	if req.ServiceRef != nil {
		fields["service_ref"] = *req.ServiceRef
	}
	if req.CustomerRef != nil {
		fields["customer_ref"] = *req.CustomerRef
	}
	if req.Tags != nil {
		fields["tags"] = workitems.NormalizeTags(req.Tags)
	}
	if req.TeamID != nil {
		tid, err := parseOptionalUUID(req.TeamID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "team_id tidak valid")
			return
		}
		fields["team_id"] = tid
	}
	if req.TargetID != nil {
		tid, err := parseOptionalUUID(req.TargetID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "target_id tidak valid")
			return
		}
		fields["target_id"] = tid
	}
	if req.DueAt != nil {
		t, err := parseOptionalTime(req.DueAt)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "due_at tidak valid (RFC3339)")
			return
		}
		fields["due_at"] = t
	}
	if req.StartAt != nil {
		t, err := parseOptionalTime(req.StartAt)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "start_at tidak valid (RFC3339)")
			return
		}
		fields["start_at"] = t
	}
	if req.ExpireAt != nil {
		t, err := parseOptionalTime(req.ExpireAt)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "expire_at tidak valid (RFC3339)")
			return
		}
		fields["expire_at"] = t
	}

	if len(fields) == 0 && req.RFS == nil && req.Task == nil && req.Ticket == nil && req.Reminder == nil {
		writeErr(w, http.StatusBadRequest, "tidak ada perubahan yang dikirim")
		return
	}

	actor := currentUsername(r)

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if len(fields) > 0 {
			// Catat operator yang melakukan perubahan terakhir.
			fields["updated_by_username"] = actor
			if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, fields); err != nil {
				return err
			}
		}

		// Extension
		if req.RFS != nil {
			if item.ItemType != models.ItemRFS {
				return errors.New("field rfs hanya berlaku untuk item_type=rfs")
			}
			var picTeam *uuid.UUID
			if req.RFS.PicTeamID != nil && strings.TrimSpace(*req.RFS.PicTeamID) != "" {
				parsed, perr := uuid.Parse(strings.TrimSpace(*req.RFS.PicTeamID))
				if perr != nil {
					return fmt.Errorf("pic_team_id tidak valid: %w", errValidation)
				}
				picTeam = &parsed
			}
			if err := s.store.UpdateRFSDetails(r.Context(), tx, id, repository.UpdateRFSDetailsParams{
				CustomerName:    strPtrOrNil(req.RFS.CustomerName),
				ServicePackage:  strPtrOrNil(req.RFS.ServicePackage),
				Bandwidth:       strPtrOrNil(req.RFS.Bandwidth),
				PicNOC:          strPtrOrNil(req.RFS.PicNOC),
				PicSales:        strPtrOrNil(req.RFS.PicSales),
				Site:            strPtrOrNil(req.RFS.Site),
				InstallStage:    strPtrOrNil(req.RFS.InstallStage),
				IssueFound:      req.RFS.IssueFound,
				Troubleshooting: req.RFS.Troubleshooting,
				ActionSolution:  req.RFS.ActionSolution,
				PicTeamID:       picTeam,
				SetPicTeam:      req.RFS.PicTeamID != nil,
			}); err != nil {
				return err
			}
			// F25: simpan data teknis aktivasi bila dikirim.
			if req.RFS.Activation != nil {
				a := req.RFS.Activation
				if err := s.store.UpsertRFSActivation(r.Context(), tx, id, repository.RFSActivationInput{
					IPAddress:     a.IPAddress,
					VLanDetail:    a.VLanDetail,
					InterfacePort: a.InterfacePort,
					BandwidthTest: a.BandwidthTest,
					PingTest:      a.PingTest,
					PacketLoss:    a.PacketLoss,
				}, actor); err != nil {
					return err
				}
			}
		}
		if req.Task != nil {
			if item.ItemType != models.ItemTask && item.ItemType != models.ItemDailyTask {
				return errors.New("field task hanya berlaku untuk item_type=task atau daily_task")
			}
			if err := s.store.UpdateTaskDetails(r.Context(), tx, id,
				req.Task.Checklist, req.Task.ProgressPct); err != nil {
				return err
			}
		}
		if req.Ticket != nil {
			if item.ItemType != models.ItemIncident && item.ItemType != models.ItemRequest && item.ItemType != models.ItemChange {
				return errors.New("field ticket hanya berlaku untuk tipe tiket")
			}
			if err := validateTicketLevels(req.Ticket.Impact, req.Ticket.Urgency); err != nil {
				return err
			}
			if err := s.store.UpdateTicketDetails(r.Context(), tx, id, repository.UpdateTicketDetailsParams{
				Category:        strPtrOrNil(req.Ticket.Category),
				Subcategory:     strPtrOrNil(req.Ticket.Subcategory),
				IncidentType:    strPtrOrNil(req.Ticket.IncidentType),
				Impact:          strPtrOrNil(req.Ticket.Impact),
				Urgency:         strPtrOrNil(req.Ticket.Urgency),
				AssignmentGroup: strPtrOrNil(req.Ticket.AssignmentGroup),
				// F21: catatan penanganan.
				IssueFound:      req.Ticket.IssueFound,
				Troubleshooting: req.Ticket.Troubleshooting,
				ActionSolution:  req.Ticket.ActionSolution,
			}); err != nil {
				return err
			}
		}
		if req.Reminder != nil {
			if item.ItemType != models.ItemReminder {
				return errors.New("field reminder hanya berlaku untuk item_type=reminder")
			}
			var policyID *uuid.UUID
			if pid, err := parseOptionalUUID(req.Reminder.EscalationPolicyID); err == nil {
				policyID = pid
			}
			if err := s.store.UpdateReminderDetails(r.Context(), tx, id, repository.UpdateReminderDetailsParams{
				Category:           strPtrOrNil(req.Reminder.Category),
				SubjectName:        strPtrOrNil(req.Reminder.SubjectName),
				SubjectType:        strPtrOrNil(req.Reminder.SubjectType),
				EscalationPolicyID: policyID,
				RecurrenceRule:     req.Reminder.RecurrenceRule,
			}); err != nil {
				return err
			}
		}

		// Catat event updated dengan ringkasan field yang berubah.
		changed := make([]string, 0, len(fields)+1)
		for k := range fields {
			changed = append(changed, k)
		}
		if req.RFS != nil || req.Task != nil || req.Ticket != nil || req.Reminder != nil {
			changed = append(changed, "extension")
		}
		if len(changed) == 0 {
			return nil
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventUpdated,
			Actor:      actor,
			Detail:     map[string]any{"fields": changed},
		})
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "work item tidak ditemukan")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "item.update", "work_item", id.String(), map[string]any{
		"ref_no": updated.RefNo,
	}, true)
	_ = wf

	// F18: perbarui baris spreadsheet bila Todo Task berubah (best-effort).
	s.syncSheet(r, updated, "updated")

	writeJSON(w, http.StatusOK, updated)
}

// handleChangeStatus mengubah status dengan validasi workflow.
//
// POST /api/items/{id}/status
func (s *Server) handleChangeStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req statusChangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if req.Status == "" {
		writeErr(w, http.StatusBadRequest, "status wajib diisi")
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	// F31: task & daily_task hanya dapat diubah oleh tim yang sama.
	if !s.canWriteItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengubah item ini")
		return
	}

	wf := workitems.WorkflowFor(item.ItemType)
	if wf == nil {
		writeErr(w, http.StatusBadRequest, "workflow untuk tipe ini tidak ditemukan")
		return
	}
	if !wf.IsValidState(req.Status) {
		writeErr(w, http.StatusBadRequest, "status tidak sah untuk tipe ini")
		return
	}

	result := wf.EvaluateTransitions(item.Status, req.Status)
	if !result.Allowed {
		writeErr(w, http.StatusConflict, "transisi status dari "+item.Status+" ke "+req.Status+" tidak diizinkan")
		return
	}

	actor := currentUsername(r)
	now := time.Now().UTC()

	fields := map[string]any{"status": req.Status, "updated_by_username": actor}
	switch {
	case result.IsClosing && item.ClosedAt == nil:
		fields["closed_at"] = now
		if item.ResolvedAt == nil {
			fields["resolved_at"] = now
		}
	case result.IsReopen:
		fields["closed_at"] = nil
		fields["resolved_at"] = nil
	}
	// Responsible response time: catat pertama kali status meninggalkan state awal.
	if item.FirstResponseAt == nil && req.Status != wf.InitialState {
		fields["first_response_at"] = now
	}

	eventType := workitems.EventStatusChanged
	switch {
	case result.IsClosing && req.Status == "closed":
		eventType = workitems.EventClosed
	case req.Status == "resolved" || req.Status == "fulfilled":
		eventType = workitems.EventResolved
	case result.IsReopen:
		eventType = workitems.EventReopened
	case req.Status == "cancelled":
		eventType = workitems.EventCancelled
	}

	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := s.store.UpdateWorkItemFields(r.Context(), tx, id, fields); err != nil {
			return err
		}
		// Keterangan penyelesaian Daily Task (ditampilkan di timeline + sheet).
		if item.ItemType == models.ItemDailyTask && req.Status == "done" &&
			(strings.TrimSpace(req.Note) != "" || strings.TrimSpace(req.ResultStatus) != "") {
			if err := s.store.SetTaskCompletionResult(r.Context(), tx, id,
				strings.TrimSpace(req.Note), strings.TrimSpace(req.ResultStatus)); err != nil {
				return err
			}
		}
		// F20: perbarui siklus SLA tiket.
		if item.IsTicket() {
			if err := s.updateSLACycleOnStatus(r.Context(), tx, item, req.Status, result, actor, now, wf.InitialState); err != nil {
				return err
			}
		}
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  eventType,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    req.Status,
			Detail:     map[string]any{"note": req.Note},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	updated.ComputeSLA(time.Now().UTC())
	s.audit(r, "", "item.status_change", "work_item", id.String(), map[string]any{
		"ref_no": updated.RefNo, "from": item.Status, "to": req.Status,
	}, true)

	// F18: perbarui status pada spreadsheet (best-effort, task saja).
	s.syncSheet(r, updated, "status_changed")

	// F22: pemicu ringkasan tugas bila mode = on_change (best-effort).
	if item.ItemType == models.ItemDailyTask && item.Status != req.Status {
		s.maybeSendDailySummary(r.Context(), now)
	}

	writeJSON(w, http.StatusOK, updated)
}

// handleForceStatus memaksa status ke state mana pun tanpa aturan transisi.
//
// POST /api/items/{id}/force-status  (admin saja)
//
// Dipakai untuk koreksi darurat, mis. mengembalikan tiket dari canceled ke
// active/closed/pending. Alasan wajib diisi dan dicatat pada event + audit.
func (s *Server) handleForceStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req statusChangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Note = strings.TrimSpace(req.Note)
	if req.Status == "" {
		writeErr(w, http.StatusBadRequest, "status wajib diisi")
		return
	}
	if req.Note == "" {
		writeErr(w, http.StatusBadRequest, "alasan force status wajib diisi")
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}

	wf := workitems.WorkflowFor(item.ItemType)
	if wf == nil {
		writeErr(w, http.StatusBadRequest, "workflow untuk tipe ini tidak ditemukan")
		return
	}
	if !wf.IsValidState(req.Status) {
		writeErr(w, http.StatusBadRequest, "status tidak sah untuk tipe ini")
		return
	}

	actor := currentUsername(r)
	now := time.Now().UTC()

	fields := map[string]any{"status": req.Status, "updated_by_username": actor}
	// Kelola closed_at/resolved_at konsisten dengan transisi normal.
	if isTerminalState(wf, req.Status) {
		if item.ClosedAt == nil {
			fields["closed_at"] = now
		}
		if item.ResolvedAt == nil {
			fields["resolved_at"] = now
		}
	} else {
		fields["closed_at"] = nil
		fields["resolved_at"] = nil
	}
	if item.FirstResponseAt == nil && req.Status != wf.InitialState {
		fields["first_response_at"] = now
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
			ToValue:    req.Status,
			Detail:     map[string]any{"note": req.Note, "forced": true},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	updated, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	updated.ComputeSLA(time.Now().UTC())
	s.audit(r, "", "item.force_status", "work_item", id.String(), map[string]any{
		"ref_no": updated.RefNo, "from": item.Status, "to": req.Status, "reason": req.Note,
	}, true)
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteWorkItem melakukan soft delete.
//
// DELETE /api/items/{id}
//
// Hanya pembuat item (created_by), owner, atau admin yang boleh menghapus.
// Anak item (parent_id = item ini, mis. todo yang menyertai tiket) ikut terhapus.
func (s *Server) handleDeleteWorkItem(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if !s.canWriteItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "hanya pembuat item atau admin yang dapat menghapus")
		return
	}

	deleted, err := s.store.SoftDeleteWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	s.audit(r, "", "item.delete", "work_item", id.String(), map[string]any{
		"ref_no": item.RefNo, "item_type": item.ItemType, "deleted_total": deleted,
	}, true)

	// F18: tandai baris spreadsheet sebagai "Dihapus" agar riwayat tetap utuh.
	s.syncSheetDeleted(r, item)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

// syncSheetDeleted menandai baris spreadsheet milik item yang dihapus.
//
// Baris TIDAK dihapus dari spreadsheet agar riwayat kerjaan tetap lengkap;
// kolom Status ditulis "Dihapus". Best-effort seperti jalur sinkronisasi lain.
func (s *Server) syncSheetDeleted(r *http.Request, item *models.WorkItem) {
	if s.sheets == nil || item == nil || !isSheetSyncedType(item.ItemType) {
		return
	}
	payload := map[string]any{
		"item_type":      item.ItemType,
		"ref_no":         item.RefNo,
		"title":          item.Title,
		"description":    item.Description,
		"priority":       item.Priority,
		"status":         item.Status,
		"owner":          item.OwnerUsername,
		"created_by":     item.CreatedBy,
		"device_ref":     item.DeviceRef,
		"tags":           strings.Join(item.Tags, ", "),
		"due_at_wib":     notify.FormatWIB(item.DueAt),
		"created_at_wib": notify.FormatWIB(&item.CreatedAt),
		"deleted":        true,
	}
	if _, err := s.sheets.Enqueue(r.Context(), sheets.EnqueueParams{
		WorkItemID: item.ID,
		RefNo:      item.RefNo,
		Op:         models.SheetOpUpdate,
		Action:     "deleted",
		Payload:    payload,
	}); err != nil {
		log.Printf("api: gagal mengantrikan penandaan hapus spreadsheet %s: %v", item.RefNo, err)
	}
}

/* ---------------------------------------------------------------------------
   Timeline & komentar
   --------------------------------------------------------------------------- */

// handleListEvents mengembalikan timeline sebuah work item.
//
// GET /api/items/{id}/events
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	// F31: pastikan berhak melihat item sebelum menampilkan timeline.
	if item, err := s.store.GetWorkItem(r.Context(), id); err != nil {
		s.itemError(w, err)
		return
	} else if !s.canViewItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak melihat item ini")
		return
	}
	events, err := s.store.ListEvents(r.Context(), id, limit)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "total": len(events)})
}

// handleAddComment menambahkan komentar.
//
// POST /api/items/{id}/comments
func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}
	var req commentCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "isi komentar wajib diisi")
		return
	}

	// Pastikan item ada & belum dihapus.
	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}
	if !s.canViewItemTeam(r, userFrom(r), item) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengomentari item ini")
		return
	}

	isInternal := true
	if req.IsInternal != nil {
		isInternal = *req.IsInternal
	}
	actor := currentUsername(r)

	var comment *models.Comment
	err = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		c, err := s.store.CreateComment(r.Context(), tx, id, actor, req.Body, isInternal)
		if err != nil {
			return err
		}
		comment = c
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: id.String(),
			EventType:  workitems.EventCommented,
			Actor:      actor,
			Detail:     map[string]any{"comment_id": c.ID.String()},
		})
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

/* ---------------------------------------------------------------------------
   Dashboard
   --------------------------------------------------------------------------- */

// handleDashboard mengembalikan ringkasan untuk halaman Dashboard.
//
// GET /api/dashboard
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// F31: non-admin melihat ringkasan task & daily_task yang terbatas pada
	// timnya; tipe lain (rfs/tiket/reminder) tetap global.
	var scope *repository.DashboardScope
	if u := userFrom(r); u != nil && !s.isSuper(r, u) {
		if u.TeamID != nil {
			scope = &repository.DashboardScope{TeamID: u.TeamID}
		} else {
			scope = &repository.DashboardScope{Username: u.Username}
		}
	}
	summary, err := s.store.DashboardSummary(r.Context(), scope)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

/* ---------------------------------------------------------------------------
   Helper
   --------------------------------------------------------------------------- */

func (s *Server) itemID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id work item tidak valid")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) itemError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "work item tidak ditemukan")
		return
	}
	writeInternalError(w, err)
}

func isKnownItemType(t string) bool {
	switch t {
	case models.ItemTask, models.ItemReminder, models.ItemRFS,
		models.ItemIncident, models.ItemRequest, models.ItemChange, models.ItemDailyTask:
		return true
	default:
		return false
	}
}

// canManageItem melaporkan apakah user boleh mengubah isi/menghapus item.
//
// Aturan: admin selalu boleh; selain itu hanya pembuat (created_by) atau owner
// item. Username dibandingkan tanpa memandang besar-kecil huruf agar konsisten
// dengan pengecekan auth di tempat lain.
func canManageItem(user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	if user.Role == models.RoleAdmin {
		return true
	}
	u := strings.ToLower(strings.TrimSpace(user.Username))
	if u == "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(item.CreatedBy)) == u {
		return true
	}
	if strings.ToLower(strings.TrimSpace(item.OwnerUsername)) == u {
		return true
	}
	return false
}

func isKnownPriority(p string) bool {
	switch p {
	case "low", "normal", "high", "critical":
		return true
	default:
		return false
	}
}

// errValidation menandai kesalahan validasi input agar handler dapat
// mengembalikan HTTP 400 (bukan 500) saat error muncul dari dalam transaksi.
var errValidation = errors.New("validasi gagal")

// isValidationErr melaporkan apakah error berasal dari validasi input.
func isValidationErr(err error) bool { return errors.Is(err, errValidation) }

// isTerminalState melaporkan apakah state termasuk terminal pada workflow.
func isTerminalState(wf *workitems.Workflow, state string) bool {
	for _, s := range wf.TerminalStates {
		if s == state {
			return true
		}
	}
	return false
}

// cleanErrMsg menghapus penanda internal dari pesan error sebelum dikirim ke klien.
func cleanErrMsg(err error) string {
	return strings.TrimSuffix(strings.TrimSpace(err.Error()), ": "+errValidation.Error())
}

// validateTicketLevels memastikan impact & urgency hanya berisi low/medium/high
// (string kosong = tidak diubah / memakai default).
func validateTicketLevels(impact, urgency string) error {
	for _, v := range []string{impact, urgency} {
		switch v {
		case "", models.LevelLow, models.LevelMedium, models.LevelHigh:
		default:
			return fmt.Errorf("dampak/urgensi harus low, medium, atau high: %w", errValidation)
		}
	}
	return nil
}

// parseOptionalTime mengurai waktu RFC3339; string kosong berarti nil.
func parseOptionalTime(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		// Terima juga format tanpa zona sebagai waktu lokal server.
		if t2, err2 := time.Parse("2006-01-02T15:04", strings.TrimSpace(*raw)); err2 == nil {
			return &t2, nil
		}
		if t3, err3 := time.Parse("2006-01-02", strings.TrimSpace(*raw)); err3 == nil {
			return &t3, nil
		}
		return nil, err
	}
	return &t, nil
}

func strPtrOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// derefStr mengembalikan nilai string dari pointer, "" bila nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// wibLocation adalah zona waktu tampilan/operasional (Asia/Jakarta).
var wibLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		// Fallback UTC+7 tetap (tanpa DST) bila tzdata tidak tersedia.
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}()

// parseWIBDay mengurai tanggal YYYY-MM-DD sebagai awal hari (00:00) zona WIB.
// Bila raw kosong, memakai hari ini menurut WIB.
func parseWIBDay(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		now := time.Now().In(wibLocation)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wibLocation), nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, wibLocation)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// workflowInfo mengembalikan definisi workflow untuk UI.
func workflowInfo(itemType string) map[string]any {
	wf := workitems.WorkflowFor(itemType)
	if wf == nil {
		return nil
	}
	return map[string]any{
		"item_type":       wf.ItemType,
		"name":            wf.Name,
		"states":          wf.States,
		"initial_state":   wf.InitialState,
		"terminal_states": wf.TerminalStates,
		"transitions":     wf.Transitions,
	}
}
