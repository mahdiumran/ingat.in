package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

/* ---------------------------------------------------------------------------
   F34 — Cadangan (backup) & pemulihan (restore) database dari panel admin.
   --------------------------------------------------------------------------- */

// Info adalah metadata satu berkas cadangan.
type Info struct {
	Name       string    `json:"name"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	Uploaded   bool      `json:"uploaded"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
	UploadErr  string    `json:"upload_error,omitempty"`
}

// ErrNoBackup dikembalikan bila tidak ada berkas cadangan.
var ErrNoBackup = errors.New("berkas cadangan tidak ditemukan")

// DumpOptions mengendalikan pembuatan cadangan.
type DumpOptions struct {
	DBURL string
	Dir   string
	// Label opsional ditambahkan pada nama berkas (mis. "auto" atau "manual").
	Label string
}

// pgConn menampung parameter koneksi hasil parsing DBURL.
type pgConn struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
}

// parseDBURL mengurai URL PostgreSQL gaya pgx (postgres://user:pass@host:port/db?...).
func parseDBURL(raw string) (*pgConn, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("DBURL kosong")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("urai DBURL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("skema DBURL tidak dikenal: %s", u.Scheme)
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		return nil, errors.New("nama database tidak ada di DBURL")
	}
	conn := &pgConn{
		Host:     u.Hostname(),
		Port:     u.Port(),
		Database: db,
		SSLMode:  "disable",
	}
	if conn.Host == "" {
		conn.Host = "127.0.0.1"
	}
	if conn.Port == "" {
		conn.Port = "5432"
	}
	if u.User != nil {
		conn.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			conn.Password = p
		}
	}
	if s := u.Query().Get("sslmode"); s != "" {
		conn.SSLMode = s
	}
	return conn, nil
}

// fileName menyusun nama berkas cadangan ber-timestamp (UTC).
func fileName(label string, at time.Time) string {
	label = strings.TrimSpace(sanitizeLabel(label))
	ts := at.UTC().Format("20060102-150405")
	if label == "" {
		return fmt.Sprintf("ingatin-%s.dump", ts)
	}
	return fmt.Sprintf("ingatin-%s-%s.dump", label, ts)
}

// sanitizeLabel memastikan label hanya memuat karakter aman untuk nama berkas.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Create menjalankan pg_dump dan menyimpan berkas ke direktori opts.Dir.
// Mengembalikan metadata berkas yang dibuat.
func Create(ctx context.Context, opts DumpOptions) (*Info, error) {
	conn, err := parseDBURL(opts.DBURL)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("buat direktori cadangan: %w", err)
	}

	now := time.Now().UTC()
	name := fileName(opts.Label, now)
	full := filepath.Join(opts.Dir, name)

	args := []string{
		"-Fc", // format custom (pg_restore)
		"-Z", "6", // kompresi
		"-h", conn.Host,
		"-p", conn.Port,
		"-U", conn.User,
		"-d", conn.Database,
		"-f", full,
	}

	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+conn.Password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_ = os.Remove(full)
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("pg_dump gagal: %s", msg)
	}

	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if st.Size() == 0 {
		_ = os.Remove(full)
		return nil, errors.New("pg_dump menghasilkan berkas kosong")
	}

	return &Info{
		Name:      name,
		SizeBytes: st.Size(),
		CreatedAt: now,
	}, nil
}

// List mengembalikan daftar berkas cadangan di dir, terbaru lebih dulu.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Info{}, nil
		}
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{
			Name:      e.Name(),
			SizeBytes: fi.Size(),
			CreatedAt: fi.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path mengembalikan jalur aman sebuah berkas cadangan (mencegah path traversal).
func Path(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return "", errors.New("nama berkas cadangan tidak valid")
	}
	if !strings.HasSuffix(name, ".dump") {
		return "", errors.New("berkas cadangan harus berekstensi .dump")
	}
	return filepath.Join(dir, name), nil
}

// Delete menghapus berkas cadangan.
func Delete(dir, name string) error {
	p, err := Path(dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoBackup
		}
		return err
	}
	return nil
}

// Prune menghapus berkas cadangan yang lebih tua dari keepDays.
// keepDays <= 0 berarti tidak ada pemangkasan. Mengembalikan jumlah dihapus.
func Prune(dir string, keepDays int) (int, error) {
	if keepDays <= 0 {
		return 0, nil
	}
	list, err := List(dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepDays) * 24 * time.Hour)
	removed := 0
	for _, it := range list {
		if it.CreatedAt.Before(cutoff) {
			p, perr := Path(dir, it.Name)
			if perr != nil {
				continue
			}
			if err := os.Remove(p); err != nil {
				log.Printf("backup: gagal hapus %s: %v", it.Name, err)
				continue
			}
			removed++
		}
	}
	return removed, nil
}

// Restore memulihkan database dari berkas cadangan memakai pg_restore.
//
// Menggunakan --clean --if-exists agar objek lama diganti. Proses ini bersifat
// destruktif: seluruh data saat ini pada tabel yang ada di cadangan akan
// tertimpa. Pemanggil wajib meminta konfirmasi eksplisit sebelumnya.
func Restore(ctx context.Context, dbURL, dumpPath string) error {
	conn, err := parseDBURL(dbURL)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dumpPath); err != nil {
		return fmt.Errorf("berkas cadangan tidak dapat dibaca: %w", err)
	}

	args := []string{
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"-h", conn.Host,
		"-p", conn.Port,
		"-U", conn.User,
		"-d", conn.Database,
		dumpPath,
	}
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+conn.Password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		// pg_restore sering mengembalikan status non-nol untuk peringatan
		// (mis. "already exists") padahal data berhasil dipulihkan. Laporkan
		// isi stderr agar operator dapat menilai.
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("pg_restore: %s", msg)
	}
	return nil
}

// DumpPath mengembalikan jalur lengkap berkas cadangan yang aman.
func DumpPath(dir, name string) (string, error) {
	return Path(dir, name)
}

// ErrNotDump dikembalikan bila berkas yang diunggah bukan dump PostgreSQL.
var ErrNotDump = errors.New("berkas bukan dump PostgreSQL (format custom pg_dump)")

// SaveUpload menyimpan berkas cadangan yang diunggah dari klien ke direktori
// cadangan. Nama hasil selalu dibentuk ulang (ber-timestamp) agar aman, dengan
// label opsional. Berkas divalidasi memiliki magic "PGDMP" (format custom
// pg_dump); berkas SQL teks polos juga diterima bila berekstensi .sql.
//
// Mengembalikan metadata berkas yang tersimpan.
func SaveUpload(dir, label string, r io.Reader, at time.Time) (*Info, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("buat direktori cadangan: %w", err)
	}

	// Baca 5 byte pertama untuk memeriksa magic tanpa menelan seluruh berkas.
	head := make([]byte, 5)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("baca berkas unggahan: %w", err)
	}
	head = head[:n]
	if len(head) < 5 || string(head) != "PGDMP" {
		return nil, ErrNotDump
	}

	if label == "" {
		label = "upload"
	}
	name := fileName(label, at)
	full := filepath.Join(dir, name)

	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("buat berkas cadangan: %w", err)
	}
	if _, err := f.Write(head); err != nil {
		_ = f.Close()
		_ = os.Remove(full)
		return nil, err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(full)
		return nil, fmt.Errorf("tulis berkas unggahan: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(full)
		return nil, err
	}

	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	return &Info{
		Name:      name,
		SizeBytes: st.Size(),
		CreatedAt: at.UTC(),
	}, nil
}
