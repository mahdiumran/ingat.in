// Package kpi menghitung agregat KPI & SLA (F20).
//
// Semua perhitungan memakai waktu kalender 24 jam. SLA tiap siklus dihitung
// sebagai "penyelesaian" (reopen tidak dipisah metriknya). Kredit per person
// diberikan SETARA untuk owner maupun collaborator.
package kpi

import (
	"sort"
	"time"

	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sla"
)

// Result adalah keluaran lengkap KPI untuk sebuah periode.
type Result struct {
	Period     Period                     `json:"period"`
	Summary    repository.KPISummary      `json:"summary"`
	ByPriority []repository.KPIByPriority `json:"by_priority"`
	Trend      []repository.KPITrendPoint `json:"trend"`
	PerPerson  []repository.KPIPerson     `json:"per_person"`
}

// Period adalah rentang waktu KPI.
type Period struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Compute menghitung KPI dari data mentah repository.
func Compute(from, to time.Time, cycles []repository.KPICycleRow, collabs []repository.KPICollaboratorRow, tasks []repository.KPISimpleTaskRow, now time.Time) Result {
	res := Result{Period: Period{From: from, To: to}}

	// Peta kolaborator per work item.
	collabByItem := map[string][]string{}
	for _, c := range collabs {
		collabByItem[c.WorkItemID] = append(collabByItem[c.WorkItemID], c.Username)
	}

	// --- Ringkasan keseluruhan + per prioritas + tren ---
	byPriority := map[string]*repository.KPIByPriority{}
	trendByDay := map[string]*repository.KPITrendPoint{}
	var resolutionSecs []int64
	seenTickets := map[string]bool{}

	for _, c := range cycles {
		targets := targetsFor(c)
		st := sla.Evaluate(c.OpenedAt, c.FirstResponseAt, c.ClosedAt, targets, now)

		// Ringkasan.
		if !seenTickets[c.WorkItemID] {
			seenTickets[c.WorkItemID] = true
			res.Summary.TicketsTotal++
			if c.ClosedAt == nil {
				res.Summary.TicketsOpen++
			}
		}
		if c.ClosedAt != nil {
			res.Summary.TicketsClosed++
		}
		res.Summary.ResponseTotal++
		if st.ResponseMet {
			res.Summary.ResponseMet++
		}
		res.Summary.ResolutionTotal++
		if st.ResolutionMet {
			res.Summary.ResolutionMet++
		} else {
			res.Summary.Breached++
		}
		if c.ClosedAt != nil {
			resolutionSecs = append(resolutionSecs, st.ResolutionSeconds)
		}

		// Per prioritas.
		p := c.Priority
		if p == "" {
			p = "normal"
		}
		bp := byPriority[p]
		if bp == nil {
			bp = &repository.KPIByPriority{Priority: p}
			byPriority[p] = bp
		}
		bp.Total++
		if st.ResponseMet {
			bp.ResponseMetPct++ // sementara: hitung jumlah
		}
		if st.ResolutionMet {
			bp.ResolutionMetPct++
		}
		bp.AvgResolutionSec += float64(st.ResolutionSeconds)

		// Tren per hari (berdasarkan closed_at, fallback opened_at).
		day := c.OpenedAt
		if c.ClosedAt != nil {
			day = *c.ClosedAt
		}
		key := day.In(wib).Format("2006-01-02")
		tp := trendByDay[key]
		if tp == nil {
			tp = &repository.KPITrendPoint{Day: key}
			trendByDay[key] = tp
		}
		if c.ClosedAt != nil {
			tp.Closed++
			if st.ResolutionMet {
				tp.ResolutionMet++
			}
		}
	}

	// Finalisasi persentase ringkasan.
	res.Summary.ResponseMetPct = pct(res.Summary.ResponseMet, res.Summary.ResponseTotal)
	res.Summary.ResolutionMetPct = pct(res.Summary.ResolutionMet, res.Summary.ResolutionTotal)
	res.Summary.SLAScore = round1(0.5*res.Summary.ResponseMetPct + 0.5*res.Summary.ResolutionMetPct)
	if len(resolutionSecs) > 0 {
		res.Summary.AvgResolutionSec = avg(resolutionSecs)
		res.Summary.MedianResSec = median(resolutionSecs)
		res.Summary.P90ResSec = percentile(resolutionSecs, 90)
	}

	// Finalisasi per prioritas.
	for _, bp := range byPriority {
		total := float64(bp.Total)
		bp.ResponseMetPct = round1(100 * bp.ResponseMetPct / total)
		bp.ResolutionMetPct = round1(100 * bp.ResolutionMetPct / total)
		bp.AvgResolutionSec = bp.AvgResolutionSec / total
		res.ByPriority = append(res.ByPriority, *bp)
	}
	sort.Slice(res.ByPriority, func(i, j int) bool { return res.ByPriority[i].Priority < res.ByPriority[j].Priority })

	// Tren terurut.
	for _, tp := range trendByDay {
		if tp.Closed > 0 {
			tp.ResolutionMetPct = round1(100 * float64(tp.ResolutionMet) / float64(tp.Closed))
		}
		res.Trend = append(res.Trend, *tp)
	}
	sort.Slice(res.Trend, func(i, j int) bool { return res.Trend[i].Day < res.Trend[j].Day })

	// --- Per person ---
	res.PerPerson = computePerPerson(cycles, collabByItem, tasks, now)

	// --- Todo/Daily ringkasan ketepatan waktu ---
	for _, t := range tasks {
		onTime := taskOnTime(t)
		switch t.ItemType {
		case "task":
			res.Summary.TodoTotal++
			if onTime {
				res.Summary.TodoOnTime++
			}
		case "daily_task":
			res.Summary.DailyTotal++
			if onTime {
				res.Summary.DailyOnTime++
			}
		}
	}
	res.Summary.TodoOnTimePct = round1(pct(res.Summary.TodoOnTime, res.Summary.TodoTotal))
	res.Summary.DailyOnTimePct = round1(pct(res.Summary.DailyOnTime, res.Summary.DailyTotal))

	return res
}

