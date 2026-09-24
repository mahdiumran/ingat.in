package sla

import (
	"testing"
	"time"
)

func TestTargetsForPriority(t *testing.T) {
	if got := TargetsForPriority("critical"); got.FirstResponseMinutes != 10 || got.ResolutionMinutes != 120 {
		t.Errorf("critical = %+v", got)
	}
	if got := TargetsForPriority("unknown"); got != DefaultTargets {
		t.Errorf("fallback = %+v, want %+v", got, DefaultTargets)
	}
}

func TestEvaluateMet(t *testing.T) {
	opened := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	resp := opened.Add(5 * time.Minute)
	closed := opened.Add(60 * time.Minute)
	now := opened.Add(2 * time.Hour)

	st := Evaluate(opened, &resp, &closed, Targets{15, 240}, now)
	if !st.ResponseDone || !st.ResponseMet {
		t.Errorf("respons harus met: %+v", st)
	}
	if st.Running || !st.ResolutionMet || st.Breached {
		t.Errorf("penyelesaian harus met: %+v", st)
	}
	if st.ResolutionSeconds != 3600 {
		t.Errorf("resolution = %d, want 3600", st.ResolutionSeconds)
	}
}

func TestEvaluateBreachedRunning(t *testing.T) {
	opened := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	now := opened.Add(5 * time.Hour) // melewati target 240 menit

	st := Evaluate(opened, nil, nil, Targets{15, 240}, now)
	if st.ResponseMet {
		t.Error("respons tanpa respons pertama melewati target harus tidak met")
	}
	if !st.Running || !st.Breached || st.ResolutionMet {
		t.Errorf("harus breached & running: %+v", st)
	}
}

func TestEvaluateCycleUsesOpenedAt(t *testing.T) {
	opened := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	resp := opened.Add(20 * time.Minute)
	st := Evaluate(opened, &resp, nil, Targets{15, 240}, opened.Add(30*time.Minute))
	if st.ResponseMet {
		t.Error("respons 20 menit > target 15 menit harus tidak met")
	}
}
