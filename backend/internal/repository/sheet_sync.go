package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Google Sheets sync (F18) — konfigurasi & antrean.
   --------------------------------------------------------------------------- */

// GetSheetSyncConfig mengambil baris konfigurasi tunggal.
func (s *Store) GetSheetSyncConfig(ctx context.Context) (*models.SheetSyncConfig, error) {
	c := &models.SheetSyncConfig{}
	var clientEmail string
	err := s.pool.QueryRow(ctx, `
		SELECT id, enabled, spreadsheet_id, sheet_name, service_account_enc,
		       header_written, last_sync_at, last_error, created_at, updated_at
		FROM sheet_sync_config
		ORDER BY created_at
		LIMIT 1`).Scan(
		&c.ID, &c.Enabled, &c.SpreadsheetID, &c.SheetName, &c.ServiceAccountEnc,
		&c.HeaderWritten, &c.LastSyncAt, &c.LastError, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	c.ServiceAccountSet = c.ServiceAccountEnc != ""
	_ = clientEmail // client_email diturunkan di lapisan sheets (bukan di sini).
	return c, nil
}

// UpdateSheetSyncConfigParams adalah parameter pembaruan konfigurasi.
type UpdateSheetSyncConfigParams struct {
	Enabled       bool
	SpreadsheetID string
	SheetName     string
	// ServiceAccountEnc nil = jangan ubah kredensial yang tersimpan.
	ServiceAccountEnc *string
	// ResetHeader false = biarkan header_written apa adanya.
	ResetHeader bool
	By          string
}

// UpdateSheetSyncConfig menyimpan konfigurasi sinkronisasi.
func (s *Store) UpdateSheetSyncConfig(ctx context.Context, p UpdateSheetSyncConfigParams) error {
	setCred := ""
	args := []any{p.Enabled, p.SpreadsheetID, p.SheetName}
	if p.ServiceAccountEnc != nil {
		setCred = ", service_account_enc = $4"
		args = append(args, *p.ServiceAccountEnc)
	}
	if p.ResetHeader {
		setCred += ", header_written = FALSE"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_config
		SET enabled=$1, spreadsheet_id=$2, sheet_name=$3`+setCred+`, updated_at=now()`,
		args...)
	return err
}

// MarkSheetHeaderWritten menandai header sudah ditulis.
func (s *Store) MarkSheetHeaderWritten(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sheet_sync_config SET header_written=TRUE, updated_at=now()`)
	return err
}

// RecordSheetSyncResult mencatat hasil sinkronisasi terakhir (untuk panel).
func (s *Store) RecordSheetSyncResult(ctx context.Context, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_config
		SET last_sync_at=now(), last_error=$1, updated_at=now()`, truncateForDB(errMsg, 2000))
	return err
}

/* ---------------------------------------------------------------------------
   Antrean
   --------------------------------------------------------------------------- */

// EnqueueSheetSyncParams adalah parameter penambahan/pembaruan entri antrean.
type EnqueueSheetSyncParams struct {
	WorkItemID  uuid.UUID
	EventKey    string
	RefNo       string
	Op          string
	Action      string
	Payload     map[string]any
	MaxAttempts int
}

// EnqueueSheetSync menambahkan entri antrean untuk sebuah work item.
//
// Idempoten: event_key UNIQUE membuat pemanggilan berulang tidak menggandakan.
// Bila entri sudah ada, payload di-refresh dengan snapshot terbaru dan entri
// dikembalikan ke 'pending' agar perubahan terakhir ikut tersinkron:
//   - bila sedang 'sending', status dibiarkan (worker sedang menulis).
//   - selain itu (pending/failed/sent) → 'pending' dengan jadwal sekarang.
//
// Setelah pernah sukses, op dipaksa 'update' agar baris yang sama diperbarui
// (bukan ditambah) di spreadsheet.
func (s *Store) EnqueueSheetSync(ctx context.Context, p EnqueueSheetSyncParams) (bool, error) {
	if p.Payload == nil {
		p.Payload = map[string]any{}
	}
	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	op := p.Op
	if op == "" {
		op = models.SheetOpAppend
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO sheet_sync_queue
			(work_item_id, event_key, ref_no, op, action, payload_json, max_attempts)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (event_key) DO UPDATE
			SET payload_json = EXCLUDED.payload_json,
			    action       = EXCLUDED.action,
			    ref_no       = EXCLUDED.ref_no,
			    -- op hanya naik append -> update; sekali pernah terkirim tetap update.
			    op           = CASE WHEN sheet_sync_queue.op = 'update' OR sheet_sync_queue.status = 'sent'
			                        THEN 'update' ELSE EXCLUDED.op END,
			    -- Jangan ganggu entri yang sedang dikirim; selain itu antre ulang.
			    status       = CASE WHEN sheet_sync_queue.status = 'sending'
			                        THEN 'sending' ELSE 'pending' END,
			    next_attempt_at = now(),
			    updated_at   = now()`,
		p.WorkItemID, p.EventKey, p.RefNo, op, p.Action, p.Payload, maxAttempts)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimSheetSync mengambil batch entri siap kirim dan menandainya 'sending'.
func (s *Store) ClaimSheetSync(ctx context.Context, batchSize int) ([]models.SheetSyncQueueRow, error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 50
	}
	rows, err := s.pool.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM sheet_sync_queue
			WHERE status IN ('pending','failed') AND next_attempt_at <= now()
			ORDER BY next_attempt_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE sheet_sync_queue q
		SET status='sending', updated_at=now()
		FROM claimed
		WHERE q.id = claimed.id
		RETURNING q.id, q.work_item_id, q.event_key, q.ref_no, q.op, q.action,
		          q.payload_json, q.status, q.attempts, q.max_attempts,
		          q.next_attempt_at, q.last_error, q.sent_at, q.created_at, q.updated_at`,
		batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SheetSyncQueueRow{}
	for rows.Next() {
		var r models.SheetSyncQueueRow
		if err := rows.Scan(&r.ID, &r.WorkItemID, &r.EventKey, &r.RefNo, &r.Op, &r.Action,
			&r.Payload, &r.Status, &r.Attempts, &r.MaxAttempts,
			&r.NextAttemptAt, &r.LastError, &r.SentAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkSheetSyncSent menandai entri berhasil tersinkron.
func (s *Store) MarkSheetSyncSent(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_queue
		SET status='sent', op='update', sent_at=now(), last_error='', updated_at=now()
		WHERE id=$1`, id)
	return err
}

