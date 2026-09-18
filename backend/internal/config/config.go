// Package config memuat konfigurasi aplikasi dari environment variable INGATIN_*.
//
// Konvensi (diadopsi dari mcnvpn/internal/config):
//   - pembacaan memakai os.Getenv dengan helper getenv (nilai default bila kosong)
//   - tidak memakai godotenv; file .env dibaca oleh docker compose (env_file)
//   - validasi ketat untuk secret yang panjangnya harus tepat (credential key 32 byte)
package config

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// ErrInvalidCredentialKey dikembalikan bila INGATIN_CREDENTIAL_KEY bukan 32 byte.
var ErrInvalidCredentialKey = errors.New("INGATIN_CREDENTIAL_KEY harus tepat 32 byte")

// ErrMissingSecretKey dikembalikan bila INGATIN_SECRET_KEY kosong.
var ErrMissingSecretKey = errors.New("INGATIN_SECRET_KEY wajib diisi")

// Mode adalah mode operasi binary.
type Mode string

const (
	ModeAPI     Mode = "api"
	ModeWorker  Mode = "worker"
	ModeMigrate Mode = "migrate"
)

const (
	defaultAddr               = "127.0.0.1:8081"
	defaultAccessTokenMinutes = 4320 // 72 jam
	defaultRefreshTokenDays   = 30
	defaultTimezone           = "Asia/Jakarta"
	defaultDataDir            = "/app/data"
	defaultLogLevel           = "info"
	defaultWahaBaseURL        = "http://127.0.0.1:8082"
)

// Config menampung seluruh konfigurasi runtime.
type Config struct {
	// Umum
	AppName    string
	AppVersion string
	Mode       Mode
	Addr       string
	LogLevel   string
	Timezone   string
	DataDir    string

	// Keamanan
	SecretKey     string
	CredentialKey string

	// Database
	DBURL string

	// Sesi
	AccessTokenMinutes        int
	RefreshTokenDays          int
	SessionIdleTimeoutMinutes int

	// Bootstrap admin
	AdminUsername string
	AdminPassword string
	AdminEmail    string

	// HTTP
	FrontendOrigin string
	TrustedProxies []string

	// WAHA (kanal WhatsApp; kredensial provider lain diisi lewat panel)
	WahaBaseURL string
	WahaAPIKey  string
	// WAHAQRURL adalah URL dashboard QR WAHA yang ditampilkan di panel
	// (biasanya nginx :8010 dengan basic auth).
	WAHAQRURL string

	// Notifikasi
	OutboxBatchSize      int
	OutboxMaxAttempts    int
	OutboxSenderInterval int // detik
	FanoutInterval       int // detik

	// Retensi (hari)
	AuditRetentionDays   int
	OutboxRetentionDays  int
	BackupKeepDays       int
	RetentionIntervalHrs int
}

