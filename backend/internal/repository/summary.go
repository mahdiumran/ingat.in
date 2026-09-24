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
	Done       []DailyTaskSummaryItem
}

// Total mengembalikan jumlah seluruh (kecuali canceled).
func (d DailyTaskSummary) Total() int {
	return len(d.Pending) + len(d.InProgress) + len(d.Done)
}

// CountByStatus mengembalikan jumlah per status utama.
func (d DailyTaskSummary) CountByStatus() map[string]int {
	return map[string]int{
		"pending":     len(d.Pending),
		"in_progress": len(d.InProgress),
		"done":        len(d.Done),
	}
}

// ListDailyTaskSummary mengambil tugas harian pada satu hari (start_at dalam
// rentang [from, to)) beserta status overdue-nya. Status 'canceled' dibuang.
func (s *Store) ListDailyTaskSummary(ctx context.Context, from, to time.Time) (DailyTaskSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ref_no, title, status, owner_username, due_at,
		       (due_at IS NOT NULL AND due_at < now() AND status NOT IN ('done','canceled')) AS overdue
		FROM work_items
		WHERE NOT is_deleted
		  AND item_type = 'daily_task'
		  AND status <> 'canceled'
		  AND start_at >= $1 AND start_at < $2
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
		case "done":
			out.Done = append(out.Done, it)
		default:
			out.Pending = append(out.Pending, it)
		}
	}
	return out, rows.Err()
}
