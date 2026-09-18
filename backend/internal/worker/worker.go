// Package worker menjalankan seluruh job terjadwal.
//
// Pola diadopsi dari mcnvpn/internal/worker:
//   - robfig/cron v3 (cron 5-field standar) dengan lokasi waktu terkonfigurasi
//   - Start()/Stop() dengan penungguan graceful
//   - tiap job memakai context.WithTimeout sendiri
//
// Job yang sudah berfungsi sejak F5–F7:
//   - outbox_sender : mengirim notifikasi yang menunggu di outbox
//   - fanout        : mematerialisasi offset reminder/RFS yang jatuh tempo
//   - digest        : ringkasan harian untuk NOC
//   - retention     : memangkas data lama
//   - sla_tick      : kerangka mesin SLA (aktif F11)
package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/robfig/cron/v3"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
)

// Worker memiliki seluruh job background.
type Worker struct {
	cfg    *config.Config
	store  *repository.Store
	outbox *notify.Outbox
	fanout *notify.Fanout
	cron   *cron.Cron
}

// New membuat Worker baru.
func New(cfg *config.Config, store *repository.Store, outbox *notify.Outbox, fanout *notify.Fanout) *Worker {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Printf("worker: timezone %q tidak dikenal, memakai UTC", cfg.Timezone)
		loc = time.UTC
	}
	return &Worker{
		cfg:    cfg,
		store:  store,
		outbox: outbox,
		fanout: fanout,
		cron:   cron.New(cron.WithLocation(loc)),
	}
}

// Start mendaftarkan seluruh job lalu menjalankan scheduler.
func (w *Worker) Start() {
	log.Printf("worker: timezone=%s", w.cron.Location())

	senderEvery := fmt.Sprintf("@every %ds", maxInt(5, w.cfg.OutboxSenderInterval))
	if _, err := w.cron.AddFunc(senderEvery, w.runOutboxSender); err != nil {
		log.Printf("worker: jadwal outbox sender: %v", err)
	}

	fanoutEvery := fmt.Sprintf("@every %ds", maxInt(30, w.cfg.FanoutInterval))
	if _, err := w.cron.AddFunc(fanoutEvery, w.runFanout); err != nil {
		log.Printf("worker: jadwal fanout: %v", err)
	}

	// Recovery: kembalikan baris 'sending' yang menggantung (worker mati saat
	// mengirim) agar tidak tersangkut selamanya.
	if _, err := w.cron.AddFunc("@every 5m", w.runOutboxRecovery); err != nil {
		log.Printf("worker: jadwal recovery outbox: %v", err)
	}

	if _, err := w.cron.AddFunc("0 8 * * *", w.runDigest); err != nil {
		log.Printf("worker: jadwal digest: %v", err)
	}

	if _, err := w.cron.AddFunc("* * * * *", w.runSLATick); err != nil {
		log.Printf("worker: jadwal sla: %v", err)
	}

	if _, err := w.cron.AddFunc("0 3 * * *", w.runRetention); err != nil {
		log.Printf("worker: jadwal retensi: %v", err)
	}

	w.cron.Start()
	log.Printf("worker: seluruh job dijadwalkan")
}

// Stop menghentikan scheduler dan menunggu job yang sedang berjalan selesai.
func (w *Worker) Stop() {
	ctx := w.cron.Stop()
	<-ctx.Done()
	log.Printf("worker: job dihentikan")
}

/* ---------------------------------------------------------------------------
   Jobs
   --------------------------------------------------------------------------- */

// runOutboxSender mengirim notifikasi yang menunggu di outbox.
func (w *Worker) runOutboxSender() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	w.withHealth(ctx, "outbox_sender", func() error {
		res, err := w.outbox.Dispatch(ctx)
		if err != nil {
			return err
		}
		if res.Claimed > 0 || res.Failed > 0 {
			log.Printf("worker[outbox_sender]: claimed=%d sent=%d retried=%d failed=%d",
				res.Claimed, res.Sent, res.Retried, res.Failed)
		}
		return nil
	})
}

// runOutboxRecovery mengembalikan baris 'sending' yang menggantung ke 'pending'.
func (w *Worker) runOutboxRecovery() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	w.withHealth(ctx, "outbox_recovery", func() error {
		n, err := w.store.RecoverStuckOutbox(ctx, 10*time.Minute)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Printf("worker[outbox_recovery]: %d baris dikembalikan ke pending", n)
		}
		return nil
	})
}

// runFanout mematerialisasi offset reminder/RFS yang jatuh tempo ke outbox.
func (w *Worker) runFanout() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	w.withHealth(ctx, "fanout", func() error {
		before := time.Now()
		if err := w.fanout.Run(ctx); err != nil {
			return err
		}
		_ = before
		return nil
	})
}