// computePerPerson mengagregasi KPI per username (owner ∪ collaborator, setara).
func computePerPerson(cycles []repository.KPICycleRow, collabByItem map[string][]string, tasks []repository.KPISimpleTaskRow, now time.Time) []repository.KPIPerson {
	people := map[string]*repository.KPIPerson{}

	get := func(username string) *repository.KPIPerson {
		p := people[username]
		if p == nil {
			p = &repository.KPIPerson{Username: username}
			people[username] = p
		}
		return p
	}

	for _, c := range cycles {
		targets := targetsFor(c)
		st := sla.Evaluate(c.OpenedAt, c.FirstResponseAt, c.ClosedAt, targets, now)

		// Kumpulan penerima kredit: owner + collaborator (unik).
		recipients := map[string]bool{}
		if c.OwnerUsername != "" {
			recipients[c.OwnerUsername] = true
		}
		for _, u := range collabByItem[c.WorkItemID] {
			recipients[u] = true
		}
		if len(recipients) == 0 {
			continue
		}

		for u := range recipients {
			p := get(u)
			if c.CycleNo == 0 {
				p.AssignedTotal++
			}
			if c.ClosedAt != nil {
				p.ResolvedTotal++
				if st.ResolutionMet {
					p.ResolutionMet++
				} else {
					p.Breached++
				}
				p.AvgResolutionSeconds += float64(st.ResolutionSeconds)
			} else {
				p.OpenTotal++
			}
			p.ResponseTotal++
			if st.ResponseMet {
				p.ResponseMet++
			}
			p.ResolutionTotal++
		}
	}

	for _, t := range tasks {
		if t.Owner == "" {
			continue
		}
		p := get(t.Owner)
		onTime := taskOnTime(t)
		if t.ItemType == "task" {
			p.TodoTotal++
			if onTime {
				p.TodoOnTime++
			}
		} else if t.ItemType == "daily_task" {
			p.DailyTotal++
			if onTime {
				p.DailyOnTime++
			}
		}
	}

	out := make([]repository.KPIPerson, 0, len(people))
	for _, p := range people {
		p.ResponseMetPct = round1(pct(p.ResponseMet, p.ResponseTotal))
		p.ResolutionMetPct = round1(pct(p.ResolutionMet, p.ResolutionTotal))
		if p.ResolvedTotal > 0 {
			p.AvgResolutionSeconds = p.AvgResolutionSeconds / float64(p.ResolvedTotal)
		}
		p.SLAScore = round1(0.5*p.ResponseMetPct + 0.5*p.ResolutionMetPct)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SLAScore != out[j].SLAScore {
			return out[i].SLAScore > out[j].SLAScore
		}
		return out[i].ResolvedTotal > out[j].ResolvedTotal
	})
	return out
}

// taskOnTime menentukan apakah Todo/Daily selesai sebelum tenggat.
func taskOnTime(t repository.KPISimpleTaskRow) bool {
	if t.Status == "canceled" || t.Status == "cancelled" {
		return false
	}
	if t.DueAt == nil {
		return t.ClosedAt != nil // tanpa tenggat: selesai = tepat waktu
	}
	if t.ClosedAt == nil {
		return time.Now().UTC().Before(*t.DueAt) // berjalan & belum lewat
	}
	return t.ClosedAt.Before(*t.DueAt) || t.ClosedAt.Equal(*t.DueAt)
}

func targetsFor(c repository.KPICycleRow) sla.Targets {
	t := sla.Targets{FirstResponseMinutes: c.TargetResponse, ResolutionMinutes: c.TargetResolution}
	if t.FirstResponseMinutes == 0 || t.ResolutionMinutes == 0 {
		return sla.TargetsForPriority(c.Priority)
	}
	return t
}

var wib = time.FixedZone("WIB", 7*3600)

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func avg(v []int64) float64 {
	var s int64
	for _, x := range v {
		s += x
	}
	return float64(s) / float64(len(v))
}

func median(v []int64) float64 {
	return percentile(v, 50)
}

func percentile(v []int64, p int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64{}, v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(float64(p) / 100 * float64(len(s)-1))
	return float64(s[idx])
}
