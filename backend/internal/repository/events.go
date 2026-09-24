package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Work item events (append-only)
   --------------------------------------------------------------------------- */

const eventColumns = `id, work_item_id, event_type, actor_username, from_value, to_value, detail_json, created_at`

// ListEvents mengambil timeline sebuah work item (terbaru lebih dulu).
func (s *Store) ListEvents(ctx context.Context, workItemID uuid.UUID, limit int) ([]models.WorkItemEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+eventColumns+` FROM work_item_events
		WHERE work_item_id=$1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, workItemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.WorkItemEvent{}
	for rows.Next() {
		var e models.WorkItemEvent
		if err := rows.Scan(&e.ID, &e.WorkItemID, &e.EventType, &e.ActorUsername,
			&e.FromValue, &e.ToValue, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountEvents menghitung jumlah event sebuah work item.
func (s *Store) CountEvents(ctx context.Context, workItemID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM work_item_events WHERE work_item_id=$1`, workItemID).Scan(&n)
	return n, err
}

// NotificationFeedItem adalah satu baris feed notifikasi sidebar.
type NotificationFeedItem struct {
	ID         int64          `json:"id"`
	WorkItemID uuid.UUID      `json:"work_item_id"`
	RefNo      string         `json:"ref_no"`
	Title      string         `json:"title"`
	ItemType   string         `json:"item_type"`
	EventType  string         `json:"event_type"`
	Actor      string         `json:"actor_username"`
	FromValue  string         `json:"from_value"`
	ToValue    string         `json:"to_value"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  time.Time      `json:"created_at"`
}

// ListNotificationFeed mengambil aktivitas terbaru dari seluruh work item untuk
// ditampilkan pada panel notifikasi sidebar (lonceng).
//
// Sumbernya adalah work_item_events karena itu satu-satunya catatan append-only
// yang sudah mencakup pembuatan, perubahan status, komentar, dan kegagalan
// notifikasi. Filter sinceID dipakai klien untuk menghitung jumlah "belum
// dibaca" tanpa perlu tabel tambahan.
func (s *Store) ListNotificationFeed(ctx context.Context, itemType string, limit int, sinceID int64) ([]NotificationFeedItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	where := []string{"NOT wi.is_deleted"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + itoa(len(args))
	}

	if itemType != "" {
		where = append(where, "wi.item_type = "+arg(itemType))
	}
	if sinceID > 0 {
		where = append(where, "e.id > "+arg(sinceID))
	}

	query := `
		SELECT e.id, e.work_item_id, wi.ref_no, wi.title, wi.item_type,
		       e.event_type, e.actor_username, e.from_value, e.to_value,
		       e.detail_json, e.created_at
		FROM work_item_events e
		JOIN work_items wi ON wi.id = e.work_item_id
		WHERE ` + joinAnd(where) + `
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT ` + arg(limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NotificationFeedItem{}
	for rows.Next() {
		var it NotificationFeedItem
		if err := rows.Scan(&it.ID, &it.WorkItemID, &it.RefNo, &it.Title, &it.ItemType,
			&it.EventType, &it.Actor, &it.FromValue, &it.ToValue, &it.Detail, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MaxEventID mengembalikan id event terbesar (0 bila belum ada).
func (s *Store) MaxEventID(ctx context.Context) (int64, error) {
	var id *int64
	if err := s.pool.QueryRow(ctx, `SELECT max(id) FROM work_item_events`).Scan(&id); err != nil {
		return 0, err
	}
	if id == nil {
		return 0, nil
	}
	return *id, nil
}

/* ---------------------------------------------------------------------------
   Comments
   --------------------------------------------------------------------------- */

const commentColumns = `id, work_item_id, author_username, body, is_internal, created_at, updated_at`

func scanComment(row pgx.Row) (*models.Comment, error) {
	c := &models.Comment{}
	err := row.Scan(&c.ID, &c.WorkItemID, &c.AuthorUsername, &c.Body,
		&c.IsInternal, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return c, nil
}

// CreateComment menambahkan komentar di dalam transaksi.
func (s *Store) CreateComment(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, author, body string, isInternal bool) (*models.Comment, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO comments (work_item_id, author_username, body, is_internal)
		VALUES ($1,$2,$3,$4)
		RETURNING `+commentColumns, workItemID, author, body, isInternal)
	return scanComment(row)
}

