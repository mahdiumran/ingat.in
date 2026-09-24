package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

// ---------------------------------------------------------------------------
// Extension 1:1 work_items
//
// Setiap item_type memiliki tabel extension sendiri. Extension WAJIB dibuat
// bersamaan dengan work_items di dalam transaksi yang sama, jika tidak
// GetWorkItem akan mengembalikan extension nil.
//
// (Temuan A1 dari review F1: CreateWorkItem sebelumnya hanya menulis tabel
//  inti tanpa extension.)
// ---------------------------------------------------------------------------

// CreateTaskDetails membuat baris task_details.
func (s *Store) CreateTaskDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, checklist []models.ChecklistItem, estimateMinutes *int, dailyTaskType string) error {
	if checklist == nil {
		checklist = []models.ChecklistItem{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO task_details (work_item_id, checklist_json, progress_pct, estimate_minutes, daily_task_type)
		VALUES ($1, $2, 0, $3, $4)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, checklist, estimateMinutes, dailyTaskType)
	return err
}

// UpdateTaskResult menyimpan hasil penyelesaian Daily Task (F24): keterangan +
// status hasil (normal/bermasalah) + jenis bila belum ada.
func (s *Store) UpdateTaskResult(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, completionNote, resultStatus string) error {
	_, err := tx.Exec(ctx, `
		UPDATE task_details
		   SET completion_note = $2, result_status = $3
		 WHERE work_item_id = $1`, workItemID, completionNote, resultStatus)
	return err
}

// LinkDailyTicket menautkan daily task ke tiket hasil eskalasi (F24).
// Idempotent: mengembalikan tiket yang sudah tertaut bila ada (ok=false bila baru).
func (s *Store) LinkDailyTicket(ctx context.Context, tx pgx.Tx, dailyTaskID, ticketID uuid.UUID, actor string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO daily_ticket_links (daily_task_id, ticket_id, created_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (daily_task_id) DO NOTHING`, dailyTaskID, ticketID, actor)
	return err
}

// GetDailyTicketLink mengambil tiket yang tertaut ke sebuah daily task (F24).
// Mengembalikan uuid.Nil bila belum ada tautan.
func (s *Store) GetDailyTicketLink(ctx context.Context, dailyTaskID uuid.UUID) (uuid.UUID, error) {
	var ticketID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT ticket_id FROM daily_ticket_links WHERE daily_task_id=$1`, dailyTaskID).Scan(&ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ticketID, err
}

// CreateReminderDetails membuat baris reminder_details.
func (s *Store) CreateReminderDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, category, subjectName, subjectType string, escalationPolicyID *uuid.UUID, recurrenceRule *string) error {
	if category == "" {
		category = models.ReminderCategoryGeneric
	}
	if subjectType == "" {
		subjectType = models.SubjectTypeNone
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO reminder_details
			(work_item_id, category, subject_name, subject_type, escalation_policy_id, recurrence_rule)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, category, subjectName, subjectType, escalationPolicyID, recurrenceRule)
	return err
}

// CreateRFSDetailsParams adalah parameter pembuatan rfs_details.
type CreateRFSDetailsParams struct {
	CustomerName   string
	ServiceID      string
	ServicePackage string
	Bandwidth      string
	PicNOC         string
	PicSales       string
	// F25: PIC berupa tim.
	PicTeamID     *uuid.UUID
	SalesUsername string
	Site          string
	InstallStage  string
	// F23: catatan penanganan.
	IssueFound      string
	Troubleshooting string
	ActionSolution  string
}

// CreateRFSDetails membuat baris rfs_details.
func (s *Store) CreateRFSDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p CreateRFSDetailsParams) error {
	if p.InstallStage == "" {
		p.InstallStage = models.RFSStagePlanned
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO rfs_details
			(work_item_id, customer_name, service_id, service_package, bandwidth,
			 pic_noc, pic_sales, pic_team_id, sales_username, site, install_stage,
			 issue_found, troubleshooting, action_solution)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, p.CustomerName, p.ServiceID, p.ServicePackage, p.Bandwidth,
		p.PicNOC, p.PicSales, p.PicTeamID, p.SalesUsername, p.Site, p.InstallStage,
		p.IssueFound, p.Troubleshooting, p.ActionSolution)
	return err
}

// CreateTicketDetailsParams adalah parameter pembuatan ticket_details (F10).
type CreateTicketDetailsParams struct {
	Category        string
	Subcategory     string
	IncidentType    string
	Impact          string
	Urgency         string
	AssignmentGroup string
	EscalationLevel int
}

// CreateTicketDetails membuat baris ticket_details (dipakai pada F10).
func (s *Store) CreateTicketDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p CreateTicketDetailsParams) error {
	if p.Impact == "" {
		p.Impact = models.LevelMedium
	}
	if p.Urgency == "" {
		p.Urgency = models.LevelMedium
	}
	if p.EscalationLevel <= 0 {
		p.EscalationLevel = 1
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_details
			(work_item_id, category, subcategory, incident_type, impact, urgency, assignment_group, escalation_level)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, p.Category, p.Subcategory, p.IncidentType, p.Impact, p.Urgency, p.AssignmentGroup, p.EscalationLevel)
	return err
}

// UpdateRFSInstallStage memperbarui tahapan instalasi RFS (aksi "Tandai Aktif").
func (s *Store) UpdateRFSInstallStage(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, stage string) error {
	_, err := tx.Exec(ctx, `
		UPDATE rfs_details SET install_stage=$2 WHERE work_item_id=$1`, workItemID, stage)
	return err
}

// UpdateReminderOffset memperbarui offset terakhir yang sudah dikirim (F6).
func (s *Store) UpdateReminderOffset(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, offsetLabel string) error {
	_, err := tx.Exec(ctx, `
		UPDATE reminder_details SET last_offset_fired=$2 WHERE work_item_id=$1`, workItemID, offsetLabel)
	return err
}

// MarkReminderActivated menandai reminder sudah diaktivasi (F6).
func (s *Store) MarkReminderActivated(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE reminder_details SET activated_at=$2 WHERE work_item_id=$1`, workItemID, at.UTC())
	return err
}
