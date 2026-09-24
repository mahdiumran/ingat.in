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
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"ingatin/backend/internal/backup"
	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
	"ingatin/backend/internal/sla"
)

// Worker memiliki seluruh job background.
type Worker struct {
	cfg    *config.Config
	store  *repository.Store
	outbox *notify.Outbox
	fanout *notify.Fanout
	sheets *sheets.Queue
	cron   *cron.Cron
}

// New membuat Worker baru.
func New(cfg *config.Config, store *repository.Store, outbox *notify.Outbox, fanout *notify.Fanout, sheetQueue *sheets.Queue) *Worker {
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
		sheets: sheetQueue,
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

	// F22: ringkasan tugas harian (pending/in_progress/done). Interval tetap
	// setiap menit; pengiriman sebenarnya mengikuti setting summary_mode &
	// summary_interval_min sehingga dapat diubah operator tanpa deploy ulang.
	if _, err := w.cron.AddFunc("* * * * *", w.runDailySummaryTick); err != nil {
		log.Printf("worker: jadwal ringkasan tugas: %v", err)
	}

	if _, err := w.cron.AddFunc("* * * * *", w.runSLATick); err != nil {
		log.Printf("worker: jadwal sla: %v", err)
	}

	if _, err := w.cron.AddFunc("0 3 * * *", w.runRetention); err != nil {
		log.Printf("worker: jadwal retensi: %v", err)
	}

	// F18: sinkronisasi spreadsheet + pemulihan entri menggantung.
	sheetEvery := fmt.Sprintf("@every %ds", maxInt(30, w.cfg.SheetSyncInterval))
	if _, err := w.cron.AddFunc(sheetEvery, w.runSheetSync); err != nil {
		log.Printf("worker: jadwal sheet sync: %v", err)
	}
	if _, err := w.cron.AddFunc("@every 5m", w.runSheetRecovery); err != nil {
		log.Printf("worker: jadwal pemulihan sheet sync: %v", err)
	}

	// F34: cadangan otomatis. Pemeriksaan tiap menit; eksekusi mengikuti jadwal
	// cron yang disimpan pada settings (dapat diubah operator tanpa deploy).
	if _, err := w.cron.AddFunc("* * * * *", w.runBackupTick); err != nil {
		log.Printf("worker: jadwal cadangan: %v", err)
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

// runDailySummaryTick mengirim ringkasan tugas harian sesuai jadwal setting.
//
// Mode 'off' menonaktifkan; 'on_change' hanya dikirim saat status berubah (dari
// API); 'interval' mengirim tiap N menit. Kunci outbox memakai tanggal + jam
// agar tidak dobel dalam satu slot.
func (w *Worker) runDailySummaryTick() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	w.withHealth(ctx, "daily_summary", func() error {
		cfg, err := w.store.GetSetting(ctx, "daily_task.summary_mode")
		if err != nil {
			return nil // setting belum ada → nonaktif
		}
		mode, _ := cfg["value"].(string)
		mode = strings.TrimSpace(strings.ToLower(mode))
		if mode != "interval" {
			return nil
		}

		interval := 60
		if v, err := w.store.GetSetting(ctx, "daily_task.summary_interval_min"); err == nil {
			switch n := v["value"].(type) {
			case float64:
				interval = int(n)
			case string:
				if p, e := strconv.Atoi(strings.TrimSpace(n)); e == nil {
					interval = p
				}
			}
		}
		if interval < 5 {
			interval = 5
		}

		now := time.Now().UTC()
		// Kirim hanya pada kelipatan interval (menit) agar hemat.
		if now.Minute()%interval != 0 {
			return nil
		}

		if _, err := w.sendDailyTaskSummary(ctx, now); err != nil {
			return err
		}
		return nil
	})
}

// sendDailyTaskSummary membangun & mengantrikan ringkasan tugas harian untuk
// hari berjalan (WIB). Kunci unik per tanggal+jam-slot.
func (w *Worker) sendDailyTaskSummary(ctx context.Context, now time.Time) (int, error) {
	targetID, err := w.store.DefaultTargetID(ctx)
	if err != nil {
		return 0, fmt.Errorf("target default tidak ditemukan: %w", err)
	}

	loc, err := time.LoadLocation(w.cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	sum, err := w.store.ListDailyTaskSummary(ctx, dayStart.UTC(), dayEnd.UTC())
	if err != nil {
		return 0, err
	}

	group := notify.SummaryGroup{
		Pending:    toSummaryTasks(sum.Pending),
		InProgress: toSummaryTasks(sum.InProgress),
		Done:       toSummaryTasks(sum.Done),
	}
	dayLabel := local.Format("02 Jan 2006 15:04")
	desc, notes := notify.BuildDailySummary(dayLabel, group)

	payload := notify.Payload{
		Title:       "Ringkasan Tugas Harian",
		CreatedAt:   notify.FormatWIB(&now),
		Description: desc,
		Notes:       notes,
	}

	// Kunci unik per tanggal+slot jam agar tidak dobel pada slot yang sama.
	slotKey := local.Format("2006-01-02-15")
	res, err := w.outbox.Enqueue(ctx, notify.EnqueueParams{
		EventKey:    "daily_summary:" + slotKey,
		SourceType:  "system",
		TemplateKey: notify.TemplateDailySummary,
		Severity:    "info",
		TargetID:    *targetID,
		Payload:     payload,
	})
	if err != nil {
		return 0, err
	}
	if res.Added > 0 {
		log.Printf("worker[daily_summary]: ringkasan %s diantrikan", slotKey)
	}
	return res.Added, nil
}

func toSummaryTasks(in []repository.DailyTaskSummaryItem) []notify.SummaryTask {
	out := make([]notify.SummaryTask, 0, len(in))
	for _, it := range in {
		out = append(out, notify.SummaryTask{
			RefNo:   it.RefNo,
			Title:   it.Title,
			Owner:   it.Owner,
			DueAt:   notify.FormatWIB(it.DueAt),
			Overdue: it.Overdue,
		})
	}
	return out
}

// runSLATick mengevaluasi SLA tiket yang sedang berjalan (F20).
//
// Setiap siklus aktif dihitung terpisah: respons pertama dan penyelesaian
// terhadap target policy (per prioritas). Hasilnya memperbarui sla_state
// work item; bila melewati target, notifikasi SLA_BREACH diantrikan (sekali).
func (w *Worker) runSLATick() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	w.withHealth(ctx, "sla_tick", func() error {
		rows, err := w.store.ListOpenSLACycles(ctx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		breached := 0

		for _, r := range rows {
			targets := sla.Targets{
				FirstResponseMinutes: r.TargetResponse,
				ResolutionMinutes:    r.TargetResolution,
			}
			if targets.FirstResponseMinutes == 0 || targets.ResolutionMinutes == 0 {
				targets = sla.TargetsForPriority(r.Priority)
			}

			st := sla.Evaluate(r.OpenedAt, r.FirstResponseAt, nil, targets, now)
			state := models.SLAStateOnTrack
			var breachedAt *time.Time
			if st.Breached {
				state = models.SLAStateBreached
				breachedAt = &now
			}

			if err := w.store.SetWorkItemSLAState(ctx, r.WorkItemID, state, breachedAt); err != nil {
				log.Printf("worker[sla_tick]: gagal set state %s: %v", r.RefNo, err)
				continue
			}
			if !st.Breached {
				continue
			}
			breached++
			w.notifySLABreach(ctx, r, targets, now)
		}
		if len(rows) > 0 {
			log.Printf("worker[sla_tick]: dievaluasi=%d breached=%d", len(rows), breached)
		}
		return nil
	})
}

// notifySLABreach mengantrikan notifikasi pelanggaran SLA sekali per siklus.
func (w *Worker) notifySLABreach(ctx context.Context, r repository.OpenSLACycleRow, t sla.Targets, now time.Time) {
	targetID, err := w.store.DefaultTargetID(ctx)
	if err != nil {
		return
	}
	targetWindow := time.Duration(t.ResolutionMinutes) * time.Minute
	payload := notify.Payload{
		RefNo:       r.RefNo,
		Title:       r.Title,
		ItemType:    r.ItemType,
		Priority:    r.Priority,
		Owner:       r.OwnerUsername,
		CreatedAt:   notify.FormatWIB(&now),
		Remaining:   notify.HumanRemaining(r.OpenedAt.Add(targetWindow), now),
		SubjectName: r.Title,
		Category:    "SLA",
	}
	_, _ = w.outbox.Enqueue(ctx, notify.EnqueueParams{
		EventKey:    fmt.Sprintf("sla_breach:%s:%d", r.WorkItemID, r.CycleNo),
		WorkItemID:  &r.WorkItemID,
		SourceType:  "system",
		TemplateKey: notify.TemplateSLABreach,
		Severity:    models.SeverityCritical,
		TargetID:    *targetID,
		Payload:     payload,
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

// runSheetSync menulis antrean sinkronisasi spreadsheet (F18).
func (w *Worker) runSheetSync() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	w.withHealth(ctx, "sheet_sync", func() error {
		res, err := w.sheets.Dispatch(ctx)
		if err != nil {
			return err
		}
		sheets.LogSummary(res)
		return nil
	})
}

// runSheetRecovery mengembalikan entri 'sending' yang menggantung ke pending.
func (w *Worker) runSheetRecovery() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	w.withHealth(ctx, "sheet_sync_recovery", func() error {
		n, err := w.sheets.RecoverStuck(ctx, 10*time.Minute)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Printf("worker[sheet_sync_recovery]: %d entri dikembalikan ke pending", n)
		}
		return nil
	})
}

// runBackupTick menjalankan cadangan otomatis (F34) sesuai jadwal yang
// disimpan pada settings. Dipanggil tiap menit; keputusan "sekarang" dihitung
// dengan mencocokkan waktu lokal dengan ekspresi cron berikut jadwal terakhir.
func (w *Worker) runBackupTick() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	w.withHealth(ctx, "backup", func() error {
		cfg, err := backup.LoadConfig(ctx, w.store)
		if err != nil {
			return err
		}
		if !cfg.Enabled {
			return nil
		}

		schedule, err := cron.ParseStandard(cfg.Schedule)
		if err != nil {
			return fmt.Errorf("jadwal cadangan tidak valid %q: %w", cfg.Schedule, err)
		}

		now := time.Now().In(w.cron.Location())
		// Jadwal berikut setelah jadwal terakhir; bila sudah lewat "now", jalankan.
		var base time.Time
		if cfg.LastRunAt != nil {
			if t, perr := time.Parse(time.RFC3339, *cfg.LastRunAt); perr == nil {
				base = t.In(w.cron.Location())
			}
		}
		if base.IsZero() {
			// Belum pernah berjalan: jalankan bila slot jadwal hari ini sudah lewat.
			base = now.Add(-24 * time.Hour)
		}
		next := schedule.Next(base)
		if next.After(now) {
			return nil
		}

		dir := w.cfg.BackupDir
		if dir == "" {
			dir = "/app/data/backups"
		}

		info, err := backup.Create(ctx, backup.DumpOptions{
			DBURL: w.cfg.DBURL,
			Dir:   dir,
			Label: "auto",
		})
		if err != nil {
			_ = backup.TouchRun(context.Background(), w.store, "error", err.Error(), "", now.UTC().Format(time.RFC3339), "worker")
			return err
		}

		// Pemangkasan retensi lokal.
		if removed, perr := backup.Prune(dir, cfg.KeepDays); perr != nil {
			log.Printf("worker[backup]: pangkas gagal: %v", perr)
		} else if removed > 0 {
			log.Printf("worker[backup]: %d berkas lama dihapus", removed)
		}

		// Unggah ke FTP bila diaktifkan.
		if cfg.FTP.Enabled && strings.TrimSpace(cfg.FTP.Host) != "" {
			ftpCfg, ferr := cfg.FTPConfig(w.store, w.cfg.CredentialKey)
			if ferr != nil {
				_ = backup.TouchRun(context.Background(), w.store, "error", ferr.Error(), info.Name, now.UTC().Format(time.RFC3339), "worker")
				return ferr
			}
			full, perr := backup.Path(dir, info.Name)
			if perr != nil {
				return perr
			}
			if uerr := backup.UploadFile(ftpCfg, full, info.Name); uerr != nil {
				_ = backup.TouchRun(context.Background(), w.store, "error", uerr.Error(), info.Name, now.UTC().Format(time.RFC3339), "worker")
				return uerr
			}
			log.Printf("worker[backup]: %s dibuat & diunggah ke FTP", info.Name)
			return backup.TouchRun(context.Background(), w.store, "ok", "", info.Name, now.UTC().Format(time.RFC3339), "worker")
		}

		log.Printf("worker[backup]: %s dibuat (lokal)", info.Name)
		return backup.TouchRun(context.Background(), w.store, "ok", "", info.Name, now.UTC().Format(time.RFC3339), "worker")
	})
}

/* ---------------------------------------------------------------------------
   Pembungkus
   --------------------------------------------------------------------------- */
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
