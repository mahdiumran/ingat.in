// Package sheets menyinkronkan Todo Task ke Google Spreadsheet (F18).
//
// Prinsip desain (mengikuti pola notify/outbox):
//   - Konfigurasi (Spreadsheet ID, nama sheet, service account) dikelola admin
//     dari panel web — TIDAK ada nilai yang di-hardcode di kode.
//   - Penulisan dilakukan lewat antrean database yang idempoten, sehingga aman
//     terhadap restart worker maupun gangguan jaringan.
package sheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// ErrNotConfigured dikembalikan bila sinkronisasi belum siap dijalankan.
var ErrNotConfigured = errors.New("sinkronisasi spreadsheet belum dikonfigurasi")

// ServiceAccount adalah bagian yang dipakai dari JSON service account Google.
type ServiceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
	TokenURI     string `json:"token_uri"`

	// raw menyimpan JSON asli (yang dikembalikan ke Sheets SDK).
	raw string
}

// RawJSON mengembalikan JSON service account asli untuk Google SDK.
func (sa *ServiceAccount) RawJSON() string { return sa.raw }

// ParseServiceAccount memvalidasi dan mengurai JSON service account.
//
// Hanya field yang diperlukan yang divalidasi agar variasi format dari Google
// tetap dapat diterima.
func ParseServiceAccount(raw string) (*ServiceAccount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: service account kosong", ErrNotConfigured)
	}
	var sa ServiceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return nil, fmt.Errorf("service account JSON tidak valid: %w", err)
	}
	if strings.TrimSpace(sa.ClientEmail) == "" {
		return nil, errors.New("service account JSON tidak memuat client_email")
	}
	if strings.TrimSpace(sa.PrivateKey) == "" {
		return nil, errors.New("service account JSON tidak memuat private_key")
	}
	if strings.TrimSpace(sa.TokenURI) == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	sa.raw = raw
	return &sa, nil
}

// forbiddenSheetChars adalah karakter yang dilarang Google Sheets pada nama tab.
const forbiddenSheetChars = "[]*?/\\:"

// ValidateSheetName memvalidasi nama sheet/tab.
//
// Aturan: tidak kosong, maksimal 100 karakter, tanpa karakter terlarang
// ([ ] * ? / \ :). Mengembalikan pesan yang dapat langsung ditampilkan ke user.
func ValidateSheetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("nama sheet wajib diisi")
	}
	if len([]rune(name)) > 100 {
		return errors.New("nama sheet maksimal 100 karakter")
	}
	for _, r := range name {
		if strings.ContainsRune(forbiddenSheetChars, r) {
			return fmt.Errorf("nama sheet tidak boleh memuat karakter %q", string(r))
		}
	}
	return nil
}

// LoadConfig mengambil konfigurasi dan mendekripsinya.
//
// Mengembalikan (config, serviceAccount). ServiceAccount nil bila kredensial
// belum diisi, sehingga pemanggil dapat memutuskan apakah itu kegagalan.
func LoadConfig(ctx context.Context, store *repository.Store, credentialKey string) (*models.SheetSyncConfig, *ServiceAccount, error) {
	cfg, err := store.GetSheetSyncConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	row := cfg

	if cfg.ServiceAccountEnc == "" {
		return row, nil, nil
	}
	plain, err := crypto.Decrypt(credentialKey, cfg.ServiceAccountEnc)
	if err != nil {
		return row, nil, fmt.Errorf("dekripsi service account: %w", err)
	}
	sa, err := ParseServiceAccount(plain)
	if err != nil {
		return row, nil, err
	}
	return row, sa, nil
}

// EncryptServiceAccount memvalidasi lalu mengenkripsi JSON service account.
func EncryptServiceAccount(credentialKey, raw string) (string, *ServiceAccount, error) {
	sa, err := ParseServiceAccount(raw)
	if err != nil {
		return "", nil, err
	}
	enc, err := crypto.Encrypt(credentialKey, raw)
	if err != nil {
		return "", nil, err
	}
	return enc, sa, nil
}
