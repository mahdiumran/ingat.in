package notify

import (
	"fmt"
	"strings"
)

// SummaryTask adalah satu baris tugas pada ringkasan harian.
type SummaryTask struct {
	RefNo   string
	Title   string
	Owner   string
	DueAt   string
	Overdue bool
}

// SummaryGroup adalah kumpulan tugas per status.
type SummaryGroup struct {
	Pending    []SummaryTask
	InProgress []SummaryTask
	Done       []SummaryTask
}

// BuildDailySummary menyusun Description (ringkas) dan Notes (daftar) untuk
// notifikasi ringkasan harian. Dipakai baik oleh job terjadwal maupun pemicu
// "on change" dari API agar formatnya konsisten.
func BuildDailySummary(dayLabel string, g SummaryGroup) (description, notes string) {
	description = fmt.Sprintf(
		"🕗 %s\n• Belum selesai : %d\n• Sedang dikerjakan : %d\n• Selesai : %d",
		dayLabel, len(g.Pending), len(g.InProgress), len(g.Done))

	var b strings.Builder
	writeGroup := func(title string, items []SummaryTask) {
		b.WriteString("— " + title + " (" + itoa(len(items)) + ") —\n")
		if len(items) == 0 {
			b.WriteString("(tidak ada)\n")
		}
		for _, it := range items {
			line := fmt.Sprintf("• %s %s", it.RefNo, it.Title)
			if it.Owner != "" {
				line += " — " + it.Owner
			}
			if it.Overdue {
				line += " [TERLAMBAT]"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	writeGroup("Belum selesai", g.Pending)
	writeGroup("Sedang dikerjakan", g.InProgress)
	writeGroup("Selesai", g.Done)
	notes = strings.TrimSpace(b.String())
	return description, notes
}
