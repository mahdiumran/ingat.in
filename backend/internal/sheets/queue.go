package sheets

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// Queue mengelola antrean sinkronisasi spreadsheet.
//
// Alur:
//  1. Enqueue dipanggil saat Todo Task dibuat/diubah (best-effort).
//  2. Dispatch dipanggil berkala oleh worker: klaim batch, tulis ke Sheets,
//     lalu perbarui status + backoff.
type Queue struct {
	cfg   *config.Config
	store *repository.Store
}

// NewQueue membuat Queue.
func NewQueue(cfg *config.Config, store *repository.Store) *Queue {
	return &Queue{cfg: cfg, store: store}
}

// EnqueueParams adalah parameter penambahan entri antrean.
type EnqueueParams struct {
	WorkItemID uuid.UUID
	RefNo      string
	Op         string
	Action     string
	Payload    map[string]any
}

// DispatchResult merangkum hasil satu putaran sinkronisasi.
type DispatchResult struct {
	Claimed int
	Sent    int
	Retried int
	Failed  int
	Skipped int
}

// Dispatch mengambil batch entri siap kirim dan menuliskannya ke spreadsheet.
//
// Bila sinkronisasi dinonaktifkan atau belum dikonfigurasi, entri dibiarkan
// mengantre (tidak dihabiskan) sehingga aktif kembali setelah admin melengkapi
// konfigurasi.
func (q *Queue) Dispatch(ctx context.Context) (DispatchResult, error) {
	var res DispatchResult

	cfgRow, sa, err := LoadConfig(ctx, q.store, q.cfg.CredentialKey)
	if err != nil {
		return res, err
	}
	if !cfgRow.Enabled {
		return res, nil
	}
	if sa == nil || cfgRow.SpreadsheetID == "" {
		return res, fmt.Errorf("%w (spreadsheet_id/service account belum diisi)", ErrNotConfigured)
	}
	if err := ValidateSheetName(cfgRow.SheetName); err != nil {
		return res, err
	}

	rows, err := q.store.ClaimSheetSync(ctx, q.cfg.OutboxBatchSize)
	if err != nil {
		return res, err
	}
	res.Claimed = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	client, err := NewClient(ctx, sa.RawJSON(), cfgRow.SpreadsheetID, cfgRow.SheetName)
	if err != nil {
		// Kredensial/nama sheet bermasalah = kegagalan konfigurasi, bukan
		// kesalahan per-baris; tunda seluruh batch lalu laporkan.
		for _, row := range rows {
			_ = q.scheduleRetry(ctx, row, err)
			res.Retried++
		}
		_ = q.store.RecordSheetSyncResult(ctx, err.Error())
		return res, err
	}

	// Pastikan tab + header ada (idempoten, hanya sekali per dispatch).
	if err := client.CreateSheetTab(ctx); err != nil {
		for _, row := range rows {
			_ = q.scheduleRetry(ctx, row, err)
			res.Retried++
		}
		_ = q.store.RecordSheetSyncResult(ctx, err.Error())
		return res, err
	}
	if err := client.EnsureHeader(ctx); err != nil {
		for _, row := range rows {
			_ = q.scheduleRetry(ctx, row, err)
			res.Retried++
		}
		_ = q.store.RecordSheetSyncResult(ctx, err.Error())
		return res, err
	}

	// Cache pemetaan Ref -> baris agar tidak membaca sheet berulang.
	rowCache := map[string]int{}
	lastErr := ""

	for _, row := range rows {
		if err := q.deliver(ctx, client, row, rowCache); err != nil {
			lastErr = err.Error()
			if row.Status == models.SheetSyncFailed {
				res.Failed++
			} else {
				res.Retried++
			}
		} else {
			res.Sent++
		}
	}

	_ = q.store.RecordSheetSyncResult(ctx, lastErr)
	return res, nil
}

// deliver menulis satu entri antrean ke spreadsheet lalu memperbarui statusnya.
func (q *Queue) deliver(ctx context.Context, client *Client, row models.SheetSyncQueueRow, rowCache map[string]int) error {
	values := RowFromPayload(row.Payload)

	// Tentukan baris target: cari lewat cache, lalu langsung ke sheet.
	rowIndex, ok := rowCache[row.RefNo]
	if !ok {
		idx, err := client.FindRow(ctx, row.RefNo)
		if err != nil {
			return q.scheduleRetry(ctx, row, err)
		}
		rowIndex = idx
		rowCache[row.RefNo] = idx
	}

	if rowIndex == 0 {
		// Baris belum ada → append, apa pun nilai op (menutup kasus sheet
		// dibersihkan manual oleh operator).
		if err := client.AppendRow(ctx, values); err != nil {
			return q.scheduleRetry(ctx, row, err)
		}
	} else {
		if err := client.UpdateRow(ctx, rowIndex, values); err != nil {
			return q.scheduleRetry(ctx, row, err)
		}
	}

	if err := q.store.MarkSheetSyncSent(ctx, row.ID); err != nil {
		return err
	}
	return nil
}

// scheduleRetry mencatat kegagalan dan menjadwalkan ulang bila masih layak.
func (q *Queue) scheduleRetry(ctx context.Context, row models.SheetSyncQueueRow, cause error) error {
	attempts := row.Attempts + 1
	maxAttempts := row.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if attempts >= maxAttempts {
		return q.store.MarkSheetSyncFailed(ctx, row.ID, attempts, cause.Error())
	}
	next := time.Now().UTC().Add(sheetBackoff(attempts))
	return q.store.RescheduleSheetSync(ctx, row.ID, attempts, next, cause.Error())
}

// RecoverStuck mengembalikan entri 'sending' yang menggantung ke 'pending'.
func (q *Queue) RecoverStuck(ctx context.Context, olderThan time.Duration) (int64, error) {
	return q.store.RecoverStuckSheetSync(ctx, olderThan)
}

// Enqueue menambahkan Todo Task ke antrean sinkronisasi.
func (q *Queue) Enqueue(ctx context.Context, p EnqueueParams) (bool, error) {
	return q.store.EnqueueSheetSync(ctx, repository.EnqueueSheetSyncParams{
		WorkItemID:  p.WorkItemID,
		EventKey:    "sheet:" + p.WorkItemID.String(),
		RefNo:       p.RefNo,
		Op:          p.Op,
		Action:      p.Action,
		Payload:     p.Payload,
		MaxAttempts: q.cfg.OutboxMaxAttempts,
	})
}

// sheetBackoff mengembalikan jeda percobaan ulang: 1, 2, 5, 15, 60 menit.
func sheetBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 1 * time.Minute
	case 2:
		return 2 * time.Minute
	case 3:
		return 5 * time.Minute
	case 4:
		return 15 * time.Minute
	default:
		return 60 * time.Minute
	}
}

// LogSummary mencatat ringkasan dispatch bila ada aktivitas.
func LogSummary(res DispatchResult) {
	if res.Claimed > 0 || res.Failed > 0 {
		log.Printf("worker[sheet_sync]: claimed=%d sent=%d retried=%d failed=%d",
			res.Claimed, res.Sent, res.Retried, res.Failed)
	}
}
