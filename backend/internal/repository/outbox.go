package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Outbox — penulisan
   --------------------------------------------------------------------------- */

// InsertOutboxParams adalah parameter penambahan baris outbox.
type InsertOutboxParams struct {
	WorkItemID  *uuid.UUID
	EventKey    string
	SourceType  string
	TemplateKey string
	OffsetLabel string
	Severity    string
	Channel     string
	Destination string
	ProviderID  *uuid.UUID
	MaxAttempts int
	// Payload adalah data yang dipakai merender template (disimpan sebagai JSONB).
	Payload map[string]any
}

// InsertOutbox menambahkan satu baris outbox.
//
// Mengembalikan true bila baris benar-benar ditambahkan, false bila event_key
// sudah ada. Sifat ini yang membuat enqueue idempoten: worker boleh dipanggil
// berulang tanpa menggandakan notifikasi.
func (s *Store) InsertOutbox(ctx context.Context, p InsertOutboxParams) (bool, error) {
	attempts := p.MaxAttempts
	if attempts <= 0 {
		attempts = 5
	}
	severity := p.Severity
	if severity == "" {
		severity = models.SeverityInfo
	}

	payload := p.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO notification_outbox
			(work_item_id, event_key, source_type, template_key, offset_label, severity,
			 channel, destination, provider_id, max_attempts, payload_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (event_key) DO NOTHING`,
		p.WorkItemID, p.EventKey, p.SourceType, p.TemplateKey, p.OffsetLabel, severity,
		p.Channel, p.Destination, p.ProviderID, attempts, payload)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimOutbox mengambil batch baris pending dan menandainya 'sending'.
//
// FOR UPDATE SKIP LOCKED memungkinkan beberapa worker berjalan bersamaan
// tanpa mengambil baris yang sama.
func (s *Store) ClaimOutbox(ctx context.Context, batchSize int) ([]models.NotificationOutbox, error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 50
	}

	rows, err := s.pool.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM notification_outbox
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_outbox o
		SET status = 'sending', updated_at = now()
		FROM claimed
		WHERE o.id = claimed.id
		RETURNING o.id, o.work_item_id, o.event_key, o.source_type, o.template_key,
		          o.offset_label, o.severity, o.channel, o.destination, o.provider_id,
		          o.payload_json, o.rendered_subject, o.rendered_body, o.status,
		          o.attempts, o.max_attempts, o.next_attempt_at, o.last_error,
		          o.provider_message_id, o.sent_at, o.created_at, o.updated_at`,
		batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NotificationOutbox{}
	for rows.Next() {
		var o models.NotificationOutbox
		if err := rows.Scan(&o.ID, &o.WorkItemID, &o.EventKey, &o.SourceType, &o.TemplateKey,
			&o.OffsetLabel, &o.Severity, &o.Channel, &o.Destination, &o.ProviderID,
			&o.Payload, &o.RenderedSubject, &o.RenderedBody, &o.Status,
			&o.Attempts, &o.MaxAttempts, &o.NextAttemptAt, &o.LastError,
			&o.ProviderMessageID, &o.SentAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SaveOutboxRender menyimpan subject/body hasil render (untuk audit).
func (s *Store) SaveOutboxRender(ctx context.Context, id int64, subject, body string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notification_outbox SET rendered_subject=$2, rendered_body=$3 WHERE id=$1`,
		id, subject, body)
	return err
}

// MarkOutboxSent menandai pengiriman berhasil.
func (s *Store) MarkOutboxSent(ctx context.Context, id int64, providerMessageID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status='sent', sent_at=$2, provider_message_id=$3, last_error='', updated_at=now()
		WHERE id=$1`, id, time.Now().UTC(), providerMessageID)
	return err
}

// MarkOutboxFailed menandai pengiriman gagal permanen.
func (s *Store) MarkOutboxFailed(ctx context.Context, id int64, attempts int, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status='failed', attempts=$2, last_error=$3, updated_at=now()
		WHERE id=$1`, id, attempts, truncateForDB(errMsg, 2000))
	return err
}

// RescheduleOutbox mengembalikan baris ke pending dengan jadwal berikutnya.
func (s *Store) RescheduleOutbox(ctx context.Context, id int64, attempts int, next time.Time, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status='pending', attempts=$2, next_attempt_at=$3, last_error=$4, updated_at=now()
		WHERE id=$1`, id, attempts, next.UTC(), truncateForDB(errMsg, 2000))
	return err
}

// AppendEventSimple mencatat event tanpa transaksi (dipakai jalur notifikasi).
func (s *Store) AppendEventSimple(ctx context.Context, workItemID uuid.UUID, eventType string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO work_item_events (work_item_id, event_type, actor_username, detail_json)
		VALUES ($1,$2,'system',$3)`, workItemID, eventType, detail)
	return err
}

// ResetOutboxForRetry mengembalikan baris gagal ke antrean (aksi operator).
func (s *Store) ResetOutboxForRetry(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notification_outbox
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

// RecoverStuckOutbox mengembalikan baris 'sending' yang menggantung (mis. worker
// mati saat mengirim) ke 'pending' agar dicoba lagi.
func (s *Store) RecoverStuckOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status='pending', next_attempt_at=now(), updated_at=now(),
		    last_error = CASE WHEN last_error='' THEN 'dipulihkan dari status sending yang menggantung' ELSE last_error END
		WHERE status='sending' AND updated_at < $1`, time.Now().UTC().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// WorkItemsDueOffsets mengambil work item yang perlu dievaluasi offset-nya.
//
// Kriteria: belum selesai/dibatalkan dan punya expire_at.
func (s *Store) WorkItemsDueOffsets(ctx context.Context, itemType string, limit int) ([]models.WorkItem, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+workItemColumns+` FROM work_items
		WHERE NOT is_deleted
		  AND item_type = $1
		  AND expire_at IS NOT NULL
		  AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled')
		ORDER BY expire_at
		LIMIT $2`, itemType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanWorkItems(rows)
}

// GetReminderDetails mengambil detail reminder (dipakai fanout).
func (s *Store) GetReminderDetails(ctx context.Context, workItemID uuid.UUID) (*models.ReminderDetails, error) {
	d := &models.ReminderDetails{WorkItemID: workItemID}
	err := s.pool.QueryRow(ctx, `
		SELECT category, subject_name, subject_type, recurrence_rule,
		       escalation_policy_id, activated_at, last_offset_fired
		FROM reminder_details WHERE work_item_id=$1`, workItemID,
	).Scan(&d.Category, &d.SubjectName, &d.SubjectType, &d.RecurrenceRule,
		&d.EscalationPolicyID, &d.ActivatedAt, &d.LastOffsetFired)
	if err != nil {
		return nil, mapErr(err)
	}
	return d, nil
}

// GetRFSDetails mengambil detail RFS (dipakai fanout).
func (s *Store) GetRFSDetails(ctx context.Context, workItemID uuid.UUID) (*models.RFSDetails, error) {
	d := &models.RFSDetails{WorkItemID: workItemID}
	err := s.pool.QueryRow(ctx, `
		SELECT customer_name, service_id, service_package, bandwidth,
		       pic_noc, pic_sales, sales_username, site, install_stage
		FROM rfs_details WHERE work_item_id=$1`, workItemID,
	).Scan(&d.CustomerName, &d.ServiceID, &d.ServicePackage, &d.Bandwidth,
		&d.PicNOC, &d.PicSales, &d.SalesUsername, &d.Site, &d.InstallStage)
	if err != nil {
		return nil, mapErr(err)
	}
	return d, nil
}

// GetTargetName mengambil nama target untuk keperluan pesan/log.
func (s *Store) GetTargetName(ctx context.Context, id uuid.UUID) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM notification_targets WHERE id=$1`, id).Scan(&name)
	if err != nil {
		return "", mapErr(err)
	}
	return name, nil
}

func truncateForDB(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// SetLastReminderOffset mencatat label offset terakhir yang sudah diantrikan.
//
// Dipakai fanout agar offset yang sama tidak dikirim dua kali untuk satu item.
func (s *Store) SetLastReminderOffset(ctx context.Context, workItemID uuid.UUID, label string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reminder_details SET last_offset_fired=$2 WHERE work_item_id=$1`, workItemID, label)
	return err
}

// ResetReminderLastOffset mengosongkan penanda offset terakhir sebuah reminder.
//
// Dipakai tombol "Kirim ulang / reset offset" agar fanout dapat menjadwalkan
// ulang offset yang sudah lewat pada putaran berikutnya.
func (s *Store) ResetReminderLastOffset(ctx context.Context, workItemID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reminder_details SET last_offset_fired='' WHERE work_item_id=$1`, workItemID)
	return err
}

// SetWorkItemStatusSystem mengubah status work item tanpa validasi workflow.
//
// Hanya dipakai jalur sistem (fanout) yang transisinya sudah ditentukan
// engine; perubahan dari pengguna tetap melalui API yang memvalidasi workflow.
func (s *Store) SetWorkItemStatusSystem(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE work_items SET status=$2, updated_at=now() WHERE id=$1 AND NOT is_deleted`,
		id, status)
	return err
}

