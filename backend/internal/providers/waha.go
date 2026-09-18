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

// WAHA mengirim pesan WhatsApp melalui WAHA (WhatsApp HTTP API) yang
// dijalankan sendiri di server ini.
//
// Konfigurasi:
//   - BaseURL: mis. http://127.0.0.1:8082
//   - APIKey : nilai WAHA_API_KEY (dikirim melalui header X-Api-Key)
//   - Extra["session"]: nama sesi WAHA, default "default"
//
// Catatan: engine WAHA (WEBJS/GOWS) ditentukan saat menjalankan container,
// bukan di sini.
type WAHA struct {
	cfg    Config
	client *http.Client
}

// NewWAHA membuat provider WAHA.
func NewWAHA(cfg Config) *WAHA {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "http://127.0.0.1:8082"
	}
	return &WAHA{
		cfg:    cfg,
		client: &http.Client{Timeout: timeoutOr(cfg)},
	}
}

func (w *WAHA) Kind() string { return KindWAHA }

func (w *WAHA) Capabilities() Capabilities {
	return Capabilities{SupportsMedia: false, SupportsGroups: true, SupportsTest: true}
}

// sessionName mengembalikan nama sesi WAHA yang dipakai.
func (w *WAHA) sessionName() string {
	if s := StringExtra(w.cfg.Extra, "session"); s != "" {
		return s
	}
	return "default"
}

// do menjalankan permintaan HTTP dengan header autentikasi WAHA.
func (w *WAHA) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}

	base := strings.TrimRight(w.cfg.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if w.cfg.APIKey != "" {
		req.Header.Set("X-Api-Key", w.cfg.APIKey)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
	return resp.StatusCode, raw, nil
}

// TestConnection memeriksa bahwa WAHA dapat dijangkau dan sesi berstatus bekerja.
//
// Status sesi WAHA: STOPPED | STARTING | SCAN_QR_CODE | WORKING | FAILED
func (w *WAHA) TestConnection(ctx context.Context) error {
	code, raw, err := w.do(ctx, http.MethodGet, "/api/sessions", nil)
	if err != nil {
		return fmt.Errorf("hubungi WAHA: %w", err)
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return fmt.Errorf("WAHA menolak API key (HTTP %d)", code)
	}
	if code != http.StatusOK {
		return fmt.Errorf("WAHA mengembalikan HTTP %d: %s", code, truncateStr(string(raw), 200))
	}

	var sessions []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return fmt.Errorf("respons WAHA tidak dapat dibaca: %w", err)
	}

	want := w.sessionName()
	for _, s := range sessions {
		if s.Name == want {
			switch s.Status {
			case "WORKING":
				return nil
			case "SCAN_QR_CODE":
				return fmt.Errorf("sesi %q menunggu scan QR — buka dashboard WAHA dan tautkan perangkat", want)
			case "STARTING":
				return fmt.Errorf("sesi %q sedang memulai; coba lagi beberapa saat", want)
			case "FAILED":
				return fmt.Errorf("sesi %q gagal; restart sesi dari dashboard WAHA", want)
			default:
				return fmt.Errorf("sesi %q berstatus %s (belum siap mengirim)", want, s.Status)
			}
		}
	}
	return fmt.Errorf("sesi %q tidak ditemukan di WAHA — buka dashboard QR dan mulai sesi", want)
}

// SessionsStatus mengembalikan status seluruh sesi WAHA.
// Dipakai panel Providers untuk menampilkan indikator status.
func (w *WAHA) SessionsStatus(ctx context.Context) ([]map[string]any, error) {
	code, raw, err := w.do(ctx, http.MethodGet, "/api/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("hubungi WAHA: %w", err)
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("WAHA HTTP %d: %s", code, truncateStr(string(raw), 200))
	}

	var sessions []map[string]any
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, fmt.Errorf("respons WAHA tidak dapat dibaca: %w", err)
	}
	return sessions, nil
}

// StartSession memulai sesi. Bila sesi belum ada, sesi dibuat terlebih dahulu.
//
// Kompatibilitas: pada WAHA versi yang lebih baru, memulai sesi yang belum
// pernah dibuat mengembalikan HTTP 404 "Session not found". Karena itu langkah
// ini idempoten: coba start; bila belum ada, buat (dengan start=true); bila
// sudah berjalan, anggap sukses.
func (w *WAHA) StartSession(ctx context.Context) error {
	name := w.sessionName()

	// 1. Coba mulai sesi yang sudah ada.
	path := "/api/sessions/" + url.PathEscape(name) + "/start"
	code, raw, err := w.do(ctx, http.MethodPost, path, map[string]any{})
	if err != nil {
		return fmt.Errorf("mulai sesi WAHA: %w", err)
	}
	if code >= 200 && code < 300 {
		return nil
	}
	// 422 = sudah berjalan; anggap sukses.
	if code == 422 {
		return nil
	}
	// Selain 404, laporkan apa adanya.
	if code != http.StatusNotFound {
		return fmt.Errorf("WAHA HTTP %d: %s", code, truncateStr(string(raw), 200))
	}

	// 2. Sesi belum ada: buat sekaligus mulai.
	code, raw, err = w.do(ctx, http.MethodPost, "/api/sessions", map[string]any{
		"name":  name,
		"start": true,
	})
	if err != nil {
		return fmt.Errorf("buat sesi WAHA: %w", err)
	}
	if code >= 400 {
		return fmt.Errorf("WAHA HTTP %d: %s", code, truncateStr(string(raw), 200))
	}
	return nil
}

