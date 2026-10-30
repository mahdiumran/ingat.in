package repository

import (
	"context"
	"time"
)

/* ---------------------------------------------------------------------------
   F22 — Ringkasan tugas harian (pending / in_progress / done).
   --------------------------------------------------------------------------- */

// DailyTaskSummaryItem adalah satu tugas pada ringkasan harian.
type DailyTaskSummaryItem struct {
	RefNo   string
	Title   string
	Status  string
	Owner   string
	DueAt   *time.Time
	Overdue bool
}

// DailyTaskSummary adalah kumpulan tugas satu hari dikelompokkan per status.
type DailyTaskSummary struct {
	Pending    []DailyTaskSummaryItem
	InProgress []DailyTaskSummaryItem
	Waiting    []DailyTaskSummaryItem
	Done       []DailyTaskSummaryItem
}

// Total mengembalikan jumlah seluruh (kecuali canceled).
func (d DailyTaskSummary) Total() int {
	return len(d.Pending) + len(d.InProgress) + len(d.Waiting) + len(d.Done)
}

// CountByStatus mengembalikan jumlah per status utama.
func (d DailyTaskSummary) CountByStatus() map[string]int {
	return map[string]int{
		"pending":          len(d.Pending),
		"in_progress":      len(d.InProgress),
		"waiting_customer": len(d.Waiting),
		"done":             len(d.Done),
	}
}

// ListDailyTaskSummary mengambil tugas hari ini beserta carry-over yang belum
// selesai dari hari sebelumnya. Status 'canceled' dibuang.
func (s *Store) ListDailyTaskSummary(ctx context.Context, from, to time.Time) (DailyTaskSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ref_no, title, status, owner_username, due_at,
		       (due_at IS NOT NULL AND due_at < now() AND status NOT IN ('done','canceled')) AS overdue
		FROM work_items
		WHERE NOT is_deleted
		  AND item_type = 'daily_task'
		  AND status <> 'canceled'
		  AND ((start_at >= $1 AND start_at < $2)
		       OR (start_at < $1 AND status IN ('pending','in_progress','waiting_customer')))
		ORDER BY due_at NULLS LAST, ref_no`, from, to)
	if err != nil {
		return DailyTaskSummary{}, err
	}
	defer rows.Close()

	out := DailyTaskSummary{}
	for rows.Next() {
		var it DailyTaskSummaryItem
		if err := rows.Scan(&it.RefNo, &it.Title, &it.Status, &it.Owner, &it.DueAt, &it.Overdue); err != nil {
			return DailyTaskSummary{}, err
		}
		switch it.Status {
		case "in_progress":
			out.InProgress = append(out.InProgress, it)
		case "waiting_customer":
			out.Waiting = append(out.Waiting, it)
		case "done":
			out.Done = append(out.Done, it)
		default:
			out.Pending = append(out.Pending, it)
		}
	}
	return out, rows.Err()
}

/* ---------------------------------------------------------------------------
   F44 — Ringkasan tiket terbuka (incident/request/change yang belum closed).
   --------------------------------------------------------------------------- */

// OpenTicketSummaryItem adalah satu baris tiket pada ringkasan.
type OpenTicketSummaryItem struct {
	RefNo    string
	Title    string
	ItemType string
	Status   string
	Owner    string
	Overdue  bool
}

// OpenTicketSummary mengelompokkan tiket belum selesai per tahap besar.
type OpenTicketSummary struct {
	New      []OpenTicketSummaryItem // new / draft
	Progress []OpenTicketSummaryItem // assigned / in_progress / review / approved / scheduled / implementing / pending_customer
	Done     []OpenTicketSummaryItem // resolved / fulfilled; belum closed
}

// SummaryTicketTypes adalah tipe yang dihitung sebagai tiket.
var SummaryTicketTypes = []string{"incident", "request", "change"}

// ListOpenTicketSummary mengambil seluruh tiket aktif (belum terminal state).
// Tiket dianggap terlambat bila expire_at sudah lewat.
func (s *Store) ListOpenTicketSummary(ctx context.Context) (OpenTicketSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ref_no, title, item_type, status, COALESCE(owner_username, ''),
		       (expire_at IS NOT NULL AND expire_at < now()) AS overdue
		FROM work_items
		WHERE NOT is_deleted
		  AND item_type IN ('incident','request','change')
		  AND NOT (
		    (item_type IN ('incident','request') AND status = 'closed')
		    OR (item_type = 'change' AND status IN ('completed','rolled_back','cancelled'))
		  )
		ORDER BY ref_no`)
	if err != nil {
		return OpenTicketSummary{}, err
	}
	defer rows.Close()

	out := OpenTicketSummary{}
	for rows.Next() {
		var it OpenTicketSummaryItem
		if err := rows.Scan(&it.RefNo, &it.Title, &it.ItemType, &it.Status, &it.Owner, &it.Overdue); err != nil {
			return OpenTicketSummary{}, err
		}
		switch it.Status {
		case "resolved", "fulfilled":
			out.Done = append(out.Done, it)
		case "new", "draft":
			out.New = append(out.New, it)
		default:
			out.Progress = append(out.Progress, it)
		}
	}
	return out, rows.Err()
}