// RescheduleSheetSync mengembalikan entri ke pending dengan jadwal berikutnya.
func (s *Store) RescheduleSheetSync(ctx context.Context, id int64, attempts int, next time.Time, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_queue
		SET status='pending', attempts=$2, next_attempt_at=$3, last_error=$4, updated_at=now()
		WHERE id=$1`, id, attempts, next.UTC(), truncateForDB(errMsg, 2000))
	return err
}

// MarkSheetSyncFailed menandai entri gagal permanen (melebihi max_attempts).
func (s *Store) MarkSheetSyncFailed(ctx context.Context, id int64, attempts int, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_queue
		SET status='failed', attempts=$2, last_error=$3, updated_at=now()
		WHERE id=$1`, id, attempts, truncateForDB(errMsg, 2000))
	return err
}

// RecoverStuckSheetSync mengembalikan entri 'sending' yang menggantung ke pending.
func (s *Store) RecoverStuckSheetSync(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_queue
		SET status='pending', next_attempt_at=now(), updated_at=now(),
		    last_error = CASE WHEN last_error='' THEN 'dipulihkan dari status sending yang menggantung' ELSE last_error END
		WHERE status='sending' AND updated_at < $1`, time.Now().UTC().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SheetSyncQueueCounts menghitung entri per status (untuk panel).
func (s *Store) SheetSyncQueueCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM sheet_sync_queue GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// ResetSheetSyncForRetry mengembalikan entri gagal ke antrean (aksi operator).
func (s *Store) ResetSheetSyncForRetry(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sheet_sync_queue
		SET status='pending', attempts=0, next_attempt_at=now(), last_error='', updated_at=now()
		WHERE id=$1 AND status IN ('failed','pending')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTaskCompletionNote menyimpan keterangan penyelesaian Daily Task.
//
// Dipakai saat operator menandai Daily Task "Selesai"; keterangan ini ikut
// disinkronkan ke spreadsheet.
func (s *Store) SetTaskCompletionNote(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, note string) error {
	// UPSERT: Daily Task lama (dibuat sebelum task_details dibuat untuk
	// daily_task) mungkin belum punya baris extension.
	_, err := tx.Exec(ctx, `
		INSERT INTO task_details (work_item_id, checklist_json, progress_pct, completion_note)
		VALUES ($1, '[]'::jsonb, 0, $2)
		ON CONFLICT (work_item_id) DO UPDATE SET completion_note = EXCLUDED.completion_note`,
		workItemID, note)
	return err
}

// SetTaskCompletionResult menyimpan hasil penyelesaian Daily Task (F24):
// keterangan + result_status (normal/bermasalah). UPSERT karena Daily Task lama
// mungkin belum punya baris extension.
func (s *Store) SetTaskCompletionResult(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, note, resultStatus string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO task_details (work_item_id, checklist_json, progress_pct, completion_note, result_status)
		VALUES ($1, '[]'::jsonb, 0, $2, $3)
		ON CONFLICT (work_item_id) DO UPDATE
		   SET completion_note = EXCLUDED.completion_note, result_status = EXCLUDED.result_status`,
		workItemID, note, resultStatus)
	return err
}
