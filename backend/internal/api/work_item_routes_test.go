package api

import (
	"testing"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/workitems"
)

func TestCanManageItem(t *testing.T) {
	item := &models.WorkItem{
		CreatedBy:     "budi",
		OwnerUsername: "andi",
	}

	cases := []struct {
		name string
		user *models.User
		want bool
	}{
		{"nil user ditolak", nil, false},
		{"admin selalu boleh", &models.User{Username: "siapa", Role: models.RoleAdmin}, true},
		{"pembuat boleh", &models.User{Username: "budi", Role: models.RoleNOC}, true},
		{"owner boleh", &models.User{Username: "andi", Role: models.RoleAgent}, true},
		{"huruf besar kecil tidak masalah", &models.User{Username: "BUDI", Role: models.RoleSales}, true},
		{"orang lain ditolak", &models.User{Username: "citra", Role: models.RoleNOC}, false},
		{"username kosong ditolak", &models.User{Username: "", Role: models.RoleNOC}, false},
	}

	for _, tc := range cases {
		if got := canManageItem(tc.user, item); got != tc.want {
			t.Errorf("%s: canManageItem = %v, want %v", tc.name, got, tc.want)
		}
	}

	if canManageItem(&models.User{Username: "x", Role: models.RoleNOC}, nil) {
		t.Error("item nil seharusnya ditolak")
	}
}

func TestIsTerminalState(t *testing.T) {
	incident := workitems.WorkflowFor("incident")
	if !isTerminalState(incident, "closed") {
		t.Error("incident closed harus terminal")
	}
	if isTerminalState(incident, "in_progress") {
		t.Error("incident in_progress bukan terminal")
	}

	task := workitems.WorkflowFor("task")
	if !isTerminalState(task, "closed") || !isTerminalState(task, "canceled") {
		t.Error("task closed/canceled harus terminal")
	}
	if isTerminalState(task, "accepted") {
		t.Error("task accepted bukan terminal")
	}
}

func TestValidateTicketLevels(t *testing.T) {
	// Kosong = default, boleh.
	if err := validateTicketLevels("", ""); err != nil {
		t.Errorf("level kosong harus diterima: %v", err)
	}
	for _, ok := range []string{"low", "medium", "high"} {
		if err := validateTicketLevels(ok, ok); err != nil {
			t.Errorf("level %q harus diterima: %v", ok, err)
		}
	}
	err := validateTicketLevels("urgent", "high")
	if err == nil {
		t.Fatal("level 'urgent' harus ditolak")
	}
	if !isValidationErr(err) {
		t.Error("error level tidak sah harus ditandai validasi (untuk HTTP 400)")
	}
	if cleanErrMsg(err) == "" || cleanErrMsg(err) == errValidation.Error() {
		t.Errorf("pesan error bersih tidak sesuai: %q", cleanErrMsg(err))
	}
}

func TestParseWIBDay(t *testing.T) {
	// Tanggal eksplisit -> awal hari 00:00 WIB (UTC-nya 17:00 hari sebelumnya).
	got, err := parseWIBDay("2026-09-18")
	if err != nil {
		t.Fatalf("parseWIBDay gagal: %v", err)
	}
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Errorf("parseWIBDay harus awal hari, got %v", got)
	}
	// 00:00 WIB == 17:00 UTC hari sebelumnya.
	if u := got.UTC(); u.Hour() != 17 || u.Day() != 17 {
		t.Errorf("2026-09-18 00:00 WIB harus 17:00 UTC 17 Sep, got %v", u)
	}

	// Kosong -> hari ini WIB (bukan zero value).
	today, err := parseWIBDay("")
	if err != nil {
		t.Fatalf("parseWIBDay(\"\") gagal: %v", err)
	}
	if today.IsZero() {
		t.Error("parseWIBDay(\"\") tidak boleh zero")
	}
	if today.Hour() != 0 {
		t.Errorf("parseWIBDay(\"\") harus awal hari, got %v", today)
	}

	// Format salah -> error.
	if _, err := parseWIBDay("18-09-2026"); err == nil {
		t.Error("parseWIBDay dengan format salah harus error")
	}
}

func TestKnownItemTypeDailyTask(t *testing.T) {
	if !isKnownItemType(models.ItemDailyTask) {
		t.Error("daily_task harus item_type yang dikenal")
	}
}

func TestUpdateSheetParamsFrom(t *testing.T) {
	current := &models.SheetSyncConfig{
		Enabled: true, SpreadsheetID: "OLD_ID", SheetName: "Todo",
	}

	// Permintaan kosong harus mempertahankan nilai lama (PATCH semantics).
	p := updateSheetParamsFrom(current, sheetSyncRequest{})
	if !p.Enabled || p.SpreadsheetID != "OLD_ID" || p.SheetName != "Todo" {
		t.Errorf("permintaan kosong mengubah nilai: %+v", p)
	}
	if p.ServiceAccountEnc != nil {
		t.Error("service account tidak boleh diubah bila tidak dikirim")
	}

	// Field yang dikirim harus menimpa.
	off := false
	id := "NEW_ID"
	name := "Kerjaan NOC"
	p = updateSheetParamsFrom(current, sheetSyncRequest{
		Enabled: &off, SpreadsheetID: &id, SheetName: &name,
	})
	if p.Enabled || p.SpreadsheetID != "NEW_ID" || p.SheetName != "Kerjaan NOC" {
		t.Errorf("field yang dikirim tidak menimpa: %+v", p)
	}

	// Nama sheet kosong harus jatuh kembali ke nilai lama (bukan menghapus).
	empty := "   "
	p = updateSheetParamsFrom(current, sheetSyncRequest{SheetName: &empty})
	if p.SheetName != "Todo" {
		t.Errorf("nama sheet kosong harus mempertahankan nilai lama, got %q", p.SheetName)
	}
}

func TestIsSheetSyncedType(t *testing.T) {
	synced := []string{models.ItemTask, models.ItemDailyTask}
	for _, it := range synced {
		if !isSheetSyncedType(it) {
			t.Errorf("isSheetSyncedType(%q) = false, want true", it)
		}
	}
	notSynced := []string{models.ItemReminder, models.ItemRFS, models.ItemIncident, models.ItemRequest, models.ItemChange, ""}
	for _, it := range notSynced {
		if isSheetSyncedType(it) {
			t.Errorf("isSheetSyncedType(%q) = true, want false", it)
		}
	}
}
