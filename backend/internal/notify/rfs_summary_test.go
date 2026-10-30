package notify

import (
	"strings"
	"testing"
)

// TestBuildRFSSummary memverifikasi ringkasan Aktivasi/EWO memuat jumlah per
// kelompok dan daftar barisnya.
func TestBuildRFSSummary(t *testing.T) {
	lines := []string{
		"— Direncanakan (1) —",
		"• RFS-2026-0001 PT Contoh [Aktivasi] — noc1 — 05 Nov 2026 10:00 · #urgent",
		"— Sedang dikerjakan (1) —",
		"• RFS-2026-0002 PT Dua [Perubahan Layanan] — field1 — 06 Nov 2026 09:00 [TERLAMBAT]",
	}
	desc, notes := BuildRFSSummary("30 Okt 2026 09:00", 1, 1, 0, lines)
	for _, want := range []string{
		"30 Okt 2026 09:00",
		"Direncanakan : 1",
		"Sedang dikerjakan : 1",
		"Menunggu troubleshooting : 0",
		"Total terbuka : 2",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description tidak memuat %q: %q", want, desc)
		}
	}
	for _, want := range []string{"RFS-2026-0001", "PT Contoh", "Perubahan Layanan", "[TERLAMBAT]"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes tidak memuat %q: %q", want, notes)
		}
	}
}

// TestBuildRFSSummaryEmpty memastikan state kosong tetap informatif.
func TestBuildRFSSummaryEmpty(t *testing.T) {
	desc, notes := BuildRFSSummary("30 Okt 2026 09:00", 0, 0, 0, nil)
	if !strings.Contains(desc, "Total terbuka : 0") {
		t.Errorf("jumlah kosong salah: %q", desc)
	}
	if !strings.Contains(notes, "tidak ada Aktivasi/EWO terbuka") {
		t.Errorf("empty state hilang: %q", notes)
	}
}

// TestFormatRFSSummaryLine memverifikasi kelengkapan kolom satu baris:
// ref, judul, customer, kategori, PIC/owner, jadwal, terlambat, dan tag.
func TestFormatRFSSummaryLine(t *testing.T) {
	line := FormatRFSSummaryLine(RFSSummaryRow{
		RefNo:    "RFS-2026-0009",
		Title:    "PT Sembilan",
		Customer: "PT Sembilan",
		Category: "Aktivasi",
		Owner:    "noc2",
		Schedule: "07 Nov 2026 08:00",
		Overdue:  true,
		Tags:     []string{"#urgent", "survey", "URGENT"},
	})
	for _, want := range []string{
		"RFS-2026-0009", "PT Sembilan", "[Aktivasi]", "noc2",
		"07 Nov 2026 08:00", "[TERLAMBAT]", "#urgent", "#survey",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("baris tidak memuat %q: %q", want, line)
		}
	}
	// Tag duplikat (tanpa memandang besar-kecil huruf) dan tanda pagar ganda
	// harus dibersihkan.
	if n := strings.Count(strings.ToLower(line), "#urgent"); n != 1 {
		t.Errorf("tag duplikat tidak dibersihkan (n=%d): %q", n, line)
	}
}

// TestFormatRFSSummaryLineMinimal memastikan field kosong tidak menyisakan
// pemisah menggantung.
func TestFormatRFSSummaryLineMinimal(t *testing.T) {
	line := FormatRFSSummaryLine(RFSSummaryRow{RefNo: "RFS-2026-0010"})
	if strings.Contains(line, "—  —") || strings.Contains(line, "[]") || strings.Contains(line, "·") {
		t.Errorf("baris minimal memuat pemisah berlebih: %q", line)
	}
	if !strings.Contains(line, "RFS-2026-0010") {
		t.Errorf("baris kehilangan ref: %q", line)
	}
}
