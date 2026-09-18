package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Telegram mengirim pesan melalui Telegram Bot API.
//
// Konfigurasi:
//   - BaseURL: default https://api.telegram.org
//   - APIKey : token bot dari @BotFather
//   - Extra["parse_mode"]: "HTML" (default) atau "MarkdownV2" atau kosong
//   - Extra["disable_notification"]: "true" untuk kirim senyap
type Telegram struct {
	cfg    Config
	client *http.Client
}

// NewTelegram membuat provider Telegram.
func NewTelegram(cfg Config) *Telegram {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	return &Telegram{
		cfg:    cfg,
		client: &http.Client{Timeout: timeoutOr(cfg)},
	}
}

func (t *Telegram) Kind() string { return KindTelegramBot }

func (t *Telegram) Capabilities() Capabilities {
	return Capabilities{SupportsMedia: false, SupportsGroups: true, SupportsTest: true}
}

// TestConnection memverifikasi token dengan memanggil getMe.
func (t *Telegram) TestConnection(ctx context.Context) error {
	if strings.TrimSpace(t.cfg.APIKey) == "" {
		return fmt.Errorf("%w: telegram bot token kosong", ErrNotConfigured)
	}

	url := fmt.Sprintf("%s/bot%s/getMe", strings.TrimRight(t.cfg.BaseURL, "/"), t.cfg.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("hubungi Telegram: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("token bot ditolak oleh Telegram (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram mengembalikan HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("respons Telegram tidak dapat dibaca: %w", err)
	}
	if !parsed.OK {
		return fmt.Errorf("Telegram melaporkan gagal: %s", parsed.Description)
	}
	return nil
}

// Send mengirim pesan teks ke chat tujuan.
func (t *Telegram) Send(ctx context.Context, msg Message) (Result, error) {
	if strings.TrimSpace(t.cfg.APIKey) == "" {
		return Result{}, fmt.Errorf("%w: telegram bot token kosong", ErrNotConfigured)
	}
	dest := strings.TrimSpace(msg.Destination)
	if dest == "" {
		return Result{}, ErrNoDestination
	}

	// chat_id dapat berupa id numerik atau @username; kirim sebagai string
	// agar Telegram menerima keduanya.
	payload := map[string]any{
		"chat_id":    dest,
		"text":       composeText(msg),
		"parse_mode": telegramParseMode(t.cfg.Extra),
	}
	if StringExtra(t.cfg.Extra, "disable_notification") == "true" {
		payload["disable_notification"] = true
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(t.cfg.BaseURL, "/"), t.cfg.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		// Kegagalan jaringan bersifat sementara.
		return Result{Retryable: true}, fmt.Errorf("kirim Telegram: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	rawStr := truncateStr(string(body), 1000)

	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		ErrorCode   int    `json:"error_code"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &parsed)

	switch {
	case resp.StatusCode == http.StatusOK && parsed.OK:
		return Result{
			ProviderMessageID: strconv.FormatInt(parsed.Result.MessageID, 10),
			RawResponse:       rawStr,
		}, nil

	case resp.StatusCode == http.StatusTooManyRequests:
		// Rate limit: boleh dicoba ulang.
		return Result{Retryable: true, RawResponse: rawStr},
			fmt.Errorf("Telegram rate limit: %s", parsed.Description)

	case resp.StatusCode >= 500:
		// Kesalahan sisi Telegram: sementara.
		return Result{Retryable: true, RawResponse: rawStr},
			fmt.Errorf("Telegram error %d: %s", resp.StatusCode, parsed.Description)

	case resp.StatusCode == http.StatusBadRequest:
		// Kesalahan permanen (chat_id salah, bot diblokir): jangan diulang.
		return Result{RawResponse: rawStr},
			fmt.Errorf("Telegram menolak pesan (400): %s", parsed.Description)

	default:
		return Result{RawResponse: rawStr},
			fmt.Errorf("Telegram HTTP %d: %s", resp.StatusCode, parsed.Description)
	}
}

// telegramParseMode menentukan parse mode. Default HTML karena lebih toleran
// terhadap karakter khusus dibanding MarkdownV2.
func telegramParseMode(extra map[string]any) string {
	mode := strings.TrimSpace(StringExtra(extra, "parse_mode"))
	switch mode {
	case "", "html", "HTML":
		return "HTML"
	case "none", "plain":
		return ""
	case "MarkdownV2", "markdownv2":
		return "MarkdownV2"
	case "Markdown", "markdown":
		return "Markdown"
	default:
		return "HTML"
	}
}

// composeText menggabungkan subject dan body bila ada.
func composeText(msg Message) string {
	body := msg.Body
	if strings.TrimSpace(msg.Subject) != "" {
		return fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(msg.Subject), escapeHTMLPreservingNewlines(msg.Body))
	}
	return escapeHTMLPreservingNewlines(body)
}

// escapeHTML mengubah karakter khusus HTML agar parse_mode=HTML tidak gagal.
func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// escapeHTMLPreservingNewlines menjaga baris baru tetapi meng-escape tag.
func escapeHTMLPreservingNewlines(s string) string {
	return escapeHTML(s)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
