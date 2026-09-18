package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPWhatsApp adalah implementasi untuk penyedia WhatsApp berbasis HTTP API
// yang umum dipakai di Indonesia (Fonnte, Wablas, Starsender) serta endpoint
// kustom.
//
// Perbedaan tiap penyedia ditangani melalui field Kind dan Extra, sehingga
// menambah penyedia baru tidak memerlukan file baru selama bentuk
// permintaannya sama (POST dengan token + tujuan + pesan).
//
// Konfigurasi:
//   - BaseURL: endpoint kirim, mis. https://api.fonnte.com/send
//   - APIKey : token penyedia
//   - Extra["auth_header"]  : nama header token (default "Authorization")
//   - Extra["auth_prefix"]  : prefiks nilai header (mis. "Bearer ")
//   - Extra["dest_field"]   : nama field tujuan (default "target")
//   - Extra["text_field"]   : nama field pesan (default "message")
//   - Extra["form_encoded"] : "true" untuk application/x-www-form-urlencoded
//   - Extra["success_field"]: field pada respons JSON yang menandakan sukses
//   - Extra["id_field"]     : field pada respons JSON yang memuat id pesan
type HTTPWhatsApp struct {
	kind   string
	cfg    Config
	client *http.Client
}

// NewFonnte membuat provider Fonnte.
func NewFonnte(cfg Config) *HTTPWhatsApp {
	return newHTTPWhatsApp(KindFonnte, cfg, httpDefaults{
		authHeader:  "Authorization",
		destField:   "target",
		textField:   "message",
		successFiel: "status",
		idField:     "id",
	})
}

// NewWablas membuat provider Wablas.
func NewWablas(cfg Config) *HTTPWhatsApp {
	return newHTTPWhatsApp(KindWablas, cfg, httpDefaults{
		authHeader:  "Authorization",
		destField:   "phone",
		textField:   "message",
		successFiel: "status",
		idField:     "id",
	})
}

// NewStarsender membuat provider Starsender.
func NewStarsender(cfg Config) *HTTPWhatsApp {
	return newHTTPWhatsApp(KindStarsender, cfg, httpDefaults{
		authHeader:  "Authorization",
		destField:   "to",
		textField:   "message",
		successFiel: "success",
		idField:     "data",
	})
}

// NewCustomHTTP membuat provider HTTP kustom.
func NewCustomHTTP(cfg Config) *HTTPWhatsApp {
	return newHTTPWhatsApp(KindCustomHTTP, cfg, httpDefaults{
		authHeader:  "Authorization",
		destField:   "to",
		textField:   "message",
		successFiel: "",
		idField:     "",
	})
}

type httpDefaults struct {
	authHeader  string
	destField   string
	textField   string
	successFiel string
	idField     string
}

func newHTTPWhatsApp(kind string, cfg Config, def httpDefaults) *HTTPWhatsApp {
	if StringExtra(cfg.Extra, "auth_header") == "" {
		ensureExtra(&cfg, "auth_header", def.authHeader)
	}
	if StringExtra(cfg.Extra, "dest_field") == "" {
		ensureExtra(&cfg, "dest_field", def.destField)
	}
	if StringExtra(cfg.Extra, "text_field") == "" {
		ensureExtra(&cfg, "text_field", def.textField)
	}
	if _, ok := cfg.Extra["success_field"]; !ok && def.successFiel != "" {
		ensureExtra(&cfg, "success_field", def.successFiel)
	}
	if _, ok := cfg.Extra["id_field"]; !ok && def.idField != "" {
		ensureExtra(&cfg, "id_field", def.idField)
	}
	return &HTTPWhatsApp{
		kind:   kind,
		cfg:    cfg,
		client: &http.Client{Timeout: timeoutOr(cfg)},
	}
}

func ensureExtra(cfg *Config, key, val string) {
	if cfg.Extra == nil {
		cfg.Extra = map[string]any{}
	}
	cfg.Extra[key] = val
}

func (h *HTTPWhatsApp) Kind() string { return h.kind }

func (h *HTTPWhatsApp) Capabilities() Capabilities {
	return Capabilities{SupportsMedia: false, SupportsGroups: true, SupportsTest: true}
}

