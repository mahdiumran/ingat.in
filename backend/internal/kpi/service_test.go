package kpi

import (
	"testing"
	"time"

	"ingatin/backend/internal/repository"
)

func TestComputeSummaryAndPerson(t *testing.T) {
	base := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	resp := base.Add(5 * time.Minute)
	closed := base.Add(60 * time.Minute)

	cycles := []repository.KPICycleRow{
		{
			WorkItemID: "w1", ItemType: "incident", Priority: "high",
			OwnerUsername: "budi", CycleNo: 0,
			OpenedAt: base, ClosedAt: &closed, FirstResponseAt: &resp,
			TargetResponse: 15, TargetResolution: 240,
		},
	}
	collabs := []repository.KPICollaboratorRow{{WorkItemID: "w1", Username: "sari"}}
	tasks := []repository.KPISimpleTaskRow{
		{ItemType: "task", Owner: "budi", DueAt: &closed, ClosedAt: &base, Status: "closed"},
		{ItemType: "daily_task", Owner: "sari", DueAt: &resp, ClosedAt: &closed, Status: "done"},
	}

	res := Compute(base, closed.Add(time.Hour), cycles, collabs, tasks, closed)

	if res.Summary.TicketsTotal != 1 || res.Summary.TicketsClosed != 1 {
		t.Errorf("summary tiket = %+v", res.Summary)
	}
	if res.Summary.ResolutionMetPct != 100 {
		t.Errorf("resolution met pct = %v, want 100", res.Summary.ResolutionMetPct)
	}
	if res.Summary.SLAScore != 100 {
		t.Errorf("skor = %v, want 100", res.Summary.SLAScore)
	}
	// Dua person: budi (owner + todo), sari (collaborator + daily).
	if len(res.PerPerson) != 2 {
		t.Fatalf("per_person = %d, want 2", len(res.PerPerson))
	}
	byName := map[string]repository.KPIPerson{}
	for _, p := range res.PerPerson {
		byName[p.Username] = p
	}
	if byName["sari"].SLAScore != 100 {
		t.Errorf("collaborator sari harus dapat kredit setara: %+v", byName["sari"])
	}
	if byName["budi"].TodoTotal != 1 || byName["budi"].TodoOnTime != 1 {
		t.Errorf("todo budi = %+v", byName["budi"])
	}
}

func TestTaskOnTime(t *testing.T) {
	due := time.Date(2026, 9, 10, 23, 59, 0, 0, time.UTC)
	early := due.Add(-time.Hour)
	late := due.Add(time.Hour)

	if !taskOnTime(repository.KPISimpleTaskRow{Status: "closed", DueAt: &due, ClosedAt: &early}) {
		t.Error("selesai sebelum tenggat harus tepat waktu")
	}
	if taskOnTime(repository.KPISimpleTaskRow{Status: "closed", DueAt: &due, ClosedAt: &late}) {
		t.Error("selesai setelah tenggat harus terlambat")
	}
	if taskOnTime(repository.KPISimpleTaskRow{Status: "canceled", DueAt: &due, ClosedAt: &early}) {
		t.Error("canceled tidak dihitung tepat waktu")
	}
}
