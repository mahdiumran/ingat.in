package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F20 — Siklus SLA tiket
   --------------------------------------------------------------------------- */

const slaCycleColumns = `
	id, work_item_id, cycle_no, opened_at, reopened_at, closed_at,
	first_response_at, handler_username, closed_by, created_at`

func scanSLACycle(row pgx.Row) (*models.SLACycle, error) {
	c := &models.SLACycle{}
	err := row.Scan(&c.ID, &c.WorkItemID, &c.CycleNo, &c.OpenedAt, &c.ReopenedAt,
		&c.ClosedAt, &c.FirstResponseAt, &c.HandlerUsername, &c.ClosedBy, &c.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return c, nil
}

// CreateInitialSLACycle membuat siklus 0 saat tiket dibuat.
func (s *Store) CreateInitialSLACycle(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, openedAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_sla_cycles (work_item_id, cycle_no, opened_at)
		VALUES ($1, 0, $2)
		ON CONFLICT (work_item_id, cycle_no) DO NOTHING`, workItemID, openedAt)
	return err
}

// ListSLACycles mengambil seluruh siklus sebuah tiket (urut cycle_no).
func (s *Store) ListSLACycles(ctx context.Context, workItemID uuid.UUID) ([]models.SLACycle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+slaCycleColumns+` FROM ticket_sla_cycles
		WHERE work_item_id=$1 ORDER BY cycle_no`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SLACycle{}
	for rows.Next() {
		c, err := scanSLACycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CurrentSLACycle mengambil siklus aktif (closed_at NULL) terakhir.
func (s *Store) CurrentSLACycle(ctx context.Context, workItemID uuid.UUID) (*models.SLACycle, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+slaCycleColumns+` FROM ticket_sla_cycles
		WHERE work_item_id=$1 AND closed_at IS NULL
		ORDER BY cycle_no DESC LIMIT 1`, workItemID)
	return scanSLACycle(row)
}

// CloseCurrentSLACycle menutup siklus aktif: mencatat closed_at, first_response,
// dan handler (owner) saat ditutup.
func (s *Store) CloseCurrentSLACycle(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, closedAt *time.Time, firstResponseAt *time.Time, handler, closedBy string) error {
	_, err := tx.Exec(ctx, `
		UPDATE ticket_sla_cycles
		SET closed_at         = $2,
		    first_response_at = COALESCE($3, first_response_at),
		    handler_username  = CASE WHEN $4 <> '' THEN $4 ELSE handler_username END,
		    closed_by         = CASE WHEN $5 <> '' THEN $5 ELSE closed_by END
		WHERE work_item_id = $1 AND closed_at IS NULL`,
		workItemID, closedAt, firstResponseAt, handler, closedBy)
	return err
}

// OpenNewSLACycle membuka siklus baru saat tiket dibuka kembali.
//
// Mengembalikan nomor siklus baru.
func (s *Store) OpenNewSLACycle(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, reopenedAt time.Time) (int, error) {
	var nextNo int
	err := tx.QueryRow(ctx, `
		INSERT INTO ticket_sla_cycles (work_item_id, cycle_no, opened_at, reopened_at)
		SELECT $1, COALESCE(MAX(cycle_no), -1) + 1, $2, $2
		FROM ticket_sla_cycles WHERE work_item_id = $1
		RETURNING cycle_no`, workItemID, reopenedAt).Scan(&nextNo)
	return nextNo, err
}

// SetSLACycleFirstResponse mencatat respons pertama pada siklus aktif.
func (s *Store) SetSLACycleFirstResponse(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE ticket_sla_cycles
		SET first_response_at = COALESCE(first_response_at, $2)
		WHERE work_item_id = $1 AND closed_at IS NULL`, workItemID, at)
	return err
}

