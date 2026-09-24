// Command ingatin adalah entrypoint tunggal untuk seluruh mode operasi.
//
// Satu binary, tiga mode:
//
//	-mode=migrate   terapkan migrasi lalu keluar (dijalankan sebagai service run-once)
//	-mode=api       HTTP API server
//	-mode=worker    background worker (cron jobs)
//
// Urutan startup mengikuti pola yang terbukti di mcnvpn:
// config -> signal context -> migrate -> connect pool -> store -> services ->
// worker.start -> http.Server -> graceful shutdown.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ingatin/backend/internal/api"
	"ingatin/backend/internal/attachments"
	"ingatin/backend/internal/auth"
	"ingatin/backend/internal/bot"
	"ingatin/backend/internal/config"
	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/db"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
	"ingatin/backend/internal/worker"
)

// version diisi saat build dengan -ldflags "-X main.buildVersion=...".
var buildVersion = "dev"

func main() {
	var (
		modeFlag = flag.String("mode", "", "mode operasi: api | worker | migrate (default dari INGATIN_MODE)")
		showVer  = flag.Bool("version", false, "tampilkan versi lalu keluar")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("ingatin %s\n", buildVersion)
		return
	}

	// Set mode dari flag bila diberikan (mengalahkan env).
	if *modeFlag != "" {
		if err := os.Setenv("INGATIN_MODE", *modeFlag); err != nil {
			log.Fatalf("gagal set mode: %v", err)
		}
	}
	os.Setenv("INGATIN_APP_VERSION", buildVersion)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("konfigurasi: %v", err)
	}

	setupLogging(cfg)

	log.Printf("ingatin %s — mode=%s", buildVersion, cfg.Mode)
	log.Printf("config: %s", string(cfg.Marshal()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case config.ModeMigrate:
		if err := runMigrate(ctx, cfg); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	case config.ModeAPI:
		if err := runAPI(ctx, cfg); err != nil {
			log.Fatalf("api: %v", err)
		}
	case config.ModeWorker:
		if err := runWorker(ctx, cfg); err != nil {
			log.Fatalf("worker: %v", err)
		}
	default:
		log.Fatalf("mode tidak dikenal: %s", cfg.Mode)
	}

	log.Printf("ingatin berhenti dengan bersih")
}

// runMigrate menerapkan migrasi skema lalu keluar.
func runMigrate(ctx context.Context, cfg *config.Config) error {
	if cfg.DBURL == "" {
		return errors.New("INGATIN_DB_URL wajib diisi untuk migrasi")
	}
	if err := db.Migrate(ctx, cfg.DBURL); err != nil {
		return err
	}
	if v, err := db.SchemaVersion(ctx, cfg.DBURL); err == nil {
		repository.SetSchemaVersion(v)
		log.Printf("versi skema saat ini: %d", v)
	}
	return nil
}

