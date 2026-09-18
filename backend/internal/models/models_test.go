package models

import (
	"testing"
	"time"
)

func TestComputeSLATicket(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 1, 13, 30, 0, 0, time.UTC) // +3.5 jam

	closed := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) // +2 jam

	// Berjalan: belum closed → pakai now.
	running := &WorkItem{ItemType: ItemIncident, FirstResponseAt: &base}
	running.ComputeSLA(now)
	if !running.SLARunning {
		t.Error("tiket tanpa closed_at harus berjalan (running)")
	}
	if running.SLASeconds != 12600 { // 3.5 jam
		t.Errorf("sla_seconds berjalan = %d, want 12600", running.SLASeconds)
	}

	// Final: sudah closed → durasi tetap.
	done := &WorkItem{ItemType: ItemIncident, FirstResponseAt: &base, ClosedAt: &closed}
	done.ComputeSLA(now)
	if done.SLARunning {
		t.Error("tiket sudah closed tidak boleh running")
	}
	if done.SLASeconds != 7200 { // 2 jam
		t.Errorf("sla_seconds final = %d, want 7200", done.SLASeconds)
	}

	// Non-tiket → SLA nol.
	task := &WorkItem{ItemType: ItemTask, FirstResponseAt: &base}
	task.ComputeSLA(now)
	if task.SLASeconds != 0 || task.SLARunning {
		t.Errorf("task tidak boleh punya SLA, got %d running=%v", task.SLASeconds, task.SLARunning)
	}

	// Tiket tanpa first_response_at → belum mulai, SLA nol.
	notstarted := &WorkItem{ItemType: ItemIncident}
	notstarted.ComputeSLA(now)
	if notstarted.SLASeconds != 0 || notstarted.SLAStartAt != nil {
		t.Error("tiket belum ditangani tidak boleh punya SLA")
	}
}

func TestIsTicket(t *testing.T) {
	tickets := []string{ItemIncident, ItemRequest, ItemChange}
	for _, it := range tickets {
		if !(&WorkItem{ItemType: it}).IsTicket() {
			t.Errorf("%s harus dianggap tiket", it)
		}
	}
	others := []string{ItemTask, ItemReminder, ItemRFS}
	for _, it := range others {
		if (&WorkItem{ItemType: it}).IsTicket() {
			t.Errorf("%s tidak boleh dianggap tiket", it)
		}
	}
}