// Total mengembalikan jumlah seluruh tiket pada ringkasan.
func (d OpenTicketSummary) Total() int {
	return len(d.New) + len(d.Progress) + len(d.Done)
}

/* ---------------------------------------------------------------------------
   F45 — Ringkasan Aktivasi/EWO (RFS) yang belum aktif/batal.
   --------------------------------------------------------------------------- */

// OpenRFSSummaryItem adalah satu baris RFS pada ringkasan.
type OpenRFSSummaryItem struct {
	RefNo    string
	Title    string
	Customer string
	Category string
	Owner    string
	Schedule *time.Time
	Overdue  bool
	Tags     []string
}

// OpenRFSSummary mengelompokkan RFS belum selesai.
type OpenRFSSummary struct {
	Planned    []OpenRFSSummaryItem // new + planned (legacy) + postponed
	InProgress []OpenRFSSummaryItem // in_progress + in_progress_field
	Pending    []OpenRFSSummaryItem // pending_troubleshoot
}

// Total mengembalikan jumlah seluruh RFS pada ringkasan.
func (d OpenRFSSummary) Total() int {
	return len(d.Planned) + len(d.InProgress) + len(d.Pending)
}

// ListOpenRFSSummary mengambil seluruh RFS aktif (bukan activated/cancelled).
// Terlambat dihitung dari expire_at (jadwal RFS) yang sudah lewat.
//
// Owner/PIC memakai urutan: owner item -> nama tim PIC -> pic_noc -> pic_sales.
func (s *Store) ListOpenRFSSummary(ctx context.Context) (OpenRFSSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT wi.ref_no, wi.title, COALESCE(rd.customer_name, ''),
		       COALESCE(NULLIF(rd.category_label, ''), NULLIF(rd.category_code, ''), ''),
		       COALESCE(NULLIF(wi.owner_username, ''), NULLIF(t.name, ''),
		                NULLIF(rd.pic_noc, ''), NULLIF(rd.pic_sales, ''), ''),
		       wi.status,
		       wi.expire_at,
		       (wi.expire_at IS NOT NULL AND wi.expire_at < now()) AS overdue,
		       COALESCE(wi.tags, '[]'::jsonb)
		FROM work_items wi
		LEFT JOIN rfs_details rd ON rd.work_item_id = wi.id
		LEFT JOIN teams t ON t.id = rd.pic_team_id
		WHERE NOT wi.is_deleted
		  AND wi.item_type = 'rfs'
		  AND wi.status NOT IN ('activated', 'cancelled')
		ORDER BY wi.expire_at NULLS LAST, wi.ref_no`)
	if err != nil {
		return OpenRFSSummary{}, err
	}
	defer rows.Close()

	scanRows := []RFSSummaryRow{}
	for rows.Next() {
		var r RFSSummaryRow
		if err := rows.Scan(&r.RefNo, &r.Title, &r.Customer, &r.Category,
			&r.Owner, &r.Status, &r.Schedule, &r.Overdue, &r.Tags); err != nil {
			return OpenRFSSummary{}, err
		}
		scanRows = append(scanRows, r)
	}
	if err := rows.Err(); err != nil {
		return OpenRFSSummary{}, err
	}
	return GroupRFSSummary(scanRows), nil
}

// GroupRFSSummary memindahkan baris ke kelompok yang tepat sesuai status.
// Dipisah dari query agar pengelompokan dapat diuji tanpa database.
func GroupRFSSummary(rows []RFSSummaryRow) OpenRFSSummary {
	out := OpenRFSSummary{}
	for _, r := range rows {
		// Guard pertahanan: query utama sudah mengecualikan status terminal,
		// tetapi helper tidak boleh mengelompokkan activated/cancelled bila
		// dipakai ulang oleh caller atau test lain.
		if r.Status == "activated" || r.Status == "cancelled" {
			continue
		}
		it := OpenRFSSummaryItem{
			RefNo:    r.RefNo,
			Title:    r.Title,
			Customer: r.Customer,
			Category: r.Category,
			Owner:    r.Owner,
			Schedule: r.Schedule,
			Overdue:  r.Overdue,
			Tags:     r.Tags,
		}
		switch r.Status {
		case "in_progress", "in_progress_field":
			out.InProgress = append(out.InProgress, it)
		case "pending_troubleshoot":
			out.Pending = append(out.Pending, it)
		default:
			// new, planned (legacy), postponed, dan status tak dikenal lain.
			out.Planned = append(out.Planned, it)
		}
	}
	return out
}

// RFSSummaryRow adalah baris mentah ringkasan RFS sebelum dikelompokkan.
type RFSSummaryRow struct {
	RefNo    string
	Title    string
	Customer string
	Category string
	Owner    string
	Status   string
	Schedule *time.Time
	Overdue  bool
	Tags     []string
}
