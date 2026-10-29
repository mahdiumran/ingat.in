package kpi

import "testing"

// TestPctRounding memastikan persentase dibulatkan 1 desimal (F42).
func TestPctRounding(t *testing.T) {
	if got := pct(4, 12); got != 33.3 {
		t.Errorf("pct(4,12) = %v, want 33.3", got)
	}
	if got := pct(1, 3); got != 33.3 {
		t.Errorf("pct(1,3) = %v, want 33.3", got)
	}
	if got := pct(2, 3); got != 66.7 {
		t.Errorf("pct(2,3) = %v, want 66.7", got)
	}
	if got := pct(0, 0); got != 0 {
		t.Errorf("pct(0,0) = %v, want 0", got)
	}
	if got := pct(1, 1); got != 100 {
		t.Errorf("pct(1,1) = %v, want 100", got)
	}
}
