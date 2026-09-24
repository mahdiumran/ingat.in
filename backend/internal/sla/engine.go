// Package sla berisi mesin perhitungan SLA tiket (F20).
//
// SLA dihitung PER SIKLUS (ticket_sla_cycles):
//   - Response  = first_response_at − opened_at
//   - Resolution= closed_at − opened_at (atau now bila berjalan)
//
// Siklus 0 adalah pekerjaan awal; siklus 1.. adalah setiap reopen. Metrik
// reopen tidak dipisah — semuanya dihitung sebagai SLA penyelesaian.
package sla

import (
	"time"

	"ingatin/backend/internal/models"
)

// Targets adalah target SLA (menit) untuk sebuah policy.
type Targets struct {
	FirstResponseMinutes int
	ResolutionMinutes    int
}

// DefaultTargets dipakai bila policy/target tidak ditemukan di database.
//
// Nilainya setara SLA-NOC-DEFAULT (respons 15 menit, penyelesaian 240 menit).
var DefaultTargets = Targets{FirstResponseMinutes: 15, ResolutionMinutes: 240}

// PriorityTargets adalah target per prioritas (kalender 24 jam).
var PriorityTargets = map[string]Targets{
	"critical": {FirstResponseMinutes: 10, ResolutionMinutes: 120},
	"high":     {FirstResponseMinutes: 15, ResolutionMinutes: 240},
	"normal":   {FirstResponseMinutes: 30, ResolutionMinutes: 480},
	"low":      {FirstResponseMinutes: 60, ResolutionMinutes: 960},
}

// TargetsForPriority mengembalikan target untuk sebuah prioritas (fallback default).
func TargetsForPriority(priority string) Targets {
	if t, ok := PriorityTargets[priority]; ok {
		return t
	}
	return DefaultTargets
}

// Status adalah hasil evaluasi SLA satu siklus.
type Status struct {
	// ResponseSeconds = durasi hingga respons pertama (0 bila belum ada).
	ResponseSeconds int64
	// ResponseMet = respons ≤ target (true bila belum ada respons & belum lewat).
	ResponseMet bool
	// ResponseDone = respons sudah tercatat.
	ResponseDone bool

	// ResolutionSeconds = durasi siklus (berjalan atau final).
	ResolutionSeconds int64
	// ResolutionMet = penyelesaian ≤ target.
	ResolutionMet bool
	// Running = siklus belum ditutup.
	Running bool
	// Breached = siklus sudah melewati target penyelesaian.
	Breached bool
}

// Evaluate menghitung status SLA sebuah siklus terhadap target.
func Evaluate(openedAt time.Time, firstResponseAt, closedAt *time.Time, t Targets, now time.Time) Status {
	var st Status

	// --- Response ---
	responseTarget := time.Duration(t.FirstResponseMinutes) * time.Minute
	if firstResponseAt != nil {
		st.ResponseDone = true
		st.ResponseSeconds = clampSeconds(firstResponseAt.Sub(openedAt))
		st.ResponseMet = st.ResponseSeconds <= int64(responseTarget.Seconds())
	} else {
		// Belum ada respons: met bila belum melewati target.
		st.ResponseSeconds = clampSeconds(now.Sub(openedAt))
		st.ResponseMet = now.Sub(openedAt) <= responseTarget
	}

	// --- Resolution ---
	resolutionTarget := time.Duration(t.ResolutionMinutes) * time.Minute
	st.Running = closedAt == nil
	end := now
	if closedAt != nil {
		end = *closedAt
	}
	st.ResolutionSeconds = clampSeconds(end.Sub(openedAt))
	st.ResolutionMet = st.ResolutionSeconds <= int64(resolutionTarget.Seconds())
	st.Breached = !st.ResolutionMet

	return st
}

// EvaluateCycle menghitung status satu siklus SLA.
func EvaluateCycle(c models.SLACycle, t Targets, now time.Time) Status {
	return Evaluate(c.OpenedAt, c.FirstResponseAt, c.ClosedAt, t, now)
}

func clampSeconds(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d.Seconds())
}

// SLAStateFromStatus memetakan hasil evaluasi ke nilai kolom work_items.sla_state.
//
// Hanya "on_track", "at_risk", "breached" yang dikembalikan; "none"/"paused"
// ditangani pemanggil.
func SLAStateFromStatus(st Status, warningPct int) string {
	if st.Breached {
		return models.SLAStateBreached
	}
	if warningPct <= 0 {
		warningPct = 80
	}
	return models.SLAStateOnTrack
}
