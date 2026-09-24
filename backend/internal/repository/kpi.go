package repository

import (
	"context"
	"time"
)

/* ---------------------------------------------------------------------------
   F20 — Data mentah untuk agregasi KPI (perhitungan di paket kpi).
   --------------------------------------------------------------------------- */

// KPICycleRow adalah satu siklus SLA tiket + metadata untuk KPI.
type KPICycleRow struct {
	WorkItemID       string
	ItemType         string
	Priority         string
	OwnerUsername    string
	CycleNo          int
	OpenedAt         time.Time
	ClosedAt         *time.Time
	FirstResponseAt  *time.Time
	HandlerUsername  string
	TargetResponse   int // menit (0 = pakai fallback)
	TargetResolution int
}

// KPICollaboratorRow memetakan work item ke penangan tambahan.
type KPICollaboratorRow struct {
	WorkItemID string
	Username   string
}

// KPISimpleTaskRow adalah Todo/Daily untuk KPI ketepatan waktu.
type KPISimpleTaskRow struct {
	ItemType string
	Owner    string
	DueAt    *time.Time
	ClosedAt *time.Time
	Status   string
}

// ListKPICycles mengambil siklus SLA dalam rentang (closed_at atau opened_at).
func (s *Store) ListKPICycles(ctx context.Context, from, to time.Time, itemType, owner string) ([]KPICycleRow, error) {
	args := []any{from, to}
	where := `NOT w.is_deleted AND w.item_type IN ('incident','request','change')
	          AND COALESCE(c.closed_at, c.opened_at) >= $1
	          AND COALESCE(c.closed_at, c.opened_at) < $2`
	if itemType != "" {
		args = append(args, itemType)
		where += " AND w.item_type = $" + itoa(len(args))
	}
	if owner != "" {
		args = append(args, owner)
		where += " AND (lower(w.owner_username) = lower($" + itoa(len(args)) + ")"
		args = append(args, owner)
		where += " OR EXISTS (SELECT 1 FROM ticket_collaborators tc WHERE tc.work_item_id = w.id AND lower(tc.username) = lower($" + itoa(len(args)) + ")))"
	}

	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.item_type, w.priority, w.owner_username, c.cycle_no,
		       c.opened_at, c.closed_at, c.first_response_at, c.handler_username,
		       COALESCE(fr.target_minutes, 0), COALESCE(rs.target_minutes, 0)
		FROM ticket_sla_cycles c
		JOIN work_items w ON w.id = c.work_item_id
		LEFT JOIN sla_targets fr ON fr.sla_policy_id = w.sla_policy_id AND fr.metric='first_response'
		LEFT JOIN sla_targets rs ON rs.sla_policy_id = w.sla_policy_id AND rs.metric='resolution'
		WHERE `+where+`
		ORDER BY c.opened_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []KPICycleRow{}
	for rows.Next() {
		var r KPICycleRow
		if err := rows.Scan(&r.WorkItemID, &r.ItemType, &r.Priority, &r.OwnerUsername,
			&r.CycleNo, &r.OpenedAt, &r.ClosedAt, &r.FirstResponseAt, &r.HandlerUsername,
			&r.TargetResponse, &r.TargetResolution); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListKPICollaborators mengambil seluruh pasangan (work_item, username).
func (s *Store) ListKPICollaborators(ctx context.Context) ([]KPICollaboratorRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT work_item_id, username FROM ticket_collaborators`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KPICollaboratorRow{}
	for rows.Next() {
		var r KPICollaboratorRow
		if err := rows.Scan(&r.WorkItemID, &r.Username); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListKPISimpleTasks mengambil Todo/Daily dalam rentang untuk KPI ketepatan waktu.
func (s *Store) ListKPISimpleTasks(ctx context.Context, from, to time.Time, owner string) ([]KPISimpleTaskRow, error) {
	args := []any{from, to}
	where := `NOT is_deleted AND item_type IN ('task','daily_task')
	          AND COALESCE(closed_at, created_at) >= $1
	          AND COALESCE(closed_at, created_at) < $2`
	if owner != "" {
		args = append(args, owner)
		where += " AND lower(owner_username) = lower($" + itoa(len(args)) + ")"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_type, owner_username, due_at, closed_at, status
		FROM work_items WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KPISimpleTaskRow{}
	for rows.Next() {
		var r KPISimpleTaskRow
		if err := rows.Scan(&r.ItemType, &r.Owner, &r.DueAt, &r.ClosedAt, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListKPIUsernames mengembalikan daftar username aktif untuk filter KPI.
func (s *Store) ListKPIUsernames(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT username FROM users
		WHERE is_active AND role NOT IN ('customer')
		ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

/* ---------------------------------------------------------------------------
   Tipe keluaran KPI (dihitung di paket kpi).
   --------------------------------------------------------------------------- */

// KPISummary adalah ringkasan keseluruhan periode.
type KPISummary struct {
	TicketsTotal     int     `json:"tickets_total"`
	TicketsClosed    int     `json:"tickets_closed"`
	TicketsOpen      int     `json:"tickets_open"`
	ResponseMet      int     `json:"response_met"`
	ResponseTotal    int     `json:"response_total"`
	ResponseMetPct   float64 `json:"response_met_pct"`
	ResolutionMet    int     `json:"resolution_met"`
	ResolutionTotal  int     `json:"resolution_total"`
	ResolutionMetPct float64 `json:"resolution_met_pct"`
	AvgResolutionSec float64 `json:"avg_resolution_seconds"`
	MedianResSec     float64 `json:"median_resolution_seconds"`
	P90ResSec        float64 `json:"p90_resolution_seconds"`
	Breached         int     `json:"breached"`
	SLAScore         float64 `json:"sla_score"`

	TodoTotal      int     `json:"todo_total"`
	TodoOnTime     int     `json:"todo_on_time"`
	TodoOnTimePct  float64 `json:"todo_on_time_pct"`
	DailyTotal     int     `json:"daily_total"`
	DailyOnTime    int     `json:"daily_on_time"`
	DailyOnTimePct float64 `json:"daily_on_time_pct"`
}

// KPIByPriority adalah agregat per prioritas (grafik).
type KPIByPriority struct {
	Priority         string  `json:"priority"`
	Total            int     `json:"total"`
	ResponseMetPct   float64 `json:"response_met_pct"`
	ResolutionMetPct float64 `json:"resolution_met_pct"`
	AvgResolutionSec float64 `json:"avg_resolution_seconds"`
}

// KPITrendPoint adalah satu titik deret waktu (hari WIB).
type KPITrendPoint struct {
	Day              string  `json:"day"`
	Closed           int     `json:"closed"`
	ResolutionMet    int     `json:"resolution_met"`
	ResolutionMetPct float64 `json:"resolution_met_pct"`
}

// KPIPerson adalah baris KPI per person (owner atau collaborator).
type KPIPerson struct {
	Username             string  `json:"username"`
	AssignedTotal        int     `json:"assigned_total"`
	ResolvedTotal        int     `json:"resolved_total"`
	OpenTotal            int     `json:"open_total"`
	ResponseMet          int     `json:"response_met"`
	ResponseTotal        int     `json:"response_total"`
	ResolutionMet        int     `json:"resolution_met"`
	ResolutionTotal      int     `json:"resolution_total"`
	AvgResolutionSeconds float64 `json:"avg_resolution_seconds"`
	Breached             int     `json:"breached"`
	ResponseMetPct       float64 `json:"response_met_pct"`
	ResolutionMetPct     float64 `json:"resolution_met_pct"`
	SLAScore             float64 `json:"sla_score"`
	TodoTotal            int     `json:"todo_total"`
	TodoOnTime           int     `json:"todo_on_time"`
	DailyTotal           int     `json:"daily_total"`
	DailyOnTime          int     `json:"daily_on_time"`
}
