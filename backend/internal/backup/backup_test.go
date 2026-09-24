package backup

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseDBURL(t *testing.T) {
	conn, err := parseDBURL("postgres://ingatin:secret@127.0.0.1:5432/ingatin?sslmode=disable")
	if err != nil {
		t.Fatalf("parseDBURL: %v", err)
	}
	if conn.User != "ingatin" || conn.Password != "secret" || conn.Database != "ingatin" {
		t.Fatalf("hasil tidak sesuai: %+v", conn)
	}
	if conn.Host != "127.0.0.1" || conn.Port != "5432" {
		t.Fatalf("host/port tidak sesuai: %+v", conn)
	}
	if conn.SSLMode != "disable" {
		t.Fatalf("sslmode tidak sesuai: %s", conn.SSLMode)
	}
}

func TestParseDBURLDefaults(t *testing.T) {
	conn, err := parseDBURL("postgres://u@db/ingatin")
	if err != nil {
		t.Fatalf("parseDBURL: %v", err)
	}
	if conn.Host != "db" || conn.Port != "5432" {
		t.Fatalf("default host/port salah: %+v", conn)
	}
	if _, err := parseDBURL("mysql://x/y"); err == nil {
		t.Fatal("skema non-postgres seharusnya gagal")
	}
	if _, err := parseDBURL(""); err == nil {
		t.Fatal("DBURL kosong seharusnya gagal")
	}
}

func TestFileName(t *testing.T) {
	at := time.Date(2026, 9, 24, 2, 30, 5, 0, time.UTC)
	if got := fileName("auto", at); got != "ingatin-auto-20260924-023005.dump" {
		t.Fatalf("fileName = %q", got)
	}
	if got := fileName("", at); got != "ingatin-20260924-023005.dump" {
		t.Fatalf("fileName tanpa label = %q", got)
	}
	// Label tidak aman dibersihkan.
	if got := fileName("a b/c..d", at); strings.ContainsAny(got, " /") || strings.Contains(got, "..") {
		t.Fatalf("label tidak dibersihkan: %q", got)
	}
}

func TestPathRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../etc/passwd", "a/b.dump", "..", "", "x.sql", "/abs.dump"} {
		if _, err := Path("/data/backups", bad); err == nil {
			t.Fatalf("Path(%q) seharusnya gagal", bad)
		}
	}
	got, err := Path("/data/backups", "ingatin-auto-1.dump")
	if err != nil {
		t.Fatalf("Path valid gagal: %v", err)
	}
	if got != "/data/backups/ingatin-auto-1.dump" {
		t.Fatalf("Path = %q", got)
	}
}

func TestSanitizeLabel(t *testing.T) {
	if got := sanitizeLabel("auto-2026_01"); got != "auto-2026_01" {
		t.Fatalf("sanitizeLabel = %q", got)
	}
	if got := sanitizeLabel("we!rd @label"); got != "werdlabel" {
		t.Fatalf("sanitizeLabel = %q", got)
	}
}

func TestPruneKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	// keepDays <= 0 → tidak memangkas.
	n, err := Prune(dir, 0)
	if err != nil || n != 0 {
		t.Fatalf("Prune(0) = %d, %v", n, err)
	}
}

func TestSaveUploadRejectsNonDump(t *testing.T) {
	dir := t.TempDir()
	_, err := SaveUpload(dir, "upload", strings.NewReader("this is not a dump"), time.Now())
	if err != ErrNotDump {
		t.Fatalf("SaveUpload seharusnya menolak non-dump, dapat %v", err)
	}
}

func TestSaveUploadAcceptsDump(t *testing.T) {
	dir := t.TempDir()
	body := "PGDMP" + strings.Repeat("x", 100)
	info, err := SaveUpload(dir, "upload", strings.NewReader(body), time.Date(2026, 9, 24, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if !strings.HasPrefix(info.Name, "ingatin-upload-") {
		t.Fatalf("nama berkas tidak sesuai: %s", info.Name)
	}
	if info.SizeBytes != int64(len(body)) {
		t.Fatalf("ukuran tidak sesuai: %d != %d", info.SizeBytes, len(body))
	}
	full, err := Path(dir, info.Name)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("baca berkas: %v", err)
	}
	if string(got) != body {
		t.Fatalf("isi berkas tidak sama")
	}
}
