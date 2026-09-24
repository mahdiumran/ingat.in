package attachments

import (
	"testing"

	"ingatin/backend/internal/config"
)

func newTestStore() *Store {
	cfg := &config.Config{
		DataDir:                 t0,
		AttachmentsMaxMB:        50,
		AttachmentsAllowedTypes: []string{"image/png", "image/*", "application/pdf", "text/plain"},
	}
	return &Store{cfg: cfg, maxMB: 50}
}

const t0 = "/tmp/ingatin-test-attachments"

func TestTypesAllowed(t *testing.T) {
	s := newTestStore()
	allowed := []string{"image/png", "image/jpeg", "application/pdf", "text/plain"}
	for _, m := range allowed {
		if !s.TypesAllowed(m) {
			t.Errorf("%s harus diizinkan", m)
		}
	}
	denied := []string{"application/octet-stream", "application/x-msdownload", ""}
	for _, m := range denied {
		if s.TypesAllowed(m) {
			t.Errorf("%s harus ditolak", m)
		}
	}
}

func TestMaxBytes(t *testing.T) {
	s := newTestStore()
	if s.MaxBytes() != 50*1024*1024 {
		t.Errorf("MaxBytes = %d", s.MaxBytes())
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":   "passwd",
		"/abs/path/file.png": "file.png",
		"normal.pdf":         "normal.pdf",
		`win\path\a.txt`:     "a.txt",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
