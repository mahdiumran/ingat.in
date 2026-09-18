package repository

import (
	"context"
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
func (s *Store) CreateTaskDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, checklist []models.ChecklistItem, estimateMinutes *int) error {
	if checklist == nil {
		checklist = []models.ChecklistItem{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO task_details (work_item_id, checklist_json, progress_pct, estimate_minutes)
		VALUES ($1, $2, 0, $3)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, checklist, estimateMinutes)
	return err
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
	SalesUsername  string
	Site           string
	InstallStage   string
}

// CreateRFSDetails membuat baris rfs_details.
func (s *Store) CreateRFSDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p CreateRFSDetailsParams) error {
	if p.InstallStage == "" {
		p.InstallStage = models.RFSStagePlanned
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO rfs_details
			(work_item_id, customer_name, service_id, service_package, bandwidth,
			 pic_noc, pic_sales, sales_username, site, install_stage)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (work_item_id) DO NOTHING`,
		workItemID, p.CustomerName, p.ServiceID, p.ServicePackage, p.Bandwidth,
		p.PicNOC, p.PicSales, p.SalesUsername, p.Site, p.InstallStage)
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
