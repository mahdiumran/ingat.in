package notify

import (
	"strings"
	"testing"
)

// TestBuildDailySummaryGroups memverifikasi pengelompokan status pada ringkasan
// harian: setiap status punya header sendiri, grup kosong bertulis "(tidak ada)",
// dan "Belum selesai" pada Description mencakup tugas yang menunggu konfirmasi.
func TestBuildDailySummaryGroups(t *testing.T) {
	g := SummaryGroup{
		Pending: []SummaryTask{
			{RefNo: "DTK-2026-0001", Title: "Cek BGP", Owner: "budi"},
		},
		InProgress: []SummaryTask{
			{RefNo: "DTK-2026-0003", Title: "Restart uplink", Owner: "andi", Overdue: true},
		},
		Waiting: []SummaryTask{
			{RefNo: "DTK-2026-0002", Title: "Pengecekan PT Myfren", Owner: "mahdi", Overdue: true},
		},
		Done: []SummaryTask{
			{RefNo: "DTK-2026-0004", Title: "Pindah port HSP"},
		},
	}

	desc, notes := BuildDailySummary("29 Sep 2026 00:19", g)

	// Description: "Belum selesai" = pending + waiting.
	for _, want := range []string{
		"🕗 29 Sep 2026 00:19",
		"• Belum selesai : 2",
		"• Sedang dikerjakan : 1",
		"• Selesai : 1",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description tidak memuat %q, got:\n%s", want, desc)
		}
	}

	for _, want := range []string{
		"— Belum selesai (1) —",
		"— Sedang dikerjakan (1) —",
		"— Menunggu Konfirmasi Pelanggan (1) —",
		"— Selesai (1) —",
		"• DTK-2026-0001 Cek BGP — budi",
		"• DTK-2026-0002 Pengecekan PT Myfren — mahdi [TERLAMBAT]",
		"• DTK-2026-0003 Restart uplink — andi [TERLAMBAT]",
		"• DTK-2026-0004 Pindah port HSP",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes tidak memuat %q, got:\n%s", want, notes)
		}
	}

	// Grup kosong harus menampilkan "(tidak ada)".
	empty := SummaryGroup{}
	_, emptyNotes := BuildDailySummary("29 Sep 2026 00:19", empty)
	if n := strings.Count(emptyNotes, "(tidak ada)"); n != 4 {
		t.Errorf("grup kosong harus 4 baris (tidak ada), dapat %d:\n%s", n, emptyNotes)
	}
	if strings.Contains(emptyNotes, "DTK") {
		t.Error("ringkasan kosong tidak boleh memuat baris tugas")
	}
}
