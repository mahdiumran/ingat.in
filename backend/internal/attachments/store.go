// Package attachments menyimpan & melayani lampiran pendukung work item (F21).
//
// Berkas disimpan di <DataDir>/attachments/<work_item_id>/<uuid>__<nama aman>.
// Nama asli TIDAK dipakai sebagai path langsung; nama tersimpan sudah
// disanitasi dan diberi prefiks UUID agar tidak ada path traversal / tabrakan.
package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// ErrTooLarge / ErrTypeNotAllowed / ErrEmptyFile adalah kesalahan validasi.
var (
	ErrTooLarge       = errors.New("ukuran berkas melebihi batas")
	ErrTypeNotAllowed = errors.New("jenis berkas tidak diizinkan")
	ErrEmptyFile      = errors.New("berkas kosong")
	ErrNotFound       = errors.New("lampiran tidak ditemukan")
)

// Store menyimpan lampiran pada disk + metadata pada database.
type Store struct {
	cfg   *config.Config
	repo  *repository.Store
	maxMB int
}

// New membuat Store lampiran.
func New(cfg *config.Config, repo *repository.Store) *Store {
	return &Store{cfg: cfg, repo: repo, maxMB: cfg.AttachmentsMaxMB}
}

// MaxBytes mengembalikan batas ukuran unggahan dalam byte.
func (s *Store) MaxBytes() int64 { return int64(s.maxMB) * 1024 * 1024 }

// TypesAllowed melaporkan apakah MIME diizinkan (wildcard "image/*" didukung).
func (s *Store) TypesAllowed(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	for _, t := range s.cfg.AttachmentsAllowedTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == mime {
			return true
		}
		if strings.HasSuffix(t, "/*") && strings.HasPrefix(mime, strings.TrimSuffix(t, "*")) {
			return true
		}
	}
	return false
}

// SaveResult memuat metadata hasil penyimpanan + apakah baris benar-benar baru.
type SaveResult struct {
	Attachment models.Attachment
	Inserted   bool
}

// Save menyimpan berkas untuk sebuah work item.
//
// mime wajib lolos TypesAllowed. Ukuran dibatasi MaxBytes (pemanggil juga
// membungkus body dengan http.MaxBytesReader sebagai lapis pertama).
func (s *Store) Save(ctx context.Context, workItemID uuid.UUID, filename, mime, uploadedBy string, r io.Reader) (*SaveResult, error) {
	filename = sanitizeName(filename)
	if filename == "" {
		filename = "lampiran"
	}
	if !s.TypesAllowed(mime) {
		return nil, fmt.Errorf("%w: %s", ErrTypeNotAllowed, mime)
	}

	dir := filepath.Join(s.cfg.DataDir, "attachments", workItemID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("buat direktori lampiran: %w", err)
	}

	// Batasi ukuran saat penulisan (lapis kedua selain MaxBytesReader).
	limited := io.LimitReader(r, s.MaxBytes()+1)
	storedName := randomToken() + "__" + filename
	fullPath := filepath.Join(dir, storedName)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return nil, fmt.Errorf("buat berkas lampiran: %w", err)
	}
	written, copyErr := io.Copy(f, limited)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("tulis berkas lampiran: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(fullPath)
		return nil, closeErr
	}
	if written == 0 {
		_ = os.Remove(fullPath)
		return nil, ErrEmptyFile
	}
	if written > s.MaxBytes() {
		_ = os.Remove(fullPath)
		return nil, ErrTooLarge
	}

	att, inserted, err := s.repo.CreateAttachment(ctx, repository.CreateAttachmentParams{
		WorkItemID: workItemID,
		Filename:   filename,
		StoredPath: fullPath,
		SizeBytes:  written,
		Mime:       strings.ToLower(strings.TrimSpace(mime)),
		UploadedBy: uploadedBy,
	})
	if err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	return &SaveResult{Attachment: *att, Inserted: inserted}, nil
}

// Open membuka berkas lampiran untuk diunduh.
func (s *Store) Open(ctx context.Context, id uuid.UUID) (*models.Attachment, *os.File, error) {
	att, err := s.repo.GetAttachment(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	f, err := os.Open(att.StoredPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return att, f, nil
}

// Remove menghapus lampiran (berkas + baris).
func (s *Store) Remove(ctx context.Context, id uuid.UUID) error {
	att, err := s.repo.GetAttachment(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.repo.DeleteAttachment(ctx, id); err != nil {
		return err
	}
	_ = os.Remove(att.StoredPath)
	return nil
}

// List mengembalikan daftar lampiran sebuah work item.
func (s *Store) List(ctx context.Context, workItemID uuid.UUID) ([]models.Attachment, error) {
	return s.repo.ListAttachments(ctx, workItemID)
}

// sanitizeName membuang komponen path dan karakter berbahaya dari nama berkas.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\x00", "")
	// Batasi panjang agar aman untuk filesystem.
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

// randomToken membuat prefiks acak untuk nama berkas tersimpan.
func randomToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return uuid.NewString()[:8]
	}
	return hex.EncodeToString(b)
}
