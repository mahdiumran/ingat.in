package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

// workItemColumns adalah daftar kolom standar untuk SELECT work_items.
// Dipakai konsisten oleh semua query agar urutan Scan tidak meleset.
const workItemColumns = `
	id, ref_no, item_type, title, description, priority, status, stage,
	owner_username, requester_username, team_id, organization_id, target_id,
	source, parent_id,
	start_at, due_at, expire_at,
	sla_policy_id, sla_state, sla_breached_at, first_response_at, resolved_at,
	closed_at, paused_total_seconds,
	device_ref, service_ref, customer_ref, tags,
	is_deleted, created_by, updated_by_username, created_at, updated_at`

func scanWorkItem(row pgx.Row) (*models.WorkItem, error) {
	wi := &models.WorkItem{}
	err := row.Scan(
		&wi.ID, &wi.RefNo, &wi.ItemType, &wi.Title, &wi.Description, &wi.Priority,
		&wi.Status, &wi.Stage,
		&wi.OwnerUsername, &wi.RequesterUsername, &wi.TeamID, &wi.OrganizationID,
		&wi.TargetID, &wi.Source, &wi.ParentID,
		&wi.StartAt, &wi.DueAt, &wi.ExpireAt,
		&wi.SLAPolicyID, &wi.SLAState, &wi.SLABreachedAt, &wi.FirstResponseAt,
		&wi.ResolvedAt, &wi.ClosedAt, &wi.PausedTotalSeconds,
		&wi.DeviceRef, &wi.ServiceRef, &wi.CustomerRef, &wi.Tags,
		&wi.IsDeleted, &wi.CreatedBy, &wi.UpdatedByUsername, &wi.CreatedAt, &wi.UpdatedAt,
	)
	if err != nil {
		return nil, mapErr(err)
	}
	return wi, nil
}

func scanWorkItems(rows pgx.Rows) ([]models.WorkItem, error) {
	out := []models.WorkItem{}
	for rows.Next() {
		wi, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wi)
	}
	return out, rows.Err()
}

// CreateWorkItemParams adalah parameter pembuatan work item.
type CreateWorkItemParams struct {
	RefNo             string
	ItemType          string
	Title             string
	Description       string
	Priority          string
	Status            string
	Stage             string
	OwnerUsername     string
	RequesterUsername string
	TeamID            *uuid.UUID
	OrganizationID    *uuid.UUID
	TargetID          *uuid.UUID
	Source            string
	ParentID          *uuid.UUID
	StartAt           *time.Time
	DueAt             *time.Time
	ExpireAt          *time.Time
	DeviceRef         string
	ServiceRef        string
	CustomerRef       string
	Tags              []string
	CreatedBy         string
}