// Load membaca konfigurasi dari environment dan memvalidasi nilai kritis.
func Load() (*Config, error) {
	c := &Config{
		AppName:    getenv("INGATIN_APP_NAME", "Ingat.in"),
		AppVersion: getenv("INGATIN_APP_VERSION", "dev"),
		Mode:       Mode(getenv("INGATIN_MODE", string(ModeAPI))),
		Addr:       getenv("INGATIN_ADDR", defaultAddr),
		LogLevel:   getenv("INGATIN_LOG_LEVEL", defaultLogLevel),
		Timezone:   getenv("INGATIN_TIMEZONE", defaultTimezone),
		DataDir:    getenv("INGATIN_DATA_DIR", defaultDataDir),

		SecretKey:     getenv("INGATIN_SECRET_KEY", ""),
		CredentialKey: getenv("INGATIN_CREDENTIAL_KEY", ""),

		DBURL: getenv("INGATIN_DB_URL", ""),

		AdminUsername: getenv("INGATIN_ADMIN_USERNAME", "admin"),
		AdminPassword: getenv("INGATIN_ADMIN_PASSWORD", ""),
		AdminEmail:    getenv("INGATIN_ADMIN_EMAIL", ""),

		FrontendOrigin: getenv("INGATIN_FRONTEND_ORIGIN", "http://localhost:8091"),

		WahaBaseURL: getenv("INGATIN_WAHA_BASE_URL", defaultWahaBaseURL),
		WahaAPIKey:  getenv("INGATIN_WAHA_API_KEY", ""),
		WAHAQRURL:   getenv("INGATIN_WAHA_QR_URL", ""),
	}

	// Mode validasi
	switch c.Mode {
	case ModeAPI, ModeWorker, ModeMigrate:
	default:
		return nil, errors.New("INGATIN_MODE tidak valid (pilih: api, worker, migrate)")
	}

	// Numerik
	var err error
	if c.AccessTokenMinutes, err = getenvInt("INGATIN_ACCESS_TOKEN_MINUTES", defaultAccessTokenMinutes); err != nil {
		return nil, err
	}
	if c.RefreshTokenDays, err = getenvInt("INGATIN_REFRESH_TOKEN_DAYS", defaultRefreshTokenDays); err != nil {
		return nil, err
	}
	if c.SessionIdleTimeoutMinutes, err = getenvInt("INGATIN_SESSION_IDLE_TIMEOUT_MINUTES", 0); err != nil {
		return nil, err
	}
	if c.OutboxBatchSize, err = getenvInt("INGATIN_OUTBOX_BATCH_SIZE", 50); err != nil {
		return nil, err
	}
	if c.OutboxMaxAttempts, err = getenvInt("INGATIN_OUTBOX_MAX_ATTEMPTS", 5); err != nil {
		return nil, err
	}
	if c.OutboxSenderInterval, err = getenvInt("INGATIN_OUTBOX_SENDER_INTERVAL_SECONDS", 15); err != nil {
		return nil, err
	}
	if c.FanoutInterval, err = getenvInt("INGATIN_FANOUT_INTERVAL_SECONDS", 60); err != nil {
		return nil, err
	}
	if c.AuditRetentionDays, err = getenvInt("INGATIN_AUDIT_RETENTION_DAYS", 90); err != nil {
		return nil, err
	}
	if c.OutboxRetentionDays, err = getenvInt("INGATIN_OUTBOX_RETENTION_DAYS", 90); err != nil {
		return nil, err
	}
	if c.BackupKeepDays, err = getenvInt("INGATIN_BACKUP_KEEP_DAYS", 14); err != nil {
		return nil, err
	}
	if c.RetentionIntervalHrs, err = getenvInt("INGATIN_RETENTION_INTERVAL_HOURS", 24); err != nil {
		return nil, err
	}

	c.TrustedProxies = parseTrustedProxies(getenv("INGATIN_TRUSTED_PROXIES", ""))

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if strings.TrimSpace(c.SecretKey) == "" {
		return ErrMissingSecretKey
	}
	// Credential key dipakai AES-256-GCM -> wajib tepat 32 byte.
	// Mode migrate tidak memerlukannya (hanya menyentuh skema).
	if c.Mode != ModeMigrate && len(c.CredentialKey) != 32 {
		return ErrInvalidCredentialKey
	}
	if c.Mode != ModeMigrate && strings.TrimSpace(c.DBURL) == "" {
		return errors.New("INGATIN_DB_URL wajib diisi")
	}
	if c.AccessTokenMinutes <= 0 {
		return errors.New("INGATIN_ACCESS_TOKEN_MINUTES harus > 0")
	}
	if c.RefreshTokenDays <= 0 {
		return errors.New("INGATIN_REFRESH_TOKEN_DAYS harus > 0")
	}
	if c.OutboxBatchSize <= 0 || c.OutboxBatchSize > 1000 {
		return errors.New("INGATIN_OUTBOX_BATCH_SIZE harus antara 1..1000")
	}
	if c.OutboxMaxAttempts <= 0 {
		return errors.New("INGATIN_OUTBOX_MAX_ATTEMPTS harus > 0")
	}
	return nil
}

// Marshal mengembalikan representasi JSON konfigurasi untuk audit/log.
// Secret tidak pernah disertakan.
func (c *Config) Marshal() []byte {
	safe := map[string]any{
		"app_name":                   c.AppName,
		"app_version":                c.AppVersion,
		"mode":                       string(c.Mode),
		"addr":                       c.Addr,
		"log_level":                  c.LogLevel,
		"timezone":                   c.Timezone,
		"data_dir":                   c.DataDir,
		"access_token_minutes":       c.AccessTokenMinutes,
		"refresh_token_days":         c.RefreshTokenDays,
		"session_idle_timeout_min":   c.SessionIdleTimeoutMinutes,
		"frontend_origin":            c.FrontendOrigin,
		"trusted_proxies":            c.TrustedProxies,
		"waha_base_url":              c.WahaBaseURL,
		"waha_api_key_set":           c.WahaAPIKey != "",
		"secret_key_set":             c.SecretKey != "",
		"credential_key_set":         c.CredentialKey != "",
		"db_url_set":                 c.DBURL != "",
		"outbox_batch_size":          c.OutboxBatchSize,
		"outbox_max_attempts":        c.OutboxMaxAttempts,
		"outbox_sender_interval_sec": c.OutboxSenderInterval,
		"fanout_interval_sec":        c.FanoutInterval,
		"audit_retention_days":       c.AuditRetentionDays,
		"outbox_retention_days":      c.OutboxRetentionDays,
		"backup_keep_days":           c.BackupKeepDays,
		"retention_interval_hours":   c.RetentionIntervalHrs,
	}
	out, _ := json.Marshal(safe)
	return out
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(key + " harus berupa angka")
	}
	return v, nil
}

// parseTrustedProxies memisahkan daftar IP/CIDR pada koma, spasi, tab, atau baris baru.
func parseTrustedProxies(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if v := strings.TrimSpace(f); v != "" {
			out = append(out, v)
		}
	}
	return out
}
