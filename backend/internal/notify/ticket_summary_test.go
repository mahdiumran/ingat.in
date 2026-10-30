package notify

import (
	"strings"
	"testing"
)

func TestBuildTicketSummary(t *testing.T) {
	desc, notes := BuildTicketSummary("30 Okt 2026 09:00", 2, 1, 1, []string{
		"— Tiket baru (2) —",
		"• INC-2026-0001 Link down 🔴 — noc1",
		"• REQ-2026-0002 Tambah akses 🟡 — noc2",
	})
	for _, want := range []string{"Tiket baru : 2", "Sedang dikerjakan : 1", "Menunggu penutupan : 1", "Total terbuka : 4"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description tidak memuat %q: %q", want, desc)
		}
	}
	if !strings.Contains(notes, "INC-2026-0001") || !strings.Contains(notes, "noc1") {
		t.Errorf("notes tidak lengkap: %q", notes)
	}
}

func TestBuildTicketSummaryEmpty(t *testing.T) {
	desc, notes := BuildTicketSummary("30 Okt 2026 09:00", 0, 0, 0, nil)
	if !strings.Contains(desc, "Total terbuka : 0") {
		t.Errorf("jumlah kosong salah: %q", desc)
	}
	if !strings.Contains(notes, "tidak ada tiket terbuka") {
		t.Errorf("empty state hilang: %q", notes)
	}
}
