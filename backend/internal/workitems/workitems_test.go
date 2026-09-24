package workitems

import (
	"testing"
	"time"
)

func TestPrefixFor(t *testing.T) {
	cases := map[string]string{
		"task":       PrefixTask,
		"reminder":   PrefixReminder,
		"rfs":        PrefixRFS,
		"incident":   PrefixIncident,
		"request":    PrefixRequest,
		"change":     PrefixChange,
		"daily_task": PrefixDailyTask,
		"lain":       PrefixDefault,
		"":           PrefixDefault,
	}
	for in, want := range cases {
		if got := PrefixFor(in); got != want {
			t.Errorf("PrefixFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatRefNo(t *testing.T) {
	cases := []struct {
		prefix string
		year   int
		seq    int64
		want   string
	}{
		{"INC", 2026, 1, "INC-2026-0001"},
		{"TSK", 2026, 42, "TSK-2026-0042"},
		{"RFS", 2026, 9999, "RFS-2026-9999"},
		{"REM", 2026, 10000, "REM-2026-10000"},
	}
	for _, tc := range cases {
		if got := FormatRefNo(tc.prefix, tc.year, tc.seq); got != tc.want {
			t.Errorf("FormatRefNo(%q,%d,%d) = %q, want %q", tc.prefix, tc.year, tc.seq, got, tc.want)
		}
	}
}

func TestWorkflowTaskTransitions(t *testing.T) {
	wf := WorkflowFor("task")
	if wf == nil {
		t.Fatal("workflow task tidak ditemukan")
	}
	if wf.InitialState != "accepted" {
		t.Errorf("initial state task = %q, want accepted", wf.InitialState)
	}

	allowed := [][2]string{
		{"accepted", "on_progress"},
		{"accepted", "expired"},
		{"accepted", "canceled"},
		{"accepted", "closed"},
		{"on_progress", "closed"},
		{"on_progress", "canceled"},
		{"on_progress", "expired"},
		{"expired", "accepted"},
		{"expired", "closed"},
		{"canceled", "accepted"},
		{"closed", "accepted"},
	}
	for _, p := range allowed {
		if !wf.CanTransition(p[0], p[1]) {
			t.Errorf("transisi %s -> %s seharusnya DIIZINKAN", p[0], p[1])
		}
	}

	denied := [][2]string{
		{"accepted", "blocked"},
		{"on_progress", "accepted"},
		{"canceled", "on_progress"},
		{"closed", "on_progress"},
	}
	for _, p := range denied {
		if wf.CanTransition(p[0], p[1]) {
			t.Errorf("transisi %s -> %s seharusnya DITOLAK", p[0], p[1])
		}
	}

	// from == to selalu diizinkan (no-op).
	if !wf.CanTransition("accepted", "accepted") {
		t.Error("transisi accepted -> accepted seharusnya diizinkan")
	}
}

func TestWorkflowRFSHasActivatePath(t *testing.T) {
	wf := WorkflowFor("rfs")
	if wf == nil {
		t.Fatal("workflow rfs tidak ditemukan")
	}
	// Alur "Tandai Aktif" harus tercapai dari planned.
	if !wf.CanTransition("planned", "in_progress") {
		t.Error("rfs planned -> in_progress harus diizinkan")
	}
	if !wf.CanTransition("in_progress", "activated") {
		t.Error("rfs in_progress -> activated harus diizinkan")
	}
	if len(wf.Transitions["activated"]) != 0 {
		t.Error("activated harus terminal (tanpa transisi keluar)")
	}
	res := wf.EvaluateTransitions("in_progress", "activated")
	if !res.Allowed || !res.IsClosing {
		t.Errorf("rfs -> activated harus closing, got allowed=%v closing=%v", res.Allowed, res.IsClosing)
	}
}

func TestWorkflowReminderLifecycle(t *testing.T) {
	wf := WorkflowFor("reminder")
	if wf == nil {
		t.Fatal("workflow reminder tidak ditemukan")
	}
	// scheduled -> active -> expiring -> expired
	if !wf.CanTransition("scheduled", "active") {
		t.Error("reminder scheduled -> active harus diizinkan")
	}
	if !wf.CanTransition("active", "expiring") {
		t.Error("reminder active -> expiring harus diizinkan")
	}
	if !wf.CanTransition("expiring", "expired") {
		t.Error("reminder expiring -> expired harus diizinkan")
	}
	// expired bisa diaktifkan kembali (trial diperpanjang).
	if !wf.CanTransition("expired", "active") {
		t.Error("reminder expired -> active harus diizinkan (perpanjangan)")
	}
}

func TestWorkflowIncidentReopen(t *testing.T) {
	wf := WorkflowFor("incident")
	if wf == nil {
		t.Fatal("workflow incident tidak ditemukan")
	}
	// closed -> in_progress = reopen.
	res := wf.EvaluateTransitions("closed", "in_progress")
	if !res.Allowed {
		t.Fatal("incident closed -> in_progress harus diizinkan (reopen)")
	}
	if !res.IsReopen {
		t.Error("incident closed -> in_progress harus ditandai reopen")
	}
	// resolved -> closed = closing terminal.
	res = wf.EvaluateTransitions("resolved", "closed")
	if !res.IsClosing {
		t.Error("incident resolved -> closed harus ditandai closing")
	}
}

func TestIsValidState(t *testing.T) {
	wf := WorkflowFor("task")
	if !wf.IsValidState("on_progress") {
		t.Error("on_progress harus state valid untuk task")
	}
	if wf.IsValidState("nonexistent") {
		t.Error("nonexistent tidak boleh state valid")
	}
}

func TestAllItemTypesHaveWorkflow(t *testing.T) {
	types := []string{"task", "reminder", "rfs", "incident", "request", "change", "daily_task"}
	for _, it := range types {
		wf := WorkflowFor(it)
		if wf == nil {
			t.Errorf("item_type %q tidak punya workflow", it)
			continue
		}
		if len(wf.States) == 0 {
			t.Errorf("workflow %q tidak punya state", it)
		}
		if wf.InitialState == "" {
			t.Errorf("workflow %q tidak punya initial_state", it)
		}
		if !wf.IsValidState(wf.InitialState) {
			t.Errorf("workflow %q: initial_state %q tidak ada di daftar states", it, wf.InitialState)
		}
		// Setiap state yang muncul di transitions harus terdaftar di states.
		for from, tos := range wf.Transitions {
			if !wf.IsValidState(from) {
				t.Errorf("workflow %q: state asal %q tidak terdaftar", it, from)
			}
			for _, to := range tos {
				if !wf.IsValidState(to) {
					t.Errorf("workflow %q: state tujuan %q (dari %q) tidak terdaftar", it, to, from)
				}
			}
		}
		// Setiap state harus punya entri transitions (boleh kosong = terminal).
		for _, s := range wf.States {
			if _, ok := wf.Transitions[s]; !ok {
				t.Errorf("workflow %q: state %q tidak punya entri transitions", it, s)
			}
		}
		for _, term := range wf.TerminalStates {
			if !wf.IsValidState(term) {
				t.Errorf("workflow %q: terminal state %q tidak terdaftar", it, term)
			}
		}
	}
}

func TestWorkflowDailyTaskLifecycle(t *testing.T) {
	wf := WorkflowFor("daily_task")
	if wf == nil {
		t.Fatal("workflow daily_task tidak ditemukan")
	}
	if wf.InitialState != "pending" {
		t.Errorf("initial state daily_task = %q, want pending", wf.InitialState)
	}
	// pending -> in_progress -> done (alur utama), plus lompat langsung pending -> done.
	allowed := [][2]string{
		{"pending", "in_progress"},
		{"pending", "done"},
		{"pending", "canceled"},
		{"in_progress", "done"},
		{"in_progress", "pending"},
		{"done", "pending"},
	}
	for _, p := range allowed {
		if !wf.CanTransition(p[0], p[1]) {
			t.Errorf("transisi %s -> %s seharusnya DIIZINKAN", p[0], p[1])
		}
	}
	denied := [][2]string{
		{"done", "in_progress"},
		{"canceled", "done"},
	}
	for _, p := range denied {
		if wf.CanTransition(p[0], p[1]) {
			t.Errorf("transisi %s -> %s seharusnya DITOLAK", p[0], p[1])
		}
	}
	if !wf.IsValidState("pending") || wf.IsValidState("open") {
		t.Error("state daily_task harus pending/in_progress/done/canceled")
	}
}

func TestValidEventTypesMatchConstants(t *testing.T) {
	// Semua konstanta event harus ada di whitelist (kalau tidak, AppendEvent menolak).
	all := []string{
		EventCreated, EventUpdated, EventStatusChanged, EventStageChanged,
		EventAssigned, EventUnassigned, EventEscalated, EventReopened,
		EventCommented, EventAttachmentAdded, EventDueChanged, EventExpireChanged,
		EventSLAWarning, EventSLABreached, EventSLAPaused, EventSLAResumed,
		EventResolved, EventClosed, EventCancelled, EventNotified, EventNotifyFailed,
		EventUnlocked,
	}
	for _, e := range all {
		if !validEventTypes[e] {
			t.Errorf("event %q tidak ada di whitelist validEventTypes", e)
		}
	}
	if len(validEventTypes) != len(all) {
		t.Errorf("whitelist punya %d entri, konstanta berjumlah %d — ada yang tidak sinkron",
			len(validEventTypes), len(all))
	}
}

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, []string{}},
		{[]string{}, []string{}},
		{[]string{"  "}, []string{}},
		{[]string{"noc", "NOC", "Noc"}, []string{"noc"}},
		{[]string{" trial ", "rfs"}, []string{"trial", "rfs"}},
		{[]string{"a", "", "b"}, []string{"a", "b"}},
	}
	for _, tc := range cases {
		got := NormalizeTags(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("NormalizeTags(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("NormalizeTags(%v) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestNextRefNoIntegrationGuard(t *testing.T) {
	// Tanpa database, NextRefNo tidak dapat diuji di sini. Test ini memastikan
	// kontrak format nomor referensi tetap stabil untuk baris pertama tahun baru.
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	want := "INC-2027-0001"
	if got := FormatRefNo("INC", now.Year(), 1); got != want {
		t.Errorf("nomor referensi pertama tahun baru = %q, want %q", got, want)
	}
}
