package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"ingatin/backend/internal/bot"
)

// handleTelegramWebhook menerima update Telegram lalu meneruskannya ke
// Dispatcher. Endpoint bersifat publik namun diamankan dua lapis:
//
//  1. path secret  /api/hooks/telegram/{secret}  (dibandingkan konstan-waktu)
//  2. header X-Telegram-Bot-Api-Secret-Token (secret_token saat setWebhook)
//
// Selalu membalas 200 agar Telegram tidak mengirim ulang update; pemrosesan
// yang gagal dicatat ke log (dan update_id tetap tercatat sehingga tidak
// diproses dua kali).
func (s *Server) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if s.bot == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Lapis 1: secret pada path.
	got := chi.URLParam(r, "secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.TelegramWebhookSecret)) != 1 {
		w.WriteHeader(http.StatusOK) // jangan bocorkan apa pun ke pemanggil
		return
	}

	// Lapis 2: header secret (bila di-set saat registrasi webhook).
	if want := r.Header.Get("X-Telegram-Bot-Api-Secret-Token"); want != "" {
		if subtle.ConstantTimeCompare([]byte(want), []byte(s.cfg.TelegramWebhookSecret)) != 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	var u bot.Update
	if err := json.Unmarshal(body, &u); err != nil {
		log.Printf("bot: payload webhook tidak valid: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	s.bot.HandleUpdate(r.Context(), u)
	w.WriteHeader(http.StatusOK)
}

// registerTelegramHookRoute mendaftarkan route webhook hanya bila bot aktif.
func (s *Server) registerTelegramHookRoute(r chi.Router) {
	if s.bot == nil || !bot.Enabled(s.cfg) {
		return
	}
	r.Post("/hooks/telegram/{secret}", s.handleTelegramWebhook)
	log.Printf("bot: webhook telegram aktif di POST /api/hooks/telegram/{secret}")
}
