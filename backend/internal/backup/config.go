package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   Konfigurasi cadangan (disimpan pada tabel settings, key "backup.config").
   --------------------------------------------------------------------------- */

// SettingKey adalah kunci baris konfigurasi cadangan pada tabel settings.
const SettingKey = "backup.config"

// FTPOptions adalah konfigurasi FTP yang terlihat klien (password disamarkan).
type FTPOptions struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	Dir         string `json:"dir"`
	Passive     bool   `json:"passive"`
}

// Config adalah konfigurasi lengkap cadangan.
type Config struct {
	Enabled  bool       `json:"enabled"`
	Schedule string     `json:"schedule"`
	KeepDays int        `json:"keep_days"`
	FTP      FTPOptions `json:"ftp"`

	LastRunAt  *string `json:"last_run_at,omitempty"`
	LastStatus string  `json:"last_status"`
	LastError  string  `json:"last_error"`
	LastFile   string  `json:"last_file"`
}

// storedFTP adalah representasi mentah FTP di database (dengan ciphertext).
type storedFTP struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc"`
	Dir         string `json:"dir"`
	Passive     bool   `json:"passive"`
}

type storedConfig struct {
	Enabled    bool      `json:"enabled"`
	Schedule   string    `json:"schedule"`
	KeepDays   int       `json:"keep_days"`
	FTP        storedFTP `json:"ftp"`
	LastRunAt  *string   `json:"last_run_at"`
	LastStatus string    `json:"last_status"`
	LastError  string    `json:"last_error"`
	LastFile   string    `json:"last_file"`
}

// LoadConfig membaca konfigurasi cadangan dari settings. Bila belum ada,
// mengembalikan default (nonaktif) tanpa error.
func LoadConfig(ctx context.Context, store *repository.Store) (*Config, error) {
	raw, err := store.GetSetting(ctx, SettingKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return defaultConfig(), nil
		}
		return nil, err
	}
	// GetSetting mengembalikan map; marshal ulang lalu urai ke struct bertipe.
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var sc storedConfig
	if err := json.Unmarshal(buf, &sc); err != nil {
		return nil, fmt.Errorf("konfigurasi cadangan tidak valid: %w", err)
	}
	cfg := &Config{
		Enabled:    sc.Enabled,
		Schedule:   sc.Schedule,
		KeepDays:   sc.KeepDays,
		LastRunAt:  sc.LastRunAt,
		LastStatus: sc.LastStatus,
		LastError:  sc.LastError,
		LastFile:   sc.LastFile,
		FTP: FTPOptions{
			Enabled:     sc.FTP.Enabled,
			Host:        sc.FTP.Host,
			Port:        sc.FTP.Port,
			Username:    sc.FTP.Username,
			PasswordSet: sc.FTP.PasswordEnc != "",
			Dir:         sc.FTP.Dir,
			Passive:     sc.FTP.Passive,
		},
	}
	if cfg.Schedule == "" {
		cfg.Schedule = "0 2 * * *"
	}
	if cfg.KeepDays <= 0 {
		cfg.KeepDays = 14
	}
	if cfg.FTP.Port <= 0 {
		cfg.FTP.Port = 21
	}
	return cfg, nil
}

