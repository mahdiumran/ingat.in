package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSplitTelegramMessageShort: pesan pendek tetap satu potongan.
func TestSplitTelegramMessageShort(t *testing.T) {
	parts := splitTelegramMessage("halo dunia", telegramMaxMessageLen)
	if len(parts) != 1 || parts[0] != "halo dunia" {
		t.Errorf("short message terpecah: %v", parts)
	}
}

// TestSplitTelegramMessageEmpty: teks kosong tidak menghasilkan potongan.
func TestSplitTelegramMessageEmpty(t *testing.T) {
	if parts := splitTelegramMessage("   ", telegramMaxMessageLen); parts != nil {
		t.Errorf("teks kosong harus nil, got %v", parts)
	}
}

// TestSplitTelegramMessageLong: pesan panjang terpecah <= max dan utuh.
func TestSplitTelegramMessageLong(t *testing.T) {
	text := strings.Repeat("baris pesan reminder panjang\n", 400) // ~11.600 char
	parts := splitTelegramMessage(text, telegramMaxMessageLen)
	if len(parts) < 2 {
		t.Fatalf("pesan panjang harus terpecah >=2, got %d", len(parts))
	}
	rejoined := strings.Join(parts, "")
	if rejoined != text {
		t.Errorf("konten tidak utuh setelah di-join (len %d vs %d)", len(rejoined), len(text))
	}
	for i, p := range parts {
		if len([]rune(p)) > telegramMaxMessageLen {
			t.Errorf("potongan %d melebihi batas: %d", i, len([]rune(p)))
		}
	}
}

// TestSplitTelegramMessageEntity: entity HTML tidak terpotong di tengah.
func TestSplitTelegramMessageEntity(t *testing.T) {
	// Susun sehingga '&' jatuh tepat di dekat batas potong.
	prefix := strings.Repeat("a", telegramMaxMessageLen-2)
	text := prefix + "&amp; tail"
	parts := splitTelegramMessage(text, telegramMaxMessageLen)
	for i, p := range parts {
		// Setiap '&' harus diikuti '&'…';' lengkap dalam potongan yang sama.
		_ = i
		idx := strings.Index(p, "&")
		for idx != -1 {
			if !strings.Contains(p[idx:], ";") {
				t.Errorf("entity terpotong pada potongan %d: %q", i, p[maxInt(0, idx-10):])
			}
			next := strings.Index(p[idx+1:], "&")
			if next == -1 {
				break
			}
			idx = idx + 1 + next
		}
	}
	if got := strings.Join(parts, ""); got != text {
		t.Errorf("konten berubah: %q", got)
	}
}

// TestBalanceBoldTags: tag <b> terpotong ditutup & dibuka kembali.
func TestBalanceBoldTags(t *testing.T) {
	parts := []string{"awal <b>tebal", "lanjutan tebal</b> akhir"}
	balanceBoldTags(parts)
	if strings.Count(parts[0], "<b>") != strings.Count(parts[0], "</b>") {
		t.Errorf("potongan 0 tidak seimbang: %q", parts[0])
	}
	if strings.Count(parts[1], "<b>") != strings.Count(parts[1], "</b>") {
		t.Errorf("potongan 1 tidak seimbang: %q", parts[1])
	}
}

// TestTelegramSendSplitsLongMessage: mock hitung jumlah request sendMessage.
func TestTelegramSendSplitsLongMessage(t *testing.T) {
	var calls int32
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if s, ok := payload["text"].(string); ok {
			texts = append(texts, s)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "token-uji"})
	long := strings.Repeat("detail pelanggan sangat panjang. ", 150) // ~5.100 char
	res, err := p.Send(context.Background(), Message{
		Destination: "-100123",
		Subject:     "LAPORAN",
		Body:        long,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.ProviderMessageID != "42" {
		t.Errorf("message id = %q, ingin 42", res.ProviderMessageID)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("pesan panjang harus >1 request sendMessage, got %d", calls)
	}
	// Potongan pertama memuat subject tebal.
	if !strings.Contains(texts[0], "<b>LAPORAN</b>") {
		t.Errorf("subject hilang di potongan pertama: %q", texts[0][:80])
	}
	// Potongan lanjutan ditandai.
	if len(texts) > 1 && !strings.Contains(texts[1], "(lanjutan 2/") {
		t.Errorf("penanda lanjutan hilang: %q", texts[1][:40])
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
