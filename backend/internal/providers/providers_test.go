package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/* ---------------------------------------------------------------------------
   Telegram
   --------------------------------------------------------------------------- */

func TestTelegramTestConnectionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Errorf("path tidak sesuai: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, ingin GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":123,"username":"ingatin_bot"}}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "token-uji"})
	if err := p.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestTelegramTestConnectionRejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "token-salah"})
	if err := p.TestConnection(context.Background()); err == nil {
		t.Fatal("token salah seharusnya gagal")
	}
}

func TestTelegramTestConnectionWithoutToken(t *testing.T) {
	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: "http://localhost"})
	if err := p.TestConnection(context.Background()); err == nil {
		t.Fatal("token kosong seharusnya gagal")
	}
}

func TestTelegramSendSuccess(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":98765}}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "token-uji"})
	res, err := p.Send(context.Background(), Message{
		Destination: "-1001234567890",
		Subject:     "RFS HARI INI",
		Body:        "PT Contoh — 100 Mbps",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.ProviderMessageID != "98765" {
		t.Errorf("message id = %q, ingin 98765", res.ProviderMessageID)
	}
	if got["chat_id"] != "-1001234567890" {
		t.Errorf("chat_id = %v", got["chat_id"])
	}
	text, _ := got["text"].(string)
	if !strings.Contains(text, "RFS HARI INI") || !strings.Contains(text, "PT Contoh") {
		t.Errorf("teks pesan tidak lengkap: %q", text)
	}
	if got["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v, ingin HTML", got["parse_mode"])
	}
}

func TestTelegramEscapesHTML(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "t"})
	_, err := p.Send(context.Background(), Message{
		Destination: "123",
		Body:        "<script>alert(1)</script> & data",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := got["text"].(string)
	if strings.Contains(text, "<script>") {
		t.Errorf("HTML tidak di-escape: %q", text)
	}
	if !strings.Contains(text, "&lt;script&gt;") || !strings.Contains(text, "&amp;") {
		t.Errorf("escape tidak benar: %q", text)
	}
}

func TestTelegramRateLimitIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Too Many Requests","error_code":429}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "t"})
	res, err := p.Send(context.Background(), Message{Destination: "123", Body: "x"})
	if err == nil {
		t.Fatal("rate limit seharusnya error")
	}
	if !res.Retryable {
		t.Error("rate limit harus ditandai retryable")
	}
}

func TestTelegramServerErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Internal"}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "t"})
	res, err := p.Send(context.Background(), Message{Destination: "123", Body: "x"})
	if err == nil {
		t.Fatal("500 seharusnya error")
	}
	if !res.Retryable {
		t.Error("500 harus retryable")
	}
}

func TestTelegramBadRequestIsNotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: srv.URL, APIKey: "t"})
	res, err := p.Send(context.Background(), Message{Destination: "salah", Body: "x"})
	if err == nil {
		t.Fatal("400 seharusnya error")
	}
	if res.Retryable {
		t.Error("400 (chat not found) tidak boleh retryable")
	}
}

func TestTelegramEmptyDestination(t *testing.T) {
	p := NewTelegram(Config{Kind: KindTelegramBot, BaseURL: "http://localhost", APIKey: "t"})
	if _, err := p.Send(context.Background(), Message{Body: "x"}); err == nil {
		t.Fatal("tujuan kosong seharusnya error")
	}
}

/* ---------------------------------------------------------------------------
   WAHA
   --------------------------------------------------------------------------- */

func TestWAHATestConnectionWorking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "rahasia" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"default","status":"WORKING"}]`))
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "rahasia"})
	if err := p.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestWAHATestConnectionNeedsQR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"default","status":"SCAN_QR_CODE"}]`))
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "k"})
	err := p.TestConnection(context.Background())
	if err == nil {
		t.Fatal("status SCAN_QR_CODE seharusnya melaporkan perlu scan")
	}
	if !strings.Contains(err.Error(), "QR") {
		t.Errorf("pesan error seharusnya menyebut QR: %v", err)
	}
}

func TestWAHATestConnectionRejectsBadKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "salah"})
	if err := p.TestConnection(context.Background()); err == nil {
		t.Fatal("API key salah seharusnya gagal")
	}
}

func TestWAHASendFormatsChatID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sendText" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":{"_serialized":"true_62812@c.us_ABC"}}`))
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "k"})
	res, err := p.Send(context.Background(), Message{
		Destination: "+62 812-3456-7890",
		Subject:     "Reminder H-1",
		Body:        "Trial akan habis",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["chatId"] != "6281234567890@c.us" {
		t.Errorf("chatId = %v, ingin 6281234567890@c.us", got["chatId"])
	}
	if got["session"] != "default" {
		t.Errorf("session = %v, ingin default", got["session"])
	}
	text, _ := got["text"].(string)
	if !strings.HasPrefix(text, "*Reminder H-1*") {
		t.Errorf("subject tidak dirender tebal: %q", text)
	}
	if res.ProviderMessageID == "" {
		t.Error("provider message id kosong")
	}
}

func TestWAHASendKeepsGroupChatID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "k"})
	_, err := p.Send(context.Background(), Message{Destination: "120363000000000000@g.us", Body: "halo grup"})
	if err != nil {
		t.Fatal(err)
	}
	if got["chatId"] != "120363000000000000@g.us" {
		t.Errorf("chatId grup berubah: %v", got["chatId"])
	}
}

func TestWAHAServerErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: srv.URL, APIKey: "k"})
	res, err := p.Send(context.Background(), Message{Destination: "6281234567890", Body: "x"})
	if err == nil || !res.Retryable {
		t.Fatalf("500 harus error dan retryable (err=%v retryable=%v)", err, res.Retryable)
	}
}

func TestWAHANetworkFailureIsRetryable(t *testing.T) {
	// Port tertutup => kegagalan jaringan.
	p := NewWAHA(Config{Kind: KindWAHA, BaseURL: "http://127.0.0.1:1", APIKey: "k"})
	res, err := p.Send(context.Background(), Message{Destination: "6281234567890", Body: "x"})
	if err == nil {
		t.Fatal("kegagalan jaringan seharusnya error")
	}
	if !res.Retryable {
		t.Error("kegagalan jaringan harus retryable")
	}
}

func TestWAHASessionNameFromExtra(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	p := NewWAHA(Config{
		Kind: KindWAHA, BaseURL: srv.URL, APIKey: "k",
		Extra: map[string]any{"session": "noc"},
	})
	_, err := p.Send(context.Background(), Message{Destination: "6281234567890", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got["session"] != "noc" {
		t.Errorf("session = %v, ingin noc", got["session"])
	}
}

/* ---------------------------------------------------------------------------
   HTTP WhatsApp generik
   --------------------------------------------------------------------------- */

func TestHTTPWhatsAppSendFonnte(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token-fonnte" {
			t.Errorf("header Authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"status":true,"id":"12345"}`))
	}))
	defer srv.Close()

	p := NewFonnte(Config{Kind: KindFonnte, BaseURL: srv.URL, APIKey: "token-fonnte"})
	res, err := p.Send(context.Background(), Message{Destination: "08123456789", Body: "uji"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["target"] != "628123456789" {
		t.Errorf("target = %v, ingin 628123456789", got["target"])
	}
	if res.ProviderMessageID != "12345" {
		t.Errorf("id = %q", res.ProviderMessageID)
	}
}

func TestHTTPWhatsAppDetectsBodyFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP 200 tetapi body menandakan gagal.
		_, _ = w.Write([]byte(`{"status":false,"reason":"token habis"}`))
	}))
	defer srv.Close()

	p := NewFonnte(Config{Kind: KindFonnte, BaseURL: srv.URL, APIKey: "t"})
	if _, err := p.Send(context.Background(), Message{Destination: "628123456789", Body: "x"}); err == nil {
		t.Fatal("body status=false seharusnya terdeteksi gagal")
	}
}