// runDigest mengirim ringkasan harian ke target default.
func (w *Worker) runDigest() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	w.withHealth(ctx, "digest", func() error {
		targetID, err := w.store.DefaultTargetID(ctx)
		if err != nil {
			return fmt.Errorf("target default tidak ditemukan: %w", err)
		}

		upcoming, err := w.store.UpcomingForDigest(ctx)
		if err != nil {
			return err
		}
		counts, err := w.store.CountByTypeAndStatusRingkas(ctx)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		// Kunci unik per tanggal memakai zona waktu terkonfigurasi, sehingga
		// digest tidak terkirim dua kali pada hari yang sama.
		loc, _ := time.LoadLocation(w.cfg.Timezone)
		dayKey := now.In(loc).Format("2006-01-02")

		payload := notify.Payload{
			Title:     "Ringkasan Harian Ingat.in",
			CreatedAt: notify.FormatWIB(&now),
			Description: fmt.Sprintf(
				"Todo: %d · Reminder: %d · RFS: %d\nItem dalam 24 jam ke depan: %d",
				counts["task"], counts["reminder"], counts["rfs"], len(upcoming)),
		}

		lines := ""
		for _, it := range upcoming {
			when := notify.FormatWIB(it.ExpireAt)
			if it.ExpireAt == nil {
				when = notify.FormatWIB(it.DueAt)
			}
			lines += fmt.Sprintf("\n• %s %s (%s)", it.RefNo, it.Title, when)
		}
		if lines == "" {
			lines = "\n(tidak ada item yang jatuh tempo dalam 24 jam ke depan)"
		}
		payload.Notes = lines

		res, err := w.outbox.Enqueue(ctx, notify.EnqueueParams{
			EventKey:    "digest:" + dayKey,
			SourceType:  "system",
			TemplateKey: notify.TemplateTestMessage,
			Severity:    "info",
			TargetID:    *targetID,
			Payload:     payload,
		})
		if err != nil {
			return err
		}
		if res.Added > 0 {
			log.Printf("worker[digest]: ringkasan %s diantrikan (%d pesan)", dayKey, res.Added)
		}
		return nil
	})
}

// runSLATick adalah kerangka mesin SLA (diaktifkan pada F11).
func (w *Worker) runSLATick() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w.withHealth(ctx, "sla_tick", func() error {
		// F11: evaluasi seluruh work item ber-sla_policy terhadap sla_targets,
		// perbarui sla_state, lalu tembak SLA_WARNING/SLA_BREACH ke outbox.
		return nil
	})
}

// runRetention memangkas data lama sesuai konfigurasi retensi.
func (w *Worker) runRetention() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	w.withHealth(ctx, "retention", func() error {
		now := time.Now().UTC()

		outboxCutoff := now.Add(-time.Duration(w.cfg.OutboxRetentionDays) * 24 * time.Hour)
		nOutbox, err := w.store.PurgeOutbox(ctx, outboxCutoff)
		if err != nil {
			return fmt.Errorf("pangkas outbox: %w", err)
		}

		auditCutoff := now.Add(-time.Duration(w.cfg.AuditRetentionDays) * 24 * time.Hour)
		nAudit, err := w.store.PurgeAuditLogs(ctx, auditCutoff)
		if err != nil {
			return fmt.Errorf("pangkas audit: %w", err)
		}

		// Refresh token yang sudah dicabut/kedaluwarsa tidak perlu disimpan.
		nTokens, err := w.store.PurgeExpiredRefreshTokens(ctx, auditCutoff)
		if err != nil {
			return fmt.Errorf("pangkas refresh token: %w", err)
		}

		if nOutbox+nAudit+nTokens > 0 {
			log.Printf("worker[retention]: outbox=%d audit=%d refresh_token=%d dihapus",
				nOutbox, nAudit, nTokens)
		}
		return nil
	})
}

/* ---------------------------------------------------------------------------
   Pembungkus
   --------------------------------------------------------------------------- */

// withHealth membungkus job agar error/panic tercatat tanpa menghentikan scheduler.
func (w *Worker) withHealth(ctx context.Context, name string, fn func() error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("worker[%s]: panic: %v", name, rec)
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := w.store.Ping(pingCtx); err != nil {
		log.Printf("worker[%s]: database tidak dapat dijangkau: %v", name, err)
		return
	}

	start := time.Now()
	if err := fn(); err != nil {
		log.Printf("worker[%s]: gagal setelah %s: %v", name, time.Since(start).Round(time.Millisecond), err)
		return
	}
	// Job yang lambat selalu dicatat; job cepat hanya bila melewati ambang
	// (menghindari log spam setiap 15 detik pada sistem yang tenang).
	if d := time.Since(start); d > 2*time.Second {
		log.Printf("worker[%s]: selesai dalam %s", name, d.Round(time.Millisecond))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