// runAPI menjalankan HTTP API server.
func runAPI(ctx context.Context, cfg *config.Config) error {
	pool, err := db.Connect(ctx, cfg.DBURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if v, err := db.SchemaVersion(ctx, cfg.DBURL); err == nil {
		repository.SetSchemaVersion(v)
	}

	store := repository.New(pool)

	if err := bootstrapAdmin(ctx, cfg, store); err != nil {
		log.Printf("bootstrap admin: %v", err)
	}

	if err := bootstrapWAHAProvider(ctx, cfg, store); err != nil {
		log.Printf("bootstrap provider WAHA: %v", err)
	}

	authMgr := auth.New(cfg, store)
	notifier := notify.NewResolver(cfg, store)
	sheetQueue := sheets.NewQueue(cfg, store)
	attachStore := attachments.New(cfg, store)

	// F12: bot command Telegram (inbound). Aktif hanya bila secret webhook diisi.
	var botDisp *bot.Dispatcher
	if bot.Enabled(cfg) {
		botDisp = bot.NewDispatcher(cfg, store, notifier, sheetQueue)
		log.Printf("telegram bot: aktif (webhook)")
	} else {
		log.Printf("telegram bot: nonaktif (INGATIN_TELEGRAM_WEBHOOK_SECRET kosong)")
	}

	srv := api.New(cfg, store, authMgr, notifier, sheetQueue, attachStore, botDisp)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("api: mendengarkan pada %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Printf("api: mematikan server...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("api: shutdown: %v", err)
	}
	return nil
}

// runWorker menjalankan background worker (cron jobs).
func runWorker(ctx context.Context, cfg *config.Config) error {
	pool, err := db.Connect(ctx, cfg.DBURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if v, err := db.SchemaVersion(ctx, cfg.DBURL); err == nil {
		repository.SetSchemaVersion(v)
	}

	store := repository.New(pool)
	resolver := notify.NewResolver(cfg, store)
	outbox := notify.NewOutbox(cfg, store, resolver)
	fanout := notify.NewFanout(store, outbox)
	sheetQueue := sheets.NewQueue(cfg, store)
	w := worker.New(cfg, store, outbox, fanout, sheetQueue)
	w.Start()
	defer w.Stop()

	log.Printf("worker: berjalan; menunggu sinyal berhenti")
	<-ctx.Done()
	return nil
}

// bootstrapAdmin membuat user admin awal bila belum ada.
// Password tidak pernah di-seed lewat migrasi agar tidak tersimpan di file SQL.
func bootstrapAdmin(ctx context.Context, cfg *config.Config, store *repository.Store) error {
	if cfg.AdminPassword == "" {
		log.Printf("bootstrap admin dilewati: INGATIN_ADMIN_PASSWORD kosong")
		return nil
	}
	exists, err := store.UserExists(ctx, cfg.AdminUsername)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	hash, err := crypto.HashPassword(cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("hash password admin: %w", err)
	}
	if err := store.CreateAdminUser(ctx, cfg.AdminUsername, cfg.AdminEmail, hash); err != nil {
		return fmt.Errorf("buat admin: %w", err)
	}
	log.Printf("bootstrap admin: user %q dibuat", cfg.AdminUsername)
	return nil
}

// bootstrapWAHAProvider memastikan provider WhatsApp/WAHA awal tersedia.
//
// WAHA berjalan sebagai container sendiri dengan API key yang juga dipakai
// untuk dashboard. Agar tidak ada dua sumber kebenaran (dan agar admin tidak
// perlu menyalin API key secara manual), provider dibuat otomatis dari
// INGATIN_WAHA_BASE_URL + INGATIN_WAHA_API_KEY bila belum ada provider
// WhatsApp sama sekali. Bila sudah ada, fungsi ini tidak menyentuh apa pun
// sehingga perubahan manual admin tetap dihormati.
func bootstrapWAHAProvider(ctx context.Context, cfg *config.Config, store *repository.Store) error {
	if strings.TrimSpace(cfg.WahaAPIKey) == "" {
		log.Printf("bootstrap provider WAHA dilewati: INGATIN_WAHA_API_KEY kosong")
		return nil
	}

	n, err := store.CountProvidersByChannel(ctx, models.ChannelWhatsApp)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	enc, err := crypto.Encrypt(cfg.CredentialKey, cfg.WahaAPIKey)
	if err != nil {
		return fmt.Errorf("enkripsi API key WAHA: %w", err)
	}

	baseURL := strings.TrimSpace(cfg.WahaBaseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8082"
	}

	if _, err := store.CreateProvider(ctx, repository.CreateProviderParams{
		Channel:   models.ChannelWhatsApp,
		Kind:      "waha",
		Label:     "WAHA (WhatsApp)",
		BaseURL:   baseURL,
		APIKeyEnc: enc,
		Extra:     map[string]any{"session": "default"},
		IsDefault: true,
		IsActive:  true,
	}); err != nil {
		return fmt.Errorf("buat provider WAHA: %w", err)
	}
	log.Printf("bootstrap provider WAHA: provider WhatsApp dibuat (base_url=%s)", baseURL)
	return nil
}

// setupLogging mengatur prefiks log standar.
func setupLogging(cfg *config.Config) {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("")
	log.Printf("log: level=%s timezone=%s (log waktu UTC)", cfg.LogLevel, cfg.Timezone)
}
