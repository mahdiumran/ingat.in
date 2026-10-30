package api

import "testing"

// TestTicketClaimStatus memverifikasi status tujuan saat tiket tanpa owner
// diambil (self-claim): hanya transisi sah yang dipakai, tanpa memaksa status
// yang tidak diizinkan workflow.
func TestTicketClaimStatus(t *testing.T) {
	tests := []struct {
		itemType string
		current  string
		want     string
	}{
		// Incident: new/assigned boleh langsung in_progress.
		{"incident", "new", "in_progress"},
		{"incident", "assigned", "in_progress"},
		// Request: sama seperti incident.
		{"request", "new", "in_progress"},
		{"request", "assigned", "in_progress"},

		// Change: draft/review belum boleh lompat ke implementing; approved dan
		// scheduled harus lewat status review/scheduled dulu.
		{"change", "draft", ""},
		{"change", "review", ""},
		{"change", "approved", ""},
		{"change", "scheduled", "implementing"},

		// Status yang sudah di tujuan tidak diubah.
		{"incident", "in_progress", ""},
		{"request", "in_progress", ""},
		{"change", "implementing", ""},

		// RFS (Aktivasi/EWO): planned/postponed yang mulai dikerjakan tidak
		// boleh tertinggal di planned.
		{"rfs", "planned", "in_progress"},
		{"rfs", "postponed", "in_progress"},
		{"rfs", "in_progress", ""},
		{"rfs", "in_progress_field", ""},
		// State terminal RFS tidak pernah dimajukan otomatis.
		{"rfs", "activated", ""},
		{"rfs", "cancelled", ""},

		// reopened/pending: transisi sah menurut workflow, jadi tetap dimajukan
		// ke in_progress (tiket kembali dikerjakan).
		{"incident", "pending_customer", "in_progress"},
		{"incident", "resolved", "in_progress"},

		// State terminal tidak pernah dimajukan otomatis.
		{"incident", "closed", ""},
		{"request", "fulfilled", ""},
		{"change", "completed", ""},
		{"change", "cancelled", ""},
	}
	for _, tc := range tests {
		if got := ticketClaimStatus(tc.itemType, tc.current); got != tc.want {
			t.Errorf("ticketClaimStatus(%q, %q) = %q, want %q", tc.itemType, tc.current, got, tc.want)
		}
	}
}

// TestTicketClaimStatusNonTicket memastikan tipe non-tiket tidak mendapat
// status otomatis walau punya status bernama sama.
func TestTicketClaimStatusNonTicket(t *testing.T) {
	for _, itemType := range []string{"task", "daily_task", "reminder", ""} {
		if got := ticketClaimStatus(itemType, "new"); got != "" {
			t.Errorf("ticketClaimStatus(%q, new) = %q, want kosong", itemType, got)
		}
	}
}
