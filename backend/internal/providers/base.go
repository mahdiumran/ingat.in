// Package providers berisi abstraksi pengiriman notifikasi.
//
// Setiap kanal mengimplementasikan interface Provider. Implementasi konkret
// berada di file terpisah (telegram.go, waha.go, fonnte.go, dst.) dan
// didaftarkan di registry.go sehingga provider default per kanal dapat
// ditentukan saat runtime dari panel tanpa membangun ulang aplikasi.
package providers

import (
	"context"
	"errors"
	"time"
)

// Kind adalah jenis implementasi provider.
const (
	KindWAHA           = "waha"
	KindFonnte         = "fonnte"
	KindWablas         = "wablas"
	KindStarsender     = "starsender"
	KindCustomHTTP     = "custom_http"
	KindTelegramBot    = "telegram_bot"
	KindSMTP           = "smtp"
	KindSlackWebhook   = "slack_webhook"
	KindDiscordWebhook = "discord_webhook"
	KindGeneric        = "generic"
)

// Error umum.
var (
	ErrNotConfigured = errors.New("provider belum dikonfigurasi")
	ErrNoDestination = errors.New("tujuan pengiriman kosong")
	ErrUnsupported   = errors.New("operasi tidak didukung provider ini")
)

// Message adalah satu pesan yang akan dikirim.
type Message struct {
	// Destination adalah alamat tujuan dalam bentuk asli kanal:
	// chat_id Telegram, nomor WhatsApp (format internasional), alamat email, dsb.
	Destination string

	Subject string
	Body    string

	// Severity memengaruhi cara render (mis. prefix emoji pada WhatsApp).
	Severity string

	// Metadata untuk audit/penelusuran (mis. ref_no work item).
	Metadata map[string]string
}

// Result adalah hasil pengiriman.
type Result struct {
	// ProviderMessageID adalah id pesan dari penyedia (bila ada), berguna
	// untuk penelusuran dan idempotensi.
	ProviderMessageID string

	// RawRespons berisi potongan respons mentah untuk diagnosis.
	RawResponse string

	// Retryable menandakan kegagalan bersifat sementara (mis. timeout,
	// rate limit) sehingga outbox boleh mencoba ulang.
	Retryable bool
}

// Capabilities menjelaskan kemampuan provider sehingga UI dapat menyesuaikan.
type Capabilities struct {
	SupportsMedia  bool
	SupportsGroups bool
	SupportsTest   bool
}

// Provider adalah kontrak untuk seluruh kanal notifikasi.
type Provider interface {
	// Kind mengembalikan jenis implementasi.
	Kind() string

	// Capabilities mengembalikan kemampuan provider.
	Capabilities() Capabilities

	// TestConnection memverifikasi konfigurasi/koneksi tanpa mengirim pesan.
	TestConnection(ctx context.Context) error

	// Send mengirim satu pesan.
	//
	// Implementasi harus:
	//   - menghormati ctx (timeout sudah ditetapkan pemanggil)
	//   - tidak melakukan retry internal (retry ditangani outbox)
	//   - menandai Result.Retryable=true untuk kegagalan sementara
	Send(ctx context.Context, msg Message) (Result, error)
}

// Config adalah konfigurasi provider yang dibaca dari database.
type Config struct {
	Kind    string
	Label   string
	BaseURL string
	APIKey  string
	Extra   map[string]any
	Timeout time.Duration
}

// DefaultTimeout adalah timeout default untuk pengiriman.
const DefaultTimeout = 15 * time.Second

// timeoutOr mengembalikan cfg.Timeout, atau DefaultTimeout bila nol.
func timeoutOr(cfg Config) time.Duration {
	if cfg.Timeout <= 0 {
		return DefaultTimeout
	}
	return cfg.Timeout
}

// StringExtra mengambil nilai string dari map Extra dengan type-assert aman.
func StringExtra(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	if v, ok := extra[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
