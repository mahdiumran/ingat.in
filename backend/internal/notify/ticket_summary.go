package notify

import (
	"fmt"
	"strings"
)

// TicketSummaryGroup (F44) adalah kumpulan tiket terbuka per tahap besar.
type TicketSummaryGroup struct {
	New      []string // baris jadi
	Progress []string
	Done     []string
}

// BuildTicketSummary menyusun Description + Notes ringkasan tiket terbuka,
// format konsisten dengan ringkasan tugas harian (BuildDailySummary).
func BuildTicketSummary(dayLabel string, open, progress, nearly int, lines []string) (description, notes string) {
	description = fmt.Sprintf(
		"🎫 %s\n• Tiket baru : %d\n• Sedang dikerjakan : %d\n• Menunggu penutupan : %d\n• Total terbuka : %d",
		dayLabel, open, progress, nearly, open+progress+nearly)

	var b strings.Builder
	b.WriteString("— Daftar tiket terbuka —\n")
	if len(lines) == 0 {
		b.WriteString("(tidak ada tiket terbuka)\n")
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	notes = strings.TrimSpace(b.String())
	return description, notes
}
