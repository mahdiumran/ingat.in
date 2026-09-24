package sheets

import (
	"strings"
	"testing"
)

func TestValidateSheetName(t *testing.T) {
	valid := []string{"Todo", "Kerjaan NOC", "Log Harian 2026", "Sheet1"}
	for _, name := range valid {
		if err := ValidateSheetName(name); err != nil {
			t.Errorf("ValidateSheetName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "   ", "a[b", "a]b", "a*b", "a?b", "a/b", "a\\b", "a:b", strings.Repeat("x", 101)}
	for _, name := range invalid {
		if err := ValidateSheetName(name); err == nil {
			t.Errorf("ValidateSheetName(%q) = nil, want error", name)
		}
	}
}

func TestParseServiceAccount(t *testing.T) {
	good := `{"type":"service_account","client_email":"svc@proj.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"}`
	sa, err := ParseServiceAccount(good)
	if err != nil {
		t.Fatalf("ParseServiceAccount gagal: %v", err)
	}
	if sa.ClientEmail != "svc@proj.iam.gserviceaccount.com" {
		t.Errorf("client_email = %q", sa.ClientEmail)
	}
	if sa.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Errorf("token_uri default tidak diisi: %q", sa.TokenURI)
	}
	if sa.RawJSON() != good {
		t.Error("RawJSON harus mengembalikan JSON asli")
	}

	bad := []string{
		"",
		"not json",
		`{"type":"service_account"}`,
		`{"client_email":"x@y.z"}`,
	}
	for _, raw := range bad {
		if _, err := ParseServiceAccount(raw); err == nil {
			t.Errorf("ParseServiceAccount(%q) = nil, want error", raw)
		}
	}
}

func TestRowFromPayload(t *testing.T) {
	payload := map[string]any{
		"item_type":        "task",
		"ref_no":           "TSK-2026-0001",
		"title":            "Cek BGP flap",
		"description":      "Deskripsi",
		"priority":         "high",
		"status":           "on_progress",
		"owner":            "budi",
		"created_by":       "admin",
		"updated_by":       "operatorA",
		"device_ref":       "MX204-CGK1",
		"tags":             "bgp, cgk1",
		"due_at_wib":       "18 Sep 2026 17:00",
		"created_at_wib":   "18 Sep 2026 08:00",
		"completed_at_wib": "18 Sep 2026 16:30",
		"completed_by":     "operatorA",
		"completion_note":  "Sudah dicek, normal",
	}

	row := RowFromPayload(payload)
	if len(row) != len(HeaderRow) {
		t.Fatalf("jumlah kolom = %d, want %d", len(row), len(HeaderRow))
	}
	if row[0] != "TSK-2026-0001" {
		t.Errorf("kolom Ref = %v", row[0])
	}
	if row[1] != "Todo" {
		t.Errorf("kolom Tipe = %v, want Todo", row[1])
	}
	// Status internal harus jadi label yang enak dibaca.
	if row[5] != "On Progress" {
		t.Errorf("kolom Status = %v, want On Progress", row[5])
	}
	if row[8] != "operatorA" {
		t.Errorf("kolom Diperbarui Oleh = %v, want operatorA", row[8])
	}
	if row[11] != "18 Sep 2026 17:00" {
		t.Errorf("kolom Due = %v", row[11])
	}
	// F27: waktu selesai + aktor penyelesai.
	if row[14] != "18 Sep 2026 16:30" {
		t.Errorf("kolom Selesai = %v", row[14])
	}
	if row[15] != "operatorA" {
		t.Errorf("kolom Diselesaikan Oleh = %v", row[15])
	}
	if row[16] != "Sudah dicek, normal" {
		t.Errorf("kolom Keterangan = %v", row[16])
	}
	// Kolom Diperbarui (indeks 13) diisi waktu sekarang, tidak boleh kosong.
	if str(row[13]) == "" {
		t.Error("kolom Diperbarui harus terisi")
	}
}

func TestRowFromPayloadDailyTask(t *testing.T) {
	row := RowFromPayload(map[string]any{
		"item_type": "daily_task",
		"ref_no":    "DTK-2026-0001",
		"title":     "Cek tiket pelanggan",
		"status":    "pending",
	})
	if row[1] != "Daily Task" {
		t.Errorf("kolom Tipe = %v, want Daily Task", row[1])
	}
	if row[5] != "Belum Selesai" {
		t.Errorf("kolom Status = %v, want Belum Selesai", row[5])
	}
}

func TestRowFromPayloadMissingKeys(t *testing.T) {
	row := RowFromPayload(map[string]any{})
	if len(row) != len(HeaderRow) {
		t.Fatalf("jumlah kolom = %d, want %d", len(row), len(HeaderRow))
	}
	// Indeks 13 (Diperbarui) selalu terisi waktu sekarang; kolom lain harus kosong.
	for i, v := range row {
		if i == 13 {
			continue
		}
		if s := str(v); s != "" {
			t.Errorf("kolom %d harus kosong, got %q", i, s)
		}
	}
}

func TestStatusLabel(t *testing.T) {
	cases := map[string]string{
		"accepted":    "Accepted",
		"on_progress": "On Progress",
		"expired":     "Expired",
		"canceled":    "Canceled",
		"closed":      "Closed",
		"":            "",
		"lain":        "lain",
	}
	for in, want := range cases {
		if got := statusLabel(in); got != want {
			t.Errorf("statusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeaderRowMatchesLastColumn(t *testing.T) {
	// A=1 .. Q=17; HeaderRow harus 17 kolom dan LastColumn = Q.
	if len(HeaderRow) != 17 {
		t.Fatalf("HeaderRow = %d kolom, want 17", len(HeaderRow))
	}
	if LastColumn != "Q" {
		t.Errorf("LastColumn = %q, want Q", LastColumn)
	}
}

func TestItemTypeLabel(t *testing.T) {
	cases := map[string]string{
		"task":       "Todo",
		"daily_task": "Daily Task",
		"":           "",
		"lain":       "lain",
	}
	for in, want := range cases {
		if got := itemTypeLabel(in); got != want {
			t.Errorf("itemTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRowFromPayloadDeletedMarker(t *testing.T) {
	row := RowFromPayload(map[string]any{
		"item_type": "daily_task",
		"ref_no":    "DTK-2026-0009",
		"title":     "Task dihapus",
		"status":    "done",
		"deleted":   true,
	})
	if row[5] != "Dihapus" {
		t.Errorf("status item terhapus = %v, want Dihapus", row[5])
	}
}

func TestRowFromPayloadUpdatedByAndNote(t *testing.T) {
	// Kolom baru: I (Diperbarui Oleh, indeks 8) dan Q (Keterangan, indeks 16).
	row := RowFromPayload(map[string]any{
		"item_type":       "daily_task",
		"ref_no":          "DTK-2026-0001",
		"updated_by":      "operatorB",
		"completion_note": "Sudah dicek, clear",
	})
	if row[8] != "operatorB" {
		t.Errorf("kolom Diperbarui Oleh = %v, want operatorB", row[8])
	}
	if row[16] != "Sudah dicek, clear" {
		t.Errorf("kolom Keterangan = %v", row[16])
	}
}

func TestHeaderRowHasNewColumns(t *testing.T) {
	if HeaderRow[8] != "Diperbarui Oleh" {
		t.Errorf("kolom I = %v, want Diperbarui Oleh", HeaderRow[8])
	}
	// F27: Selesai (WIB) di O, Diselesaikan Oleh di P, Keterangan di Q.
	if HeaderRow[14] != "Selesai (WIB)" {
		t.Errorf("kolom O = %v, want Selesai (WIB)", HeaderRow[14])
	}
	if HeaderRow[15] != "Diselesaikan Oleh" {
		t.Errorf("kolom P = %v, want Diselesaikan Oleh", HeaderRow[15])
	}
	if HeaderRow[16] != "Keterangan" {
		t.Errorf("kolom Q = %v, want Keterangan", HeaderRow[16])
	}
}