// ListComments mengambil komentar sebuah work item.
//
// Query wajib melalui parent yang belum di-soft-delete, sesuai kebijakan
// yang didokumentasikan di DEVELOPMENT.md §4.2.
func (s *Store) ListComments(ctx context.Context, workItemID uuid.UUID) ([]models.Comment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.work_item_id, c.author_username, c.body, c.is_internal, c.created_at, c.updated_at
		FROM comments c
		JOIN work_items w ON w.id = c.work_item_id AND NOT w.is_deleted
		WHERE c.work_item_id = $1
		ORDER BY c.created_at ASC`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

/* ---------------------------------------------------------------------------
   Extension updates
   --------------------------------------------------------------------------- */

// UpdateRFSDetailsParams adalah parameter update rfs_details (parsial).
type UpdateRFSDetailsParams struct {
	CustomerName   *string
	ServicePackage *string
	Bandwidth      *string
	PicNOC         *string
	PicSales       *string
	Site           *string
	InstallStage   *string
	// F23: catatan penanganan (modal Troubleshoot).
	IssueFound      *string
	Troubleshooting *string
	ActionSolution  *string
	// F25: PIC team. SetPicTeam + PicTeamID memakai pola set-flag agar dapat
	// dikosongkan (NULL).
	PicTeamID  *uuid.UUID
	SetPicTeam bool
}

// UpdateRFSDetails memperbarui rfs_details.
func (s *Store) UpdateRFSDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p UpdateRFSDetailsParams) error {
	_, err := tx.Exec(ctx, `
		UPDATE rfs_details SET
			customer_name   = COALESCE($2, customer_name),
			service_package = COALESCE($3, service_package),
			bandwidth       = COALESCE($4, bandwidth),
			pic_noc         = COALESCE($5, pic_noc),
			pic_sales       = COALESCE($6, pic_sales),
			site            = COALESCE($7, site),
			install_stage   = COALESCE($8, install_stage),
			issue_found     = COALESCE($9, issue_found),
			troubleshooting = COALESCE($10, troubleshooting),
			action_solution = COALESCE($11, action_solution),
			pic_team_id     = CASE WHEN $13::boolean THEN $12 ELSE pic_team_id END
		WHERE work_item_id = $1`,
		workItemID, p.CustomerName, p.ServicePackage, p.Bandwidth,
		p.PicNOC, p.PicSales, p.Site, p.InstallStage,
		p.IssueFound, p.Troubleshooting, p.ActionSolution,
		p.PicTeamID, p.SetPicTeam)
	return err
}

// UpdateTaskDetails memperbarui checklist dan progres task.
func (s *Store) UpdateTaskDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, checklist []models.ChecklistItem, progressPct *int) error {
	_, err := tx.Exec(ctx, `
		UPDATE task_details SET
			checklist_json = COALESCE($2, checklist_json),
			progress_pct   = COALESCE($3, progress_pct)
		WHERE work_item_id = $1`, workItemID, checklist, progressPct)
	return err
}

// UpdateTicketDetailsParams adalah parameter update ticket_details (parsial).
type UpdateTicketDetailsParams struct {
	Category        *string
	Subcategory     *string
	IncidentType    *string
	Impact          *string
	Urgency         *string
	AssignmentGroup *string
	// Catatan penanganan (F21).
	IssueFound      *string
	Troubleshooting *string
	ActionSolution  *string
}

// UpdateTicketDetails memperbarui ticket_details (dipakai mulai F10).
func (s *Store) UpdateTicketDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p UpdateTicketDetailsParams) error {
	_, err := tx.Exec(ctx, `
		UPDATE ticket_details SET
			category         = COALESCE($2, category),
			subcategory      = COALESCE($3, subcategory),
			incident_type    = COALESCE($4, incident_type),
			impact           = COALESCE($5, impact),
			urgency          = COALESCE($6, urgency),
			assignment_group = COALESCE($7, assignment_group),
			issue_found      = COALESCE($8, issue_found),
			troubleshooting  = COALESCE($9, troubleshooting),
			action_solution  = COALESCE($10, action_solution)
		WHERE work_item_id = $1`,
		workItemID, p.Category, p.Subcategory, p.IncidentType, p.Impact, p.Urgency, p.AssignmentGroup,
		p.IssueFound, p.Troubleshooting, p.ActionSolution)
	return err
}

// UpdateReminderDetailsParams adalah parameter update reminder_details (parsial).
type UpdateReminderDetailsParams struct {
	Category           *string
	SubjectName        *string
	SubjectType        *string
	EscalationPolicyID *uuid.UUID
	ClearPolicy        bool
	RecurrenceRule     *string
}

// UpdateReminderDetails memperbarui reminder_details.
//
// Sebelumnya handler PATCH menerima blok `reminder` tetapi tidak menerapkannya
// (temuan saat menambah fitur edit), sehingga perubahan kategori/subjek/policy
// pada reminder tidak pernah tersimpan. Fungsi ini menutup celah tersebut.
func (s *Store) UpdateReminderDetails(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, p UpdateReminderDetailsParams) error {
	_, err := tx.Exec(ctx, `
		UPDATE reminder_details SET
			category             = COALESCE($2, category),
			subject_name         = COALESCE($3, subject_name),
			subject_type         = COALESCE($4, subject_type),
			escalation_policy_id = CASE WHEN $5::boolean THEN NULL ELSE COALESCE($6, escalation_policy_id) END,
			recurrence_rule      = COALESCE($7, recurrence_rule)
		WHERE work_item_id = $1`,
		workItemID, p.Category, p.SubjectName, p.SubjectType, p.ClearPolicy, p.EscalationPolicyID, p.RecurrenceRule)
	return err
}

/* ---------------------------------------------------------------------------
   Dashboard summary
   --------------------------------------------------------------------------- */

// DashboardSummary adalah ringkasan untuk halaman Dashboard.
type DashboardSummary struct {
	ByTypeStatus map[string]map[string]int `json:"by_type_status"`
	Total        int                       `json:"total"`

	OpenTotal      int `json:"open_total"`
	OverdueTotal   int `json:"overdue_total"`
	DueTodayTotal  int `json:"due_today_total"`
	ExpiringSoon   int `json:"expiring_soon"`
	ExpiredTotal   int `json:"expired_total"`
	RFSUpcoming    int `json:"rfs_upcoming"`
	NotifFailed24h int `json:"notif_failed_24h"`
	NotifPending   int `json:"notif_pending"`

	Upcoming []models.WorkItem `json:"upcoming"`
}

// DashboardScope (F31) membatasi baris task & daily_task sesuai tim/pemilik.
// Nil scope berarti tanpa pembatasan (admin).
type DashboardScope struct {
	TeamID   *uuid.UUID
	Username string
}

// scopeClause menghasilkan klausa SQL yang HANYA membatasi task/daily_task ke
// scope, sementara tipe lain (rfs/tiket/reminder) tetap tanpa filter (Y).
// Mengembalikan string kosong bila scope nil.
func (sc *DashboardScope) clause(arg func(any) string) string {
	if sc == nil {
		return ""
	}
	var inner string
	if sc.TeamID != nil {
		inner = "team_id = " + arg(*sc.TeamID)
	} else if strings.TrimSpace(sc.Username) != "" {
		u := strings.ToLower(strings.TrimSpace(sc.Username))
		inner = "(team_id IS NULL AND (lower(created_by) = " + arg(u) +
			" OR lower(owner_username) = " + arg(u) + "))"
	} else {
		return ""
	}
	return " AND (item_type NOT IN ('task','daily_task') OR (" + inner + "))"
}

// DashboardSummary menghitung ringkasan operasional.
//
// scope (F31), bila tidak nil, membatasi baris task & daily_task ke tim/pemilik
// pengguna; tipe lain tetap global.
func (s *Store) DashboardSummary(ctx context.Context, scope *DashboardScope) (*DashboardSummary, error) {
	out := &DashboardSummary{}

	byTypeStatus, err := s.CountWorkItemsByTypeStatus(ctx, scope)
	if err != nil {
		return nil, err
	}
	out.ByTypeStatus = byTypeStatus

	for _, statuses := range byTypeStatus {
		for status, n := range statuses {
			out.Total += n
			switch status {
			case "done", "cancelled", "activated", "closed", "expired", "fulfilled":
				// status akhir tidak dihitung sebagai pekerjaan terbuka
			default:
				out.OpenTotal += n
			}
		}
	}

	// Argumen terpisah per query (placeholder dihitung ulang tiap query).
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	scopeSQL := scope.clause(arg)

	row := s.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE due_at IS NOT NULL AND due_at < now()
		                     AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled'))            AS overdue,
		  count(*) FILTER (WHERE due_at IS NOT NULL
		                     AND due_at::date = (now() AT TIME ZONE 'Asia/Jakarta')::date
		                     AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled'))            AS due_today,
		  count(*) FILTER (WHERE expire_at IS NOT NULL
		                     AND expire_at > now() AND expire_at <= now() + interval '24 hours'
		                     AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled'))            AS expiring_soon,
		  count(*) FILTER (WHERE expire_at IS NOT NULL AND expire_at <= now()
		                     AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled'))            AS expired,
		  count(*) FILTER (WHERE item_type='rfs'
		                     AND expire_at IS NOT NULL AND expire_at > now()
		                     AND expire_at <= now() + interval '7 days'
		                     AND status NOT IN ('activated','cancelled'))                                                   AS rfs_upcoming
		FROM work_items WHERE NOT is_deleted`+scopeSQL, args...)
	if err := row.Scan(&out.OverdueTotal, &out.DueTodayTotal, &out.ExpiringSoon,
		&out.ExpiredTotal, &out.RFSUpcoming); err != nil {
		return nil, err
	}

	notifRow := s.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE status='failed' AND created_at > now() - interval '24 hours') AS failed24h,
		  count(*) FILTER (WHERE status='pending')                                             AS pending
		FROM notification_outbox`)
	if err := notifRow.Scan(&out.NotifFailed24h, &out.NotifPending); err != nil {
		return nil, err
	}

	// 7 item terdekat yang perlu perhatian.
	args2 := []any{}
	arg2 := func(v any) string {
		args2 = append(args2, v)
		return fmt.Sprintf("$%d", len(args2))
	}
	scopeSQL2 := scope.clause(arg2)
	rows, err := s.pool.Query(ctx, `
		SELECT `+workItemColumns+` FROM work_items
		WHERE NOT is_deleted
		  AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled')
		  AND (due_at IS NOT NULL OR expire_at IS NOT NULL)`+scopeSQL2+`
		ORDER BY LEAST(COALESCE(due_at, 'infinity'::timestamptz), COALESCE(expire_at, 'infinity'::timestamptz))
		LIMIT 7`, args2...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	upcoming, err := scanWorkItems(rows)
	if err != nil {
		return nil, err
	}
	out.Upcoming = upcoming

	return out, nil
}

/* ---------------------------------------------------------------------------
   Pembersihan (job retensi F9)
   --------------------------------------------------------------------------- */

// PurgeOutbox menghapus baris outbox yang sudah final dan lebih tua dari batas.
func (s *Store) PurgeOutbox(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM notification_outbox
		WHERE status IN ('sent','suppressed')
		  AND created_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountOutboxByStatus menghitung outbox per status.
func (s *Store) CountOutboxByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM notification_outbox GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// OutboxRow adalah ringkasan satu baris outbox untuk halaman Audit.
type OutboxRow struct {
	ID                int64      `json:"id"`
	WorkItemID        *uuid.UUID `json:"work_item_id,omitempty"`
	EventKey          string     `json:"event_key"`
	Channel           string     `json:"channel"`
	Destination       string     `json:"destination"`
	OffsetLabel       string     `json:"offset_label"`
	Severity          string     `json:"severity"`
	Status            string     `json:"status"`
	Attempts          int        `json:"attempts"`
	MaxAttempts       int        `json:"max_attempts"`
	LastError         string     `json:"last_error"`
	ProviderMessageID string     `json:"provider_message_id"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	NextAttemptAt     time.Time  `json:"next_attempt_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// ListOutboxParams adalah filter riwayat outbox.
type ListOutboxParams struct {
	Status  string
	Channel string
	Limit   int
	Offset  int
}

// ListOutbox mengambil riwayat pengiriman notifikasi.
func (s *Store) ListOutbox(ctx context.Context, p ListOutboxParams) ([]OutboxRow, int, error) {
	where := []string{"TRUE"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + itoa(len(args))
	}
	if p.Status != "" {
		where = append(where, "status = "+arg(p.Status))
	}
	if p.Channel != "" {
		where = append(where, "channel = "+arg(p.Channel))
	}
	clause := " WHERE " + joinAnd(where)

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox`+clause, args...).Scan(&total); err != nil {
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

	rows, err := s.pool.Query(ctx, `
		SELECT id, work_item_id, event_key, channel, destination, offset_label, severity,
		       status, attempts, max_attempts, last_error, provider_message_id, sent_at,
		       next_attempt_at, created_at
		FROM notification_outbox`+clause+
		" ORDER BY created_at DESC LIMIT "+arg(limit)+" OFFSET "+arg(offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []OutboxRow{}
	for rows.Next() {
		var o OutboxRow
		if err := rows.Scan(&o.ID, &o.WorkItemID, &o.EventKey, &o.Channel, &o.Destination,
			&o.OffsetLabel, &o.Severity, &o.Status, &o.Attempts, &o.MaxAttempts,
			&o.LastError, &o.ProviderMessageID, &o.SentAt, &o.NextAttemptAt, &o.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}
