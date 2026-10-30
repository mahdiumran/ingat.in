package notify

import (
	"fmt"
	"strings"
)

/* ---------------------------------------------------------------------------
   F45 — Ringkasan Aktivasi/EWO (RFS) terbuka.
   --------------------------------------------------------------------------- */

// RFSSummaryRow adalah data satu baris RFS untuk diformat pada ringkasan.
//
// Field disusun agar baris memuat: ref, judul, customer, kategori, PIC/owner,
// jadwal, status terlambat, dan tag — sesuai kebutuhan operasional NOC.
type RFSSummaryRow struct {
	RefNo    string
	Title    string
	Customer string
	Category string
	Owner    string
	Schedule string
	Overdue  bool
	Tags     []string
}

// FormatRFSSummaryLine menyusun satu baris teks ringkasan RFS.
//
// Contoh: "• RFS-2026-0001 PT Contoh — PT Contoh [Aktivasi] — noc1 — 05 Nov 2026 10:00 [TERLAMBAT] · #urgent"
func FormatRFSSummaryLine(r RFSSummaryRow) string {
	refNo := strings.TrimSpace(r.RefNo)
	title := strings.TrimSpace(r.Title)
	var b strings.Builder
	b.WriteString("• ")
	b.WriteString(refNo)
	if title != "" {
		b.WriteString(" ")
		b.WriteString(title)
	}
	if c := strings.TrimSpace(r.Customer); c != "" {
		b.WriteString(" — ")
		b.WriteString(c)
	}
	if c := strings.TrimSpace(r.Category); c != "" {
		b.WriteString(" [")
		b.WriteString(c)
		b.WriteString("]")
	}
	if o := strings.TrimSpace(r.Owner); o != "" {
		b.WriteString(" — ")
		b.WriteString(o)
	}
	if s := strings.TrimSpace(r.Schedule); s != "" {
		b.WriteString(" — ")
		b.WriteString(s)
	}
	if r.Overdue {
		b.WriteString(" [TERLAMBAT]")
	}
	tags := normalizeSummaryTags(r.Tags)
	if len(tags) > 0 {
		b.WriteString(" · #")
		b.WriteString(strings.Join(tags, " #"))
	}
	return b.String()
}

// normalizeSummaryTags membersihkan tag untuk tampilan (tanpa duplikat, tanpa
// tanda pagar ganda bila operator menulis "#urgent").
func normalizeSummaryTags(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// BuildRFSSummary menyusun Description (ringkas) + Notes (daftar) untuk
// notifikasi ringkasan Aktivasi/EWO terbuka, konsisten dengan
// BuildTicketSummary dan BuildDailySummary.
//
// Kelompok: Direncanakan (planned+postponed), Sedang dikerjakan
// (in_progress+in_progress_field), Menunggu troubleshooting
// (pending_troubleshoot). RFS activated/cancelled tidak dihitung.
func BuildRFSSummary(dayLabel string, planned, progress, pending int, lines []string) (description, notes string) {
	description = fmt.Sprintf(
		"📅 %s\n• Direncanakan : %d\n• Sedang dikerjakan : %d\n• Menunggu troubleshooting : %d\n• Total terbuka : %d",
		dayLabel, planned, progress, pending, planned+progress+pending)

	var b strings.Builder
	b.WriteString("— Daftar Aktivasi/EWO terbuka —\n")
	if len(lines) == 0 {
		b.WriteString("(tidak ada Aktivasi/EWO terbuka)\n")
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	notes = strings.TrimSpace(b.String())
	return description, notes
}
