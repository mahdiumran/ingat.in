package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
)

// Update adalah bagian dari payload webhook Telegram yang kami butuhkan.
type Update struct {
	UpdateID int64          `json:"update_id"`
	Message  *UpdateMessage `json:"message,omitempty"`
}

// UpdateMessage adalah subset message Telegram.
type UpdateMessage struct {
	MessageID int64        `json:"message_id"`
	Text      string       `json:"text"`
	Chat      UpdateChat   `json:"chat"`
	From      *UpdateActor `json:"from,omitempty"`
}

// UpdateChat adalah subset chat Telegram.
type UpdateChat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// UpdateActor adalah subset pengirim pesan.
type UpdateActor struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// DisplayName mengembalikan nama tampil pengirim (first_name → @username).
func (a *UpdateActor) DisplayName() string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.FirstName) != "" {
		return a.FirstName
	}
	if strings.TrimSpace(a.Username) != "" {
		return "@" + a.Username
	}
	return ""
}

// Dispatcher mengeksekusi command bot.
type Dispatcher struct {
	cfg      *config.Config
	store    *repository.Store
	resolver *notify.Resolver
	sheets   *sheets.Queue
	loc      *time.Location

	// rate limiting sederhana per chat (anti-spam /open).
	mu     sync.Mutex
	bursts map[int64][]time.Time
}

// NewDispatcher membuat Dispatcher. loc diambil dari cfg.Timezone.
func NewDispatcher(cfg *config.Config, store *repository.Store, resolver *notify.Resolver, sheetQueue *sheets.Queue) *Dispatcher {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return &Dispatcher{
		cfg:      cfg,
		store:    store,
		resolver: resolver,
		sheets:   sheetQueue,
		loc:      loc,
		bursts:   map[int64][]time.Time{},
	}
}

// Enabled melaporkan apakah bot aktif (secret webhook terisi).
func Enabled(cfg *config.Config) bool {
	return strings.TrimSpace(cfg.TelegramWebhookSecret) != ""
}

// rate limit: maksimum maksPerWindow command per jendela waktu per chat.
const (
	rateWindow   = time.Minute
	rateMaxPerCh = 20
)

// allowRate membatasi laju command per chat.
func (d *Dispatcher) allowRate(chatID int64) bool {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := now.Add(-rateWindow)
	kept := d.bursts[chatID][:0]
	for _, t := range d.bursts[chatID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rateMaxPerCh {
		d.bursts[chatID] = kept
		return false
	}
	d.bursts[chatID] = append(kept, now)
	return true
}

// HandleUpdate memproses satu update webhook Telegram.
//
// Idempoten terhadap update_id: kiriman ulang dari Telegram diabaikan. Seluruh
// kesalahan ditelan (dicatat ke log) karena webhook harus tetap membalas 2xx.
func (d *Dispatcher) HandleUpdate(ctx context.Context, u Update) {
	if u.UpdateID == 0 || u.Message == nil {
		return
	}
	msg := u.Message

	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// 1. Idempotensi: catat update_id; lewati bila sudah pernah diproses.
	fresh, err := d.store.MarkBotUpdate(ctx, u.UpdateID, msg.Chat.ID)
	if err != nil {
		log.Printf("bot: gagal mencatat update %d: %v", u.UpdateID, err)
		return
	}
	if !fresh {
		return
	}

	cmd, ok := parseCommand(msg.Text)
	if !ok {
		return // bukan command (abaikan pesan biasa)
	}

	// 2. Allowlist grup (master data "telegram_chat").
	group, err := d.store.GetTelegramChat(ctx, chatID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Grup tak terdaftar: balas hanya untuk /id & /help agar operator
			// bisa mendaftarkan grupnya, selain itu diabaikan.
			if cmd.Name == "id" || cmd.Name == "help" || cmd.Name == "start" {
				d.reply(ctx, chatID, d.helpUnregistered(chatID))
			}
			return
		}
		log.Printf("bot: cek allowlist chat %s: %v", chatID, err)
		return
	}

	// 3. Rate limit.
	if !d.allowRate(msg.Chat.ID) {
		d.reply(ctx, chatID, "⚠️ Terlalu banyak perintah. Coba lagi sebentar lagi.")
		return
	}

	// 4. Eksekusi.
	res := d.dispatch(ctx, cmd, group, msg)
	if strings.TrimSpace(res.reply) != "" {
		d.reply(ctx, chatID, res.reply)
	}

	// 5. Audit command (best-effort).
	if err := d.store.LogBotCommand(ctx, repository.LogBotCommandParams{
		ChatID:      msg.Chat.ID,
		GroupLabel:  group.Label,
		Username:    msg.From.DisplayName(),
		Command:     cmd.Name,
		Args:        cmd.Raw,
		WorkItemRef: res.workRef,
		Result:      res.result,
	}); err != nil {
		log.Printf("bot: gagal mencatat command %q: %v", cmd.Name, err)
	}
}

// helpUnregistered menghasilkan pesan pendaftaran grup.
func (d *Dispatcher) helpUnregistered(chatID string) string {
	return fmt.Sprintf(
		"🔒 Grup ini belum terdaftar untuk bot.\n\n"+
			"🆔 Chat ID grup ini:\n%s\n\n"+
			"Minta admin menambahkannya di panel:\n"+
			"Master Data → Grup Telegram Bot → Tambah\n(Kode = chat id di atas).",
		chatID)
}
