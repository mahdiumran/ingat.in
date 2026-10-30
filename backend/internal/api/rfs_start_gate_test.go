package api

import (
	"strings"
	"testing"
	"time"

	"ingatin/backend/internal/models"
)

// TestRFSStartGate memverifikasi aturan jadwal untuk tombol "Mulai Kerjakan":
// RFS baru boleh dikerjakan setelah expire_at (jadwal RFS) tiba.
func TestRFSStartGate(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	rfs := func(status string, expire *time.Time) *models.WorkItem {
		return &models.WorkItem{ItemType: models.ItemRFS, Status: status, ExpireAt: expire}
	}

	tests := []struct {
		name  string
		item  *models.WorkItem
		block bool
	}{
		{"new dengan jadwal lampau", rfs("new", &past), false},
		{"new dengan jadwal mendatang", rfs("new", &future), true},
		{"new tanpa jadwal", rfs("new", nil), false},
		{"planned legacy dengan jadwal mendatang", rfs("planned", &future), true},
		{"planned legacy dengan jadwal lampau", rfs("planned", &past), false},
		// Status selain state awal tidak dibatasi jadwal: RFS yang sudah
		// dikerjakan tidak boleh tersangkut hanya karena jadwal diubah.
		{"in_progress dengan jadwal mendatang", rfs("in_progress", &future), false},
		{"in_progress_field dengan jadwal mendatang", rfs("in_progress_field", &future), false},
		{"pending_troubleshoot dengan jadwal mendatang", rfs("pending_troubleshoot", &future), false},
		// Bukan RFS: gate tidak berlaku.
		{"tiket incident", &models.WorkItem{ItemType: models.ItemIncident, Status: "new", ExpireAt: &future}, false},
		{"item nil", nil, false},
	}
	for _, tc := range tests {
		at, blocked := rfsStartGate(tc.item, now)
		if blocked != tc.block {
			t.Errorf("%s: blocked = %v, want %v", tc.name, blocked, tc.block)
		}
		if blocked && at.IsZero() {
			t.Errorf("%s: jadwal harus dikembalikan saat diblokir", tc.name)
		}
	}
}

// TestRFSStartGateMessage memastikan pesan penolakan menyebut jadwal dalam WIB.
func TestRFSStartGateMessage(t *testing.T) {
	at := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC) // 10:00 WIB
	msg := rfsStartGateMessage(at.In(wibLocation))
	if msg == "" {
		t.Fatal("pesan penolakan kosong")
	}
	for _, want := range []string{"05 Nov 2026", "10:00", "WIB", "Mulai Kerjakan"} {
		if !strings.Contains(msg, want) {
			t.Errorf("pesan tidak memuat %q: %q", want, msg)
		}
	}
}