// UpcomingForDigest mengambil work item yang perlu masuk ringkasan harian.
func (s *Store) UpcomingForDigest(ctx context.Context) ([]models.WorkItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+workItemColumns+` FROM work_items
		WHERE NOT is_deleted
		  AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled')
		  AND (
		    (due_at IS NOT NULL AND due_at <= now() + interval '24 hours')
		    OR (expire_at IS NOT NULL AND expire_at <= now() + interval '24 hours')
		  )
		ORDER BY LEAST(COALESCE(due_at,'infinity'::timestamptz), COALESCE(expire_at,'infinity'::timestamptz))
		LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

// CountByTypeAndStatusRingkas menghitung item terbuka per tipe (untuk digest).
func (s *Store) CountByTypeAndStatusRingkas(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_type, count(*) FROM work_items
		WHERE NOT is_deleted
		  AND status NOT IN ('done','cancelled','closed','activated','expired','fulfilled')
		GROUP BY item_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, rows.Err()
}

// DefaultTargetID mengembalikan id target default untuk pengiriman sistem
// (mis. digest harian).
func (s *Store) DefaultTargetID(ctx context.Context) (*uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM notification_targets
		WHERE is_active
		ORDER BY (name = 'NOC-Team') DESC, created_at
		LIMIT 1`).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	return &id, nil
}
