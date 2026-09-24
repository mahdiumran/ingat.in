package bot

import (
	"testing"
	"time"

	"ingatin/backend/internal/models"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		name string
		raw  string
		want bool
	}{
		{"/open Link HSP Down | Erpan", "open", "Link HSP Down | Erpan", true},
		{"/open@NocBot Link Down", "open", "Link Down", true},
		{"/LIST", "list", "", true},
		{"  /solved DTK-2026-0007  ", "solved", "DTK-2026-0007", true},
		{"/help", "help", "", true},
		{"hello world", "", "", false},
		{"", "", "", false},
		{"/", "", "", false},
	}
	for _, c := range cases {
		got, ok := parseCommand(c.in)
		if ok != c.want {
			t.Errorf("parseCommand(%q) ok=%v want %v", c.in, ok, c.want)
			continue
		}
		if !c.want {
			continue
		}
		if got.Name != c.name || got.Raw != c.raw {
			t.Errorf("parseCommand(%q) = {%q,%q} want {%q,%q}", c.in, got.Name, got.Raw, c.name, c.raw)
		}
	}
}

func TestSplitPipe(t *testing.T) {
	if got := splitPipe("Link Down | Erpan"); len(got) != 2 || got[0] != "Link Down" || got[1] != "Erpan" {
		t.Errorf("splitPipe = %v", got)
	}
	if got := splitPipe("Only Title"); len(got) != 1 || got[0] != "Only Title" {
		t.Errorf("splitPipe single = %v", got)
	}
}

func TestParseDay(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	d, ok := parseDay(loc, "23/05/2026")
	if !ok || d.Year() != 2026 || d.Month() != time.May || d.Day() != 23 || d.Hour() != 0 {
		t.Errorf("parseDay = %v ok=%v", d, ok)
	}
	if _, ok := parseDay(loc, "bukan tanggal"); ok {
		t.Errorf("parseDay should fail on invalid input")
	}
}

func TestCommandAllowedDefaultTrue(t *testing.T) {
	// meta kosong → semua diizinkan (default).
	if !commandAllowed(&models.MasterData{}, "open") {
		t.Error("empty meta should allow all commands")
	}
	if !commandAllowed(nil, "open") {
		t.Error("nil group should allow all commands")
	}
	// meta menonaktifkan open.
	g := &models.MasterData{Meta: map[string]any{"allow_open": false}}
	if commandAllowed(g, "open") {
		t.Error("allow_open=false should deny open")
	}
	if !commandAllowed(g, "list") {
		t.Error("unset key should default to allowed")
	}
}

func TestTargetState(t *testing.T) {
	cases := []struct {
		item  string
		solve bool
		want  string
	}{
		{models.ItemDailyTask, true, "done"},
		{models.ItemTask, true, "closed"},
		{models.ItemIncident, true, "resolved"},
		{models.ItemRequest, true, "fulfilled"},
		{models.ItemChange, true, "completed"},
		{models.ItemDailyTask, false, "waiting_customer"},
		{models.ItemIncident, false, "pending_customer"},
		{models.ItemRFS, true, ""},
	}
	for _, c := range cases {
		if got := targetState(c.item, c.solve); got != c.want {
			t.Errorf("targetState(%s, %v) = %q want %q", c.item, c.solve, got, c.want)
		}
	}
}

func TestDefaultDailyTaskDue(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	base := time.Date(2026, 5, 23, 14, 30, 0, 0, loc)
	due := defaultDailyTaskDue(base)
	if due == nil || due.Hour() != 23 || due.Minute() != 59 || due.Day() != 23 {
		t.Errorf("defaultDailyTaskDue = %v", due)
	}
}
