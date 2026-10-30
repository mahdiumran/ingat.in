package api

import (
	"strings"
	"testing"
	"time"

	"ingatin/backend/internal/models"
)

// TestRFSSchedulePending memverifikasi deteksi "jadwal belum tiba" untuk RFS.
//
// Kebijakan produk: jadwal BUKAN penghalang. Deteksi ini hanya dipakai untuk
// meminta alasan (pengecualian) sebelum RFS dikerjakan lebih awal.
func TestRFSSchedulePending(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	rfs := func(status string, expire *time.Time) *models.WorkItem {
		return &models.WorkItem{ItemType: models.ItemRFS, Status: status, ExpireAt: expire}
	}

	tests := []struct {
		name    string
		item    *models.WorkItem
		pending bool
	}{
		{"new dengan jadwal lampau", rfs("new", &past), false},
		{"new dengan jadwal mendatang", rfs("new", &future), true},
		{"new tanpa jadwal", rfs("new", nil), false},
		{"planned legacy dengan jadwal mendatang", rfs("planned", &future), true},
		{"planned legacy dengan jadwal lampau", rfs("planned", &past), false},
		// Status selain state awal tidak dianggap pending: pekerjaan yang sudah
		// berjalan tidak boleh tersangkut hanya karena jadwal diubah.
		{"in_progress dengan jadwal mendatang", rfs("in_progress", &future), false},
		{"in_progress_field dengan jadwal mendatang", rfs("in_progress_field", &future), false},
		{"pending_troubleshoot dengan jadwal mendatang", rfs("pending_troubleshoot", &future), false},
		// Bukan RFS: tidak berlaku.
		{"tiket incident", &models.WorkItem{ItemType: models.ItemIncident, Status: "new", ExpireAt: &future}, false},
		{"item nil", nil, false},
	}
	for _, tc := range tests {
		at, pending := rfsSchedulePending(tc.item, now)
		if pending != tc.pending {
			t.Errorf("%s: pending = %v, want %v", tc.name, pending, tc.pending)
		}
		if pending {
			if at.IsZero() {
				t.Errorf("%s: jadwal harus dikembalikan saat pending", tc.name)
			}
			if at.Location() != wibLocation {
				t.Errorf("%s: jadwal harus dalam WIB, dapat %v", tc.name, at.Location())
			}
		}
	}
}

// TestRFSEarlyStartMessage memastikan pesan menyebut jadwal dalam WIB sekaligus
// menegaskan bahwa aktivasi lebih awal TETAP BOLEH dengan alasan.
func TestRFSEarlyStartMessage(t *testing.T) {
	at := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC) // 10:00 WIB
	msg := rfsEarlyStartMessage(at.In(wibLocation))
	if msg == "" {
		t.Fatal("pesan kosong")
	}
	for _, want := range []string{"05 Nov 2026", "10:00", "WIB", "alasan"} {
		if !strings.Contains(msg, want) {
			t.Errorf("pesan tidak memuat %q: %q", want, msg)
		}
	}
}