func TestHTTPWhatsAppFormEncoded(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "x-www-form-urlencoded") {
			t.Errorf("content-type = %q", ct)
		}
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(`{"status":true}`))
	}))
	defer srv.Close()

	p := NewCustomHTTP(Config{
		Kind: KindCustomHTTP, BaseURL: srv.URL, APIKey: "k",
		Extra: map[string]any{"form_encoded": "true", "dest_field": "to", "text_field": "message"},
	})
	if _, err := p.Send(context.Background(), Message{Destination: "628123456789", Body: "pesan uji"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if form["to"][0] != "628123456789" {
		t.Errorf("form to = %v", form["to"])
	}
	if form["message"][0] != "pesan uji" {
		t.Errorf("form message = %v", form["message"])
	}
}

func TestHTTPWhatsAppRequiresBaseURL(t *testing.T) {
	p := NewFonnte(Config{Kind: KindFonnte, APIKey: "k"})
	if err := p.TestConnection(context.Background()); err == nil {
		t.Fatal("base URL kosong seharusnya gagal")
	}
}

/* ---------------------------------------------------------------------------
   Normalisasi nomor
   --------------------------------------------------------------------------- */

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+6281234567890":          "6281234567890",
		"6281234567890":           "6281234567890",
		"081234567890":            "6281234567890",
		"0812-3456-7890":          "6281234567890",
		"+62 812 3456 7890":       "6281234567890",
		"(0812) 3456.7890":        "6281234567890",
		"120363000000000000@g.us": "120363000000000000@g.us",
		"  08123  ":               "628123",
		"":                        "",
		"abc":                     "",
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, ingin %q", in, got, want)
		}
	}
}

/* ---------------------------------------------------------------------------
   Registry
   --------------------------------------------------------------------------- */

func TestRegistryResolvesKnownKinds(t *testing.T) {
	for _, kind := range []string{
		KindTelegramBot, KindWAHA, KindFonnte, KindWablas, KindStarsender, KindCustomHTTP,
	} {
		p, err := New(Config{Kind: kind, BaseURL: "http://localhost", APIKey: "k"})
		if err != nil {
			t.Errorf("New(%q) gagal: %v", kind, err)
			continue
		}
		if p.Kind() != kind {
			t.Errorf("New(%q).Kind() = %q", kind, p.Kind())
		}
	}
}

func TestRegistryRejectsUnknownKind(t *testing.T) {
	if _, err := New(Config{Kind: "pigeon"}); err == nil {
		t.Fatal("kind tidak dikenal seharusnya error")
	}
}

func TestKindsForChannel(t *testing.T) {
	wa := KindsForChannel("whatsapp")
	if len(wa) == 0 {
		t.Fatal("whatsapp harus punya provider")
	}
	found := false
	for _, k := range wa {
		if k == KindWAHA {
			found = true
		}
		if k == KindTelegramBot {
			t.Error("telegram tidak boleh masuk daftar provider whatsapp")
		}
	}
	if !found {
		t.Error("WAHA harus tersedia untuk kanal whatsapp")
	}

	tg := KindsForChannel("telegram")
	if len(tg) != 1 || tg[0] != KindTelegramBot {
		t.Errorf("KindsForChannel(telegram) = %v", tg)
	}
}

func TestTimeoutOrDefault(t *testing.T) {
	if got := timeoutOr(Config{}); got != DefaultTimeout {
		t.Errorf("timeout default = %v", got)
	}
	custom := Config{Timeout: 3e9 /* 3s */}
	if got := timeoutOr(custom); got != custom.Timeout {
		t.Errorf("timeout kustom tidak dipakai: %v", got)
	}
}
