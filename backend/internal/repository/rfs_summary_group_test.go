package repository

import (
	"testing"
	"time"
)

// TestGroupRFSSummary memverifikasi pengelompokan status RFS:
// planned+postponed, in_progress+in_progress_field, pending_troubleshoot.
func TestGroupRFSSummary(t *testing.T) {
	now := time.Now()
	rows := []RFSSummaryRow{
		{RefNo: "RFS-1", Status: "planned"},
		{RefNo: "RFS-2", Status: "postponed"},
		{RefNo: "RFS-3", Status: "in_progress", Schedule: &now, Overdue: true, Tags: []string{"urgent"}},
		{RefNo: "RFS-4", Status: "in_progress_field"},
		{RefNo: "RFS-5", Status: "pending_troubleshoot"},
		{RefNo: "RFS-6", Status: "activated"},
		{RefNo: "RFS-7", Status: "cancelled"},
	}

	sum := GroupRFSSummary(rows)
	if len(sum.Planned) != 2 {
		t.Errorf("Planned = %d, want 2", len(sum.Planned))
	}
	if len(sum.InProgress) != 2 {
		t.Errorf("InProgress = %d, want 2", len(sum.InProgress))
	}
	if len(sum.Pending) != 1 {
		t.Errorf("Pending = %d, want 1", len(sum.Pending))
	}
	if sum.Total() != 5 {
		t.Errorf("Total = %d, want 5", sum.Total())
	}

	// Field harus diteruskan apa adanya.
	it := sum.InProgress[0]
	if it.RefNo != "RFS-3" || !it.Overdue || it.Schedule == nil || len(it.Tags) != 1 {
		t.Errorf("baris in_progress tidak utuh: %+v", it)
	}
}

// TestGroupRFSSummaryUnknownStatusFallsToPlanned memastikan status tak dikenal
// tidak hilang dari ringkasan (masuk kelompok Direncanakan).
func TestGroupRFSSummaryUnknownStatusFallsToPlanned(t *testing.T) {
	sum := GroupRFSSummary([]RFSSummaryRow{{RefNo: "RFS-X", Status: "weird"}})
	if len(sum.Planned) != 1 || sum.Total() != 1 {
		t.Errorf("status tak dikenal harus masuk Planned: %+v", sum)
	}
}

// TestGroupRFSSummaryEmpty memastikan input kosong aman.
func TestGroupRFSSummaryEmpty(t *testing.T) {
	sum := GroupRFSSummary(nil)
	if sum.Total() != 0 {
		t.Errorf("Total = %d, want 0", sum.Total())
	}
}