// SaveConfig menyimpan konfigurasi. passwordPlain hanya diubah bila != nil;
// nilai kosong berarti mempertahankan password lama.
func SaveConfig(ctx context.Context, store *repository.Store, cfg *Config, passwordPlain *string, credentialKey, actor string) error {
	sc := storedConfig{
		Enabled:    cfg.Enabled,
		Schedule:   strings.TrimSpace(cfg.Schedule),
		KeepDays:   cfg.KeepDays,
		LastRunAt:  cfg.LastRunAt,
		LastStatus: cfg.LastStatus,
		LastError:  cfg.LastError,
		LastFile:   cfg.LastFile,
		FTP: storedFTP{
			Enabled:  cfg.FTP.Enabled,
			Host:     strings.TrimSpace(cfg.FTP.Host),
			Port:     cfg.FTP.Port,
			Username: strings.TrimSpace(cfg.FTP.Username),
			Dir:      strings.TrimSpace(cfg.FTP.Dir),
			Passive:  cfg.FTP.Passive,
		},
	}
	if sc.Schedule == "" {
		sc.Schedule = "0 2 * * *"
	}
	if sc.KeepDays <= 0 {
		sc.KeepDays = 14
	}
	if sc.FTP.Port <= 0 {
		sc.FTP.Port = 21
	}

	// Pertahankan ciphertext lama bila password tidak diganti.
	prev, err := loadStored(ctx, store)
	if err != nil {
		return err
	}
	if prev != nil {
		sc.FTP.PasswordEnc = prev.FTP.PasswordEnc
	}
	if passwordPlain != nil {
		if strings.TrimSpace(*passwordPlain) == "" {
			sc.FTP.PasswordEnc = ""
		} else {
			enc, err := crypto.Encrypt(credentialKey, *passwordPlain)
			if err != nil {
				return fmt.Errorf("enkripsi password FTP: %w", err)
			}
			sc.FTP.PasswordEnc = enc
		}
	}

	value, err := structToMap(sc)
	if err != nil {
		return err
	}
	return store.SetSetting(ctx, SettingKey, value, actor)
}

// TouchRun memperbarui kolom status terakhir pada konfigurasi.
func TouchRun(ctx context.Context, store *repository.Store, status, lastErr, fileName string, at string, actor string) error {
	prev, err := loadStored(ctx, store)
	if err != nil {
		return err
	}
	if prev == nil {
		prev = &storedConfig{Schedule: "0 2 * * *", KeepDays: 14, FTP: storedFTP{Port: 21, Passive: true}}
	}
	rs := at
	prev.LastRunAt = &rs
	prev.LastStatus = status
	prev.LastError = lastErr
	if fileName != "" {
		prev.LastFile = fileName
	}
	value, err := structToMap(*prev)
	if err != nil {
		return err
	}
	return store.SetSetting(ctx, SettingKey, value, actor)
}

// FTPConfig membangun konfigurasi FTP siap-pakai (dekripsi password).
func (c *Config) FTPConfig(store *repository.Store, credentialKey string) (FTPConfig, error) {
	prev, err := loadStoredWithStore(context.Background(), store)
	if err != nil {
		return FTPConfig{}, err
	}
	pass := ""
	if prev != nil && prev.FTP.PasswordEnc != "" {
		if p, derr := crypto.Decrypt(credentialKey, prev.FTP.PasswordEnc); derr == nil {
			pass = p
		} else {
			return FTPConfig{}, fmt.Errorf("dekripsi password FTP: %w", derr)
		}
	}
	return FTPConfig{
		Host:     c.FTP.Host,
		Port:     c.FTP.Port,
		Username: c.FTP.Username,
		Password: pass,
		Dir:      c.FTP.Dir,
		Passive:  c.FTP.Passive,
	}, nil
}

func loadStored(ctx context.Context, store *repository.Store) (*storedConfig, error) {
	return loadStoredWithStore(ctx, store)
}

func loadStoredWithStore(ctx context.Context, store *repository.Store) (*storedConfig, error) {
	raw, err := store.GetSetting(ctx, SettingKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var sc storedConfig
	if err := json.Unmarshal(buf, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

func defaultConfig() *Config {
	return &Config{
		Enabled:  false,
		Schedule: "0 2 * * *",
		KeepDays: 14,
		FTP: FTPOptions{
			Port:    21,
			Passive: true,
		},
	}
}

// structToMap mengubah struct menjadi map[string]any untuk SetSetting.
func structToMap(v any) (map[string]any, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseKeepDays mengubah nilai apa pun dari JSON menjadi int yang aman.
func ParseKeepDays(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if p, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return p
		}
	}
	return 0
}