// IncrementReopenCount menaikkan penghitung reopen sebuah tiket.
func (s *Store) IncrementReopenCount(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE ticket_details
		SET reopen_count = reopen_count + 1
		WHERE work_item_id = $1`, workItemID)
	return err
}

// SLAResolutionStats mengembalikan statistik penyelesaian siklus untuk KPI,
// dikelompokkan per owner/handler. Dipakai oleh paket KPI.
type SLACycleRow struct {
	WorkItemID      uuid.UUID
	ItemType        string
	Priority        string
	CycleNo         int
	OpenedAt        time.Time
	ClosedAt        *time.Time
	FirstResponseAt *time.Time
	HandlerUsername string
	OwnerUsername   string
}

// ListSLACyclesForKPI mengambil siklus dalam rentang waktu (berdasarkan closed_at
// atau opened_at bila masih berjalan) beserta metadata tiket.
func (s *Store) ListSLACyclesForKPI(ctx context.Context, from, to time.Time, itemType string) ([]SLACycleRow, error) {
	args := []any{from, to}
	where := `w.item_type IN ('incident','request','change') AND NOT w.is_deleted
	          AND COALESCE(c.closed_at, c.opened_at) >= $1
	          AND COALESCE(c.closed_at, c.opened_at) < $2`
	if itemType != "" {
		args = append(args, itemType)
		where += " AND w.item_type = $3"
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.work_item_id, w.item_type, w.priority, c.cycle_no,
		       c.opened_at, c.closed_at, c.first_response_at,
		       c.handler_username, w.owner_username
		FROM ticket_sla_cycles c
		JOIN work_items w ON w.id = c.work_item_id
		WHERE `+where+`
		ORDER BY c.opened_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SLACycleRow{}
	for rows.Next() {
		var r SLACycleRow
		if err := rows.Scan(&r.WorkItemID, &r.ItemType, &r.Priority, &r.CycleNo,
			&r.OpenedAt, &r.ClosedAt, &r.FirstResponseAt, &r.HandlerUsername, &r.OwnerUsername); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrNoCurrentCycle menandakan siklus aktif tidak ditemukan.
var ErrNoCurrentCycle = errors.New("siklus SLA aktif tidak ditemukan")

// OpenSLACycleRow adalah siklus aktif yang perlu dievaluasi mesin SLA.
type OpenSLACycleRow struct {
	WorkItemID       uuid.UUID
	RefNo            string
	Title            string
	ItemType         string
	Priority         string
	OwnerUsername    string
	SLAPolicyID      *uuid.UUID
	CycleNo          int
	OpenedAt         time.Time
	FirstResponseAt  *time.Time
	TargetResponse   int // menit; 0 bila tak ada policy
	TargetResolution int
}

// ListOpenSLACycles mengambil seluruh siklus aktif (closed_at NULL) beserta
// target SLA dari policy (fallback 0 = pakai default di kode).
func (s *Store) ListOpenSLACycles(ctx context.Context) ([]OpenSLACycleRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.ref_no, w.title, w.item_type, w.priority, w.owner_username,
		       w.sla_policy_id, c.cycle_no, c.opened_at, c.first_response_at,
		       COALESCE(fr.target_minutes, 0), COALESCE(rs.target_minutes, 0)
		FROM ticket_sla_cycles c
		JOIN work_items w ON w.id = c.work_item_id
		LEFT JOIN sla_targets fr ON fr.sla_policy_id = w.sla_policy_id AND fr.metric='first_response'
		LEFT JOIN sla_targets rs ON rs.sla_policy_id = w.sla_policy_id AND rs.metric='resolution'
		WHERE c.closed_at IS NULL AND NOT w.is_deleted
		  AND w.item_type IN ('incident','request','change')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []OpenSLACycleRow{}
	for rows.Next() {
		var r OpenSLACycleRow
		if err := rows.Scan(&r.WorkItemID, &r.RefNo, &r.Title, &r.ItemType, &r.Priority,
			&r.OwnerUsername, &r.SLAPolicyID, &r.CycleNo, &r.OpenedAt, &r.FirstResponseAt,
			&r.TargetResponse, &r.TargetResolution); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetWorkItemSLAState memperbarui sla_state dan sla_breached_at sebuah item.
func (s *Store) SetWorkItemSLAState(ctx context.Context, id uuid.UUID, state string, breachedAt *time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE work_items SET sla_state=$2, sla_breached_at=$3, updated_at=now()
		WHERE id=$1 AND NOT is_deleted`, id, state, breachedAt)
	return err
}
