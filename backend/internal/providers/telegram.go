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
	"time"
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

	// F41: pesan > batas Telegram (4096 char) dipecah menjadi beberapa
	// sendMessage berurutan — sebelumnya Telegram menolak (400) dan notifikasi
	// hilang total. Subject tebal hanya pada potongan pertama; potongan
	// lanjutan diberi penanda "(lanjutan k/n)".
	parts := splitTelegramMessage(composeText(msg), telegramMaxMessageLen)
	if len(parts) == 0 {
		parts = []string{""}
	}

	var firstID string
	var lastRaw string
	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(t.cfg.BaseURL, "/"), t.cfg.APIKey)
	parseMode := telegramParseMode(t.cfg.Extra)
	quiet := StringExtra(t.cfg.Extra, "disable_notification") == "true"

	for k, part := range parts {
		text := part
		if len(parts) > 1 && k > 0 {
			text = fmt.Sprintf("(lanjutan %d/%d)\n\n%s", k+1, len(parts), part)
		}
		payload := map[string]any{
			"chat_id":    dest,
			"text":       text,
			"parse_mode": parseMode,
		}
		if quiet {
			payload["disable_notification"] = true
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return Result{}, err
		}
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		lastRaw = truncateStr(string(body), 1000)
		var parsed struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
			ErrorCode   int    `json:"error_code"`
			Result      struct {
				MessageID int64 `json:"message_id"`
			} `json:"result"`
		}
		_ = json.Unmarshal(body, &parsed)
		ok350 := resp.StatusCode == http.StatusOK && parsed.OK
		if k == 0 {
			firstID = strconv.FormatInt(parsed.Result.MessageID, 10)
		}
		if !ok350 {
			switch {
			case resp.StatusCode == http.StatusTooManyRequests:
				return Result{Retryable: true, RawResponse: lastRaw},
					fmt.Errorf("Telegram rate limit: %s", parsed.Description)
			case resp.StatusCode >= 500:
				return Result{Retryable: true, RawResponse: lastRaw},
					fmt.Errorf("Telegram error %d: %s", resp.StatusCode, parsed.Description)
			case resp.StatusCode == http.StatusBadRequest:
				return Result{RawResponse: lastRaw},
					fmt.Errorf("Telegram menolak pesan (400): %s", parsed.Description)
			default:
				return Result{RawResponse: lastRaw},
					fmt.Errorf("Telegram HTTP %d: %s", resp.StatusCode, parsed.Description)
			}
		}
		// Jeda kecil antar-potongan menghormati batas laju per-chat.
		if k < len(parts)-1 {
			select {
			case <-ctx.Done():
				return Result{ProviderMessageID: firstID, RawResponse: lastRaw}, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}

	return Result{ProviderMessageID: firstID, RawResponse: lastRaw}, nil
}

// telegramMaxMessageLen adalah batas aman di bawah 4096 karakter Telegram.
const telegramMaxMessageLen = 4000

// splitTelegramMessage memecah teks menjadi potongan <= max (rune), memotong
// pada batas baris terbaru bila memungkinkan, tanpa memutus entity HTML
// (&amp; &lt; &gt;) dan tanpa membuang tag <b> yang terbuka.
func splitTelegramMessage(text string, max int) []string {
	if max <= 0 {
		max = telegramMaxMessageLen
	}
	runes := []rune(text)
	if len(runes) <= max {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}

	var parts []string
	start := 0
	for start < len(runes) {
		end := start + max
		if end >= len(runes) {
			end = len(runes)
		} else {
			// Cari pemisah baris/kata terakhir sebelum max.
			seg := string(runes[start:end])
			cut := strings.LastIndex(seg, "\n")
			if cut < max*3/5 { // terlalu jauh di belakang? pakai spasi
				cut = strings.LastIndex(seg, " ")
			}
			if cut > 0 {
				end = start + cut + 1
			}
		}
		chunk := string(runes[start:end])
		// Jangan memutus entity HTML di batas potong.
		if amp := strings.LastIndex(chunk, "&"); amp != -1 && !strings.Contains(chunk[amp:], ";") {
			chunk = chunk[:amp]
			end = start + len([]rune(chunk))
		}
		parts = append(parts, chunk)
		start = end
	}
	// Seimbangkan tag <b>…</b> yang terpotong: tutup tag yang masih terbuka
	// di akhir potongan dan buka kembali di potongan berikutnya.
	balanceBoldTags(parts)
	return parts
}

// balanceBoldTags menutup <b> yang menggantung di tiap potongan dan membuka
// kembali di potongan berikutnya agar parse_mode=HTML tetap valid.
//
// Akumulasi selisih open-minus-close dihitung sekali dari teks asli, lalu
// tiap potongan ditutup sejumlah selisih kumulatifnya dan potongan berikutnya
// diberi <b> pembuka sebanyak itu. Tag yang disisipkan TIDAK dihitung ulang
// sebagai open potongan penerima (itulah sebabnya pendekatan kumulatif, bukan
// per-potongan, dipakai — mencegah </b> ganda).
func balanceBoldTags(parts []string) {
	n := len(parts)
	cum := make([]int, n)
	run := 0
	for k := range parts {
		run += strings.Count(parts[k], "<b>") - strings.Count(parts[k], "</b>")
		cum[k] = run
	}
	for k := 0; k < n; k++ {
		unclosed := cum[k]
		if k == n-1 {
			// Potongan terakhir menutup seluruh sisa tag.
			unclosed = run
		} else if unclosed < 0 {
			unclosed = 0
		}
		if unclosed > 0 {
			parts[k] += strings.Repeat("</b>", unclosed)
			if k+1 < n {
				parts[k+1] = strings.Repeat("<b>", unclosed) + parts[k+1]
			}
		}
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