// StopSession menghentikan sesi tanpa menghapusnya.
func (w *WAHA) StopSession(ctx context.Context) error {
	path := "/api/sessions/" + url.PathEscape(w.sessionName()) + "/stop"
	code, raw, err := w.do(ctx, http.MethodPost, path, map[string]any{})
	if err != nil {
		return fmt.Errorf("hentikan sesi WAHA: %w", err)
	}
	if code >= 400 && code != 422 {
		return fmt.Errorf("WAHA HTTP %d: %s", code, truncateStr(string(raw), 200))
	}
	return nil
}

// LogoutSession memutus tautan perangkat (perlu scan QR ulang).
func (w *WAHA) LogoutSession(ctx context.Context) error {
	path := "/api/sessions/" + url.PathEscape(w.sessionName()) + "/logout"
	code, raw, err := w.do(ctx, http.MethodPost, path, map[string]any{})
	if err != nil {
		return fmt.Errorf("logout sesi WAHA: %w", err)
	}
	if code >= 400 && code != 422 {
		return fmt.Errorf("WAHA HTTP %d: %s", code, truncateStr(string(raw), 200))
	}
	return nil
}

// Send mengirim pesan teks.
func (w *WAHA) Send(ctx context.Context, msg Message) (Result, error) {
	dest := normalizePhone(msg.Destination)
	if dest == "" {
		return Result{}, ErrNoDestination
	}

	// WAHA menerima chatId format "<nomor>@c.us" untuk kontak perorangan
	// dan "<groupid>@g.us" untuk grup.
	chatID := dest
	if !strings.Contains(chatID, "@") {
		chatID = chatID + "@c.us"
	}

	payload := map[string]any{
		"session": w.sessionName(),
		"chatId":  chatID,
		"text":    composeWhatsAppText(msg),
	}

	code, raw, err := w.do(ctx, http.MethodPost, "/api/sendText", payload)
	if err != nil {
		return Result{Retryable: true}, fmt.Errorf("kirim ke WAHA: %w", err)
	}

	rawStr := truncateStr(string(raw), 1000)

	switch {
	case code >= 200 && code < 300:
		var parsed struct {
			ID any `json:"id"`
		}
		_ = json.Unmarshal(raw, &parsed)
		id := ""
		if parsed.ID != nil {
			id = fmt.Sprintf("%v", parsed.ID)
		}
		return Result{ProviderMessageID: id, RawResponse: rawStr}, nil

	case code == http.StatusTooManyRequests:
		return Result{Retryable: true, RawResponse: rawStr},
			fmt.Errorf("WAHA rate limit (429)")

	case code >= 500:
		return Result{Retryable: true, RawResponse: rawStr},
			fmt.Errorf("WAHA error server (%d)", code)

	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return Result{RawResponse: rawStr},
			fmt.Errorf("WAHA menolak API key (%d)", code)

	default:
		// 400/422 biasanya berarti nomor tidak valid atau sesi belum siap.
		// Sesi belum siap layak dicoba ulang.
		retryable := code == 422 && strings.Contains(strings.ToLower(string(raw)), "session")
		return Result{Retryable: retryable, RawResponse: rawStr},
			fmt.Errorf("WAHA menolak pesan (%d): %s", code, truncateStr(string(raw), 300))
	}
}

// composeWhatsAppText menyusun teks dengan penekanan minimal.
//
// WhatsApp memakai *tebal* dan _miring_ (bukan HTML seperti Telegram), jadi
// subject dirender sebagai baris tebal.
func composeWhatsAppText(msg Message) string {
	if strings.TrimSpace(msg.Subject) != "" {
		return "*" + msg.Subject + "*\n\n" + msg.Body
	}
	return msg.Body
}

// normalizePhone membersihkan nomor telepon ke bentuk digit saja.
//
// Contoh: "+62 812-3456-7890" -> "6281234567890".
// Grup (chat id berakhiran @g.us) dikembalikan apa adanya.
func normalizePhone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "@") {
		return s
	}

	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			// tanda plus di awal diabaikan (kita simpan digit saja)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// pemisah diabaikan
		default:
			// karakter lain diabaikan
		}
	}
	digits := b.String()

	// Normalisasi awalan Indonesia: 08xx -> 628xx.
	if strings.HasPrefix(digits, "0") {
		digits = "62" + strings.TrimPrefix(digits, "0")
	}
	return digits
}