// TestConnection memverifikasi konfigurasi tanpa mengirim pesan.
//
// Karena penyedia HTTP umumnya tidak memiliki endpoint ping, pemeriksaan
// terbatas pada kelengkapan konfigurasi dan keterjangkauan host.
func (h *HTTPWhatsApp) TestConnection(ctx context.Context) error {
	if strings.TrimSpace(h.cfg.BaseURL) == "" {
		return fmt.Errorf("%w: base URL kosong", ErrNotConfigured)
	}
	u, err := url.Parse(h.cfg.BaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("base URL tidak valid: %q", h.cfg.BaseURL)
	}
	if strings.TrimSpace(h.cfg.APIKey) == "" {
		return fmt.Errorf("%w: API key kosong", ErrNotConfigured)
	}

	// Tidak semua penyedia menerima HEAD/GET pada endpoint kirim, sehingga
	// pemeriksaan dibatasi pada validitas konfigurasi. Verifikasi pengiriman
	// sebenarnya dilakukan lewat aksi "Test Kirim" pada panel.
	return nil
}

// Send mengirim pesan melalui penyedia HTTP.
func (h *HTTPWhatsApp) Send(ctx context.Context, msg Message) (Result, error) {
	if strings.TrimSpace(h.cfg.BaseURL) == "" {
		return Result{}, fmt.Errorf("%w: base URL kosong", ErrNotConfigured)
	}
	dest := normalizePhone(msg.Destination)
	if dest == "" {
		return Result{}, ErrNoDestination
	}

	destField := defaultStr(StringExtra(h.cfg.Extra, "dest_field"), "target")
	textField := defaultStr(StringExtra(h.cfg.Extra, "text_field"), "message")

	text := msg.Body
	if strings.TrimSpace(msg.Subject) != "" {
		text = "*" + msg.Subject + "*\n\n" + msg.Body
	}

	formEncoded := StringExtra(h.cfg.Extra, "form_encoded") == "true"

	var (
		req  *http.Request
		err  error
		body []byte
	)

	if formEncoded {
		form := url.Values{}
		form.Set(destField, dest)
		form.Set(textField, text)
		body = []byte(form.Encode())
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.BaseURL, bytes.NewReader(body))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		payload := map[string]any{destField: dest, textField: text}
		body, err = json.Marshal(payload)
		if err != nil {
			return Result{}, err
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.BaseURL, bytes.NewReader(body))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/json")
	}

	// Header autentikasi.
	authHeader := defaultStr(StringExtra(h.cfg.Extra, "auth_header"), "Authorization")
	authPrefix := StringExtra(h.cfg.Extra, "auth_prefix")
	req.Header.Set(authHeader, authPrefix+h.cfg.APIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return Result{Retryable: true}, fmt.Errorf("kirim ke %s: %w", h.kind, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	rawStr := truncateStr(string(raw), 1000)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Sebagian penyedia mengembalikan HTTP 200 dengan status gagal di body.
		if err := h.checkBodySuccess(raw); err != nil {
			return Result{RawResponse: rawStr}, err
		}
		return Result{
			ProviderMessageID: h.extractID(raw),
			RawResponse:       rawStr,
		}, nil

	case resp.StatusCode == http.StatusTooManyRequests:
		return Result{Retryable: true, RawResponse: rawStr}, fmt.Errorf("%s rate limit (429)", h.kind)

	case resp.StatusCode >= 500:
		return Result{Retryable: true, RawResponse: rawStr}, fmt.Errorf("%s error server (%d)", h.kind, resp.StatusCode)

	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{RawResponse: rawStr}, fmt.Errorf("%s menolak token (%d)", h.kind, resp.StatusCode)

	default:
		return Result{RawResponse: rawStr},
			fmt.Errorf("%s menolak pesan (%d): %s", h.kind, resp.StatusCode, truncateStr(string(raw), 300))
	}
}

// checkBodySuccess memeriksa field penanda sukses bila dikonfigurasi.
func (h *HTTPWhatsApp) checkBodySuccess(raw []byte) error {
	field := StringExtra(h.cfg.Extra, "success_field")
	if field == "" {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// Body bukan JSON: biarkan dianggap sukses berdasarkan HTTP status.
		return nil
	}
	val, ok := parsed[field]
	if !ok {
		return nil
	}

	switch v := val.(type) {
	case bool:
		if !v {
			return fmt.Errorf("%s melaporkan kegagalan: %s", h.kind, truncateStr(string(raw), 300))
		}
	case string:
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv == "false" || lv == "0" || lv == "fail" || lv == "failed" || lv == "error" {
			return fmt.Errorf("%s melaporkan kegagalan: %s", h.kind, truncateStr(string(raw), 300))
		}
	case float64:
		if v == 0 {
			return fmt.Errorf("%s melaporkan kegagalan: %s", h.kind, truncateStr(string(raw), 300))
		}
	}
	return nil
}

// extractID mengambil id pesan dari respons bila field-nya dikonfigurasi.
func (h *HTTPWhatsApp) extractID(raw []byte) string {
	field := StringExtra(h.cfg.Extra, "id_field")
	if field == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ""
	}
	if v, ok := parsed[field]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