// CreateWorkItem membuat work item baru di dalam transaksi yang diberikan.
func (s *Store) CreateWorkItem(ctx context.Context, tx pgx.Tx, p CreateWorkItemParams) (*models.WorkItem, error) {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO work_items (
			ref_no, item_type, title, description, priority, status, stage,
			owner_username, requester_username, team_id, organization_id, target_id,
			source, parent_id, start_at, due_at, expire_at,
			device_ref, service_ref, customer_ref, tags, created_by, updated_by_username
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,
			$18,$19,$20,$21,$22,$22
		)
		RETURNING `+workItemColumns,
		p.RefNo, p.ItemType, p.Title, p.Description, p.Priority, p.Status, p.Stage,
		p.OwnerUsername, p.RequesterUsername, p.TeamID, p.OrganizationID, p.TargetID,
		p.Source, p.ParentID, p.StartAt, p.DueAt, p.ExpireAt,
		p.DeviceRef, p.ServiceRef, p.CustomerRef, tags, p.CreatedBy,
	)
	return scanWorkItem(row)
}

// GetWorkItem mengambil satu work item berdasarkan ID.
func (s *Store) GetWorkItem(ctx context.Context, id uuid.UUID) (*models.WorkItem, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+workItemColumns+` FROM work_items WHERE id=$1 AND NOT is_deleted`, id)
	wi, err := scanWorkItem(row)
	if err != nil {
		return nil, err
	}
	if err := s.loadExtensions(ctx, wi); err != nil {
		return nil, err
	}
	return wi, nil
}

// GetWorkItemByRef mengambil work item berdasarkan nomor referensi.
func (s *Store) GetWorkItemByRef(ctx context.Context, refNo string) (*models.WorkItem, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+workItemColumns+` FROM work_items WHERE ref_no=$1 AND NOT is_deleted`, refNo)
	wi, err := scanWorkItem(row)
	if err != nil {
		return nil, err
	}
	if err := s.loadExtensions(ctx, wi); err != nil {
		return nil, err
	}
	return wi, nil
}

// loadExtensions mengisi extension sesuai item_type.
func (s *Store) loadExtensions(ctx context.Context, wi *models.WorkItem) error {
	switch wi.ItemType {
	case models.ItemTask, models.ItemDailyTask:
		d := &models.TaskDetails{WorkItemID: wi.ID}
		err := s.pool.QueryRow(ctx, `
			SELECT checklist_json, progress_pct, estimate_minutes, completion_note,
			       daily_task_type, result_status
			FROM task_details WHERE work_item_id=$1`, wi.ID,
		).Scan(&d.Checklist, &d.ProgressPct, &d.EstimateMinutes, &d.CompletionNote,
			&d.DailyTaskType, &d.ResultStatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			wi.Task = d
		}
	case models.ItemReminder:
		d := &models.ReminderDetails{WorkItemID: wi.ID}
		err := s.pool.QueryRow(ctx, `
			SELECT category, subject_name, subject_type, recurrence_rule,
			       escalation_policy_id, activated_at, last_offset_fired
			FROM reminder_details WHERE work_item_id=$1`, wi.ID,
		).Scan(&d.Category, &d.SubjectName, &d.SubjectType, &d.RecurrenceRule,
			&d.EscalationPolicyID, &d.ActivatedAt, &d.LastOffsetFired)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			wi.Reminder = d
		}
	case models.ItemRFS:
		d := &models.RFSDetails{WorkItemID: wi.ID}
		err := s.pool.QueryRow(ctx, `
			SELECT customer_name, service_id, service_package, bandwidth,
			       pic_noc, pic_sales, pic_team_id, sales_username, site, install_stage,
			       issue_found, troubleshooting, action_solution
			FROM rfs_details WHERE work_item_id=$1`, wi.ID,
		).Scan(&d.CustomerName, &d.ServiceID, &d.ServicePackage, &d.Bandwidth,
			&d.PicNOC, &d.PicSales, &d.PicTeamID, &d.SalesUsername, &d.Site, &d.InstallStage,
			&d.IssueFound, &d.Troubleshooting, &d.ActionSolution)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			wi.RFS = d
		}
		// F25: data teknis aktivasi (opsional, baris dibuat saat pertama disimpan).
		act := &models.RFSActivationData{WorkItemID: wi.ID}
		aerr := s.pool.QueryRow(ctx, `
			SELECT ip_address, vlan_detail, interface_port,
			       bandwidth_test, ping_test, packet_loss, updated_by, updated_at
			FROM rfs_activation_data WHERE work_item_id=$1`, wi.ID,
		).Scan(&act.IPAddress, &act.VLanDetail, &act.InterfacePort,
			&act.BandwidthTest, &act.PingTest, &act.PacketLoss, &act.UpdatedBy, &act.UpdatedAt)
		if aerr != nil && !errors.Is(aerr, pgx.ErrNoRows) {
			return aerr
		}
		if aerr == nil {
			wi.Activation = act
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			wi.RFS = d
		}
	case models.ItemIncident, models.ItemRequest, models.ItemChange:
		d := &models.TicketDetails{WorkItemID: wi.ID}
		err := s.pool.QueryRow(ctx, `
			SELECT category, subcategory, incident_type, impact, urgency,
			       assignment_group, escalation_level, reopen_count,
			       issue_found, troubleshooting, action_solution
			FROM ticket_details WHERE work_item_id=$1`, wi.ID,
		).Scan(&d.Category, &d.Subcategory, &d.IncidentType, &d.Impact, &d.Urgency,
			&d.AssignmentGroup, &d.EscalationLevel, &d.ReopenCount,
			&d.IssueFound, &d.Troubleshooting, &d.ActionSolution)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			wi.Ticket = d
			wi.ReopenCount = d.ReopenCount
		}
	}
	// F20: penangan tambahan + siklus SLA (hanya untuk tiket).
	if wi.IsTicket() {
		if cols, err := s.ListCollaborators(ctx, wi.ID); err == nil {
			wi.Collaborators = cols
		}
		if cycles, err := s.ListSLACycles(ctx, wi.ID); err == nil {
			wi.SLACycles = cycles
		}
	}
	// F27: waktu penyelesaian + aktornya.
	s.computeCompletion(ctx, wi)
	return nil
}

// completionStatuses adalah status akhir yang dianggap "selesai".
var completionStatuses = []string{
	"done", "closed", "activated", "completed", "fulfilled", "resolved",
}

// computeCompletion mengisi WorkItem.CompletedAt (resolved_at → closed_at) dan
// CompletedBy (actor event status-final; fallback updated_by_username).
func (s *Store) computeCompletion(ctx context.Context, wi *models.WorkItem) {
	if wi.ResolvedAt != nil {
		wi.CompletedAt = wi.ResolvedAt
	} else if wi.ClosedAt != nil {
		wi.CompletedAt = wi.ClosedAt
	}

	// Cari event terakhir yang memindahkan status ke status akhir selesai.
	var actor string
	err := s.pool.QueryRow(ctx, `
		SELECT actor_username FROM work_item_events
		WHERE work_item_id=$1 AND to_value = ANY($2::text[])
		ORDER BY id DESC LIMIT 1`, wi.ID, completionStatuses).Scan(&actor)
	if err == nil && actor != "" {
		wi.CompletedBy = actor
		return
	}
	// Fallback: pengubah terakhir.
	if wi.CompletedAt != nil {
		wi.CompletedBy = wi.UpdatedByUsername
	}
}

// ListWorkItemsParams adalah filter daftar work item.
type ListWorkItemsParams struct {
	ItemType   string
	Status     string
	Priority   string
	Owner      string
	TeamID     *uuid.UUID
	TargetID   *uuid.UUID
	Search     string
	Limit      int
	Offset     int
	OrderBy    string
	Descending bool

	// F31: pembatasan per tim untuk task & daily_task.
	// OwnerScopeUsername, bila tidak kosong, membatasi ke item tanpa tim
	// (team_id IS NULL) yang dibuat/dimiliki oleh username tsb. Dipakai untuk
	// pengguna non-admin yang belum punya tim. Bila kosong, pembatasan tim
	// memakai TeamID.
	OwnerScopeUsername string

	// DayFrom/DayTo menyaring berdasarkan tanggal (zona WIB) dari start_at.
	// Dipakai fitur Daily Task: satu hari penuh [DayFrom, DayTo).
	// Nilai nol berarti filter tidak aktif.
	DayFrom *time.Time
	DayTo   *time.Time
	// CarryOver, bila true bersama DayFrom, juga menyertakan item dari tanggal
	// sebelum DayFrom yang statusnya belum terminal (belum selesai/dibatalkan).
	CarryOver         bool
	NonTerminalStates []string
}

// ListWorkItems mengambil daftar work item dengan filter.
func (s *Store) ListWorkItems(ctx context.Context, p ListWorkItemsParams) ([]models.WorkItem, int, error) {
	where := []string{"NOT is_deleted"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if p.ItemType != "" {
		where = append(where, "item_type = "+arg(p.ItemType))
	}
	if p.Status != "" {
		where = append(where, "status = "+arg(p.Status))
	}
	if p.Priority != "" {
		where = append(where, "priority = "+arg(p.Priority))
	}
	if p.Owner != "" {
		where = append(where, "owner_username = "+arg(p.Owner))
	}
	if p.TeamID != nil {
		where = append(where, "team_id = "+arg(*p.TeamID))
	}
	// F31: pembatasan per tim untuk task & daily_task (hanya berlaku bila tipe
	// item yang diminta adalah task/daily_task — dijaga di lapisan API).
	if p.OwnerScopeUsername != "" {
		u := strings.ToLower(strings.TrimSpace(p.OwnerScopeUsername))
		where = append(where, "team_id IS NULL AND (lower(created_by) = "+arg(u)+
			" OR lower(owner_username) = "+arg(u)+")")
	}
	if p.TargetID != nil {
		where = append(where, "target_id = "+arg(*p.TargetID))
	}
	if p.Search != "" {
		like := "%" + strings.ToLower(p.Search) + "%"
		where = append(where, "(lower(title) LIKE "+arg(like)+" OR lower(ref_no) LIKE "+arg(like)+
			" OR lower(customer_ref) LIKE "+arg(like)+" OR lower(device_ref) LIKE "+arg(like)+")")
	}

	// Filter harian (Daily Task): cocokkan tanggal start_at.
	// Item tanpa start_at dianggap milik hari ini bila DayFrom aktif (agar
	// pembuatan cepat tanpa tanggal tetap tampil di kolom harian).
	if p.DayFrom != nil {
		if p.DayTo != nil {
			if p.CarryOver && len(p.NonTerminalStates) > 0 {
				// Hari ini ATAU carry-over dari sebelumnya yang belum selesai.
				where = append(where, "(start_at >= "+arg(*p.DayFrom)+" AND start_at < "+arg(*p.DayTo)+
					" OR (start_at < "+arg(*p.DayFrom)+" AND status = ANY("+arg(p.NonTerminalStates)+")))")
			} else {
				where = append(where, "start_at >= "+arg(*p.DayFrom)+" AND start_at < "+arg(*p.DayTo))
			}
		} else {
			where = append(where, "start_at >= "+arg(*p.DayFrom))
		}
	}

	clause := " WHERE " + strings.Join(where, " AND ")

	// Whitelist kolom order untuk mencegah SQL injection.
	orderCol := "created_at"
	switch p.OrderBy {
	case "due_at", "expire_at", "priority", "status", "updated_at", "ref_no", "created_at", "title":
		orderCol = p.OrderBy
	}
	dir := "ASC"
	if p.Descending {
		dir = "DESC"
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM work_items`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := p.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + workItemColumns + ` FROM work_items` + clause +
		fmt.Sprintf(" ORDER BY %s %s LIMIT %s OFFSET %s", orderCol, dir, arg(limit), arg(offset))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items, err := scanWorkItems(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateWorkItemFields melakukan update parsial pada kolom work item.
// Hanya kolom yang ada di whitelist yang diterapkan.
func (s *Store) UpdateWorkItemFields(ctx context.Context, tx pgx.Tx, id uuid.UUID, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"title": true, "description": true, "priority": true, "status": true,
		"stage": true, "owner_username": true, "requester_username": true,
		"team_id": true, "target_id": true, "start_at": true, "due_at": true,
		"expire_at": true, "device_ref": true, "service_ref": true, "customer_ref": true,
		"tags": true, "sla_policy_id": true, "sla_state": true,
		"first_response_at": true, "resolved_at": true, "closed_at": true,
		"paused_total_seconds": true, "parent_id": true, "updated_by_username": true,
	}

	sets := []string{}
	args := []any{}
	for col, val := range fields {
		if !allowed[col] {
			return fmt.Errorf("kolom %q tidak diizinkan untuk update", col)
		}
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	args = append(args, id)
	query := fmt.Sprintf("UPDATE work_items SET %s, updated_at = now() WHERE id = $%d AND NOT is_deleted",
		strings.Join(sets, ", "), len(args))

	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteWorkItem menandai work item sebagai terhapus.
//
// Penghapusan bersifat CASCADE ke anak: seluruh work item yang parent_id-nya
// menunjuk ke item ini (mis. todo/reminder/RFS yang dibuat menyertai tiket)
// ikut ditandai terhapus, secara rekursif. Mengembalikan jumlah baris yang
// terpengaruh (termasuk item induk).
func (s *Store) SoftDeleteWorkItem(ctx context.Context, id uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH RECURSIVE tree AS (
			SELECT id FROM work_items WHERE id=$1 AND NOT is_deleted
			UNION ALL
			SELECT w.id FROM work_items w
			JOIN tree t ON w.parent_id = t.id
			WHERE NOT w.is_deleted
		)
		UPDATE work_items SET is_deleted = TRUE, updated_at = now()
		WHERE id IN (SELECT id FROM tree)`, id)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	return int(tag.RowsAffected()), nil
}

// CountWorkItemsByTypeStatus mengembalikan jumlah work item dikelompokkan
// berdasarkan item_type dan status (dipakai dashboard).
// CountWorkItemsByTypeStatus menghitung jumlah per tipe & status.
//
// F31: scope (bila tidak nil) hanya membatasi baris task & daily_task ke
// tim/pemilik pengguna; tipe lain tetap dihitung global (Y).
func (s *Store) CountWorkItemsByTypeStatus(ctx context.Context, scope *DashboardScope) (map[string]map[string]int, error) {
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	q := `
		SELECT item_type, status, count(*)
		FROM work_items
		WHERE NOT is_deleted` + scope.clause(arg) + `
		GROUP BY item_type, status`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]int{}
	for rows.Next() {
		var it, st string
		var n int
		if err := rows.Scan(&it, &st, &n); err != nil {
			return nil, err
		}
		if out[it] == nil {
			out[it] = map[string]int{}
		}
		out[it][st] = n
	}
	return out, rows.Err()
}
