package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/providers"
	"ingatin/backend/internal/repository"
)

// ErrNoBinding dikembalikan bila target tidak punya binding aktif.
var ErrNoBinding = errors.New("target notifikasi tidak memiliki binding aktif")

// Outbox mengelola antrean pengiriman notifikasi.
//
// Alur:
//  1. Enqueue dipanggil saat sebuah event terjadi (todo dibuat, offset jatuh tempo).
//     Ia menulis SATU baris per (event × target × channel) dengan event_key unik.
//  2. Dispatch dipanggil berkala oleh worker: mengambil batch pending, merender
//     template, mengirim, lalu memperbarui status + backoff.
//
// Sifat penting:
//   - Idempotent: event_key UNIQUE membuat enqueue berulang tidak menggandakan pesan.
//   - Aman restart: status & attempts tersimpan di database, bukan di memori.
//   - Paralel aman: klaim baris memakai FOR UPDATE SKIP LOCKED.
type Outbox struct {
	cfg      *config.Config
	store    *repository.Store
	resolver *Resolver
}

// NewOutbox membuat Outbox.
func NewOutbox(cfg *config.Config, store *repository.Store, resolver *Resolver) *Outbox {
	return &Outbox{cfg: cfg, store: store, resolver: resolver}
}

// EnqueueParams adalah parameter penambahan item outbox.
type EnqueueParams struct {
	EventKey    string
	WorkItemID  *uuid.UUID
	SourceType  string
	TemplateKey string
	OffsetLabel string
	Severity    string
	TargetID    uuid.UUID
	Payload     Payload
}

// EnqueueResult melaporkan berapa pesan yang benar-benar ditambahkan.
type EnqueueResult struct {
	Added     int
	Skipped   int // sudah ada (event_key duplikat)
	NoBinding bool
}

// Enqueue menambahkan notifikasi untuk SELURUH binding aktif pada target.
//
// Setiap channel mendapat event_key sendiri sehingga kegagalan WhatsApp tidak
// memengaruhi pengiriman Telegram.
func (o *Outbox) Enqueue(ctx context.Context, p EnqueueParams) (EnqueueResult, error) {
	var res EnqueueResult

	bindings, err := o.store.ActiveBindingsForTarget(ctx, p.TargetID)
	if err != nil {
		return res, err
	}
	if len(bindings) == 0 {
		res.NoBinding = true
		return res, fmt.Errorf("%w (target %s, event %s)", ErrNoBinding, p.TargetID, p.EventKey)
	}

	for _, b := range bindings {
		key := fmt.Sprintf("%s:%s:%s", p.EventKey, b.Channel, b.ID)
		inserted, err := o.store.InsertOutbox(ctx, repository.InsertOutboxParams{
			WorkItemID:  p.WorkItemID,
			EventKey:    key,
			SourceType:  normalizeSourceType(p.SourceType),
			TemplateKey: p.TemplateKey,
			OffsetLabel: p.OffsetLabel,
			Severity:    p.Severity,
			Channel:     b.Channel,
			Destination: b.Destination,
			ProviderID:  b.ProviderID,
			MaxAttempts: o.cfg.OutboxMaxAttempts,
			Payload:     payloadToMap(p.Payload),
		})
		if err != nil {
			return res, err
		}
		if inserted {
			res.Added++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

// DispatchResult merangkum hasil satu putaran pengiriman.
type DispatchResult struct {
	Claimed int
	Sent    int
	Failed  int
	Retried int
}

// Dispatch mengambil dan mengirim batch outbox yang menunggu.
func (o *Outbox) Dispatch(ctx context.Context) (DispatchResult, error) {
	var res DispatchResult

	rows, err := o.store.ClaimOutbox(ctx, o.cfg.OutboxBatchSize)
	if err != nil {
		return res, err
	}
	res.Claimed = len(rows)

	for _, row := range rows {
		if err := o.deliver(ctx, row); err != nil {
			log.Printf("notify: kirim outbox %d (%s) gagal: %v", row.ID, row.EventKey, err)
		}
		// Status akhir sudah ditulis oleh deliver; hitung untuk ringkasan.
		switch {
		case row.Status == models.OutboxSent:
			res.Sent++
		case row.Status == models.OutboxFailed:
			res.Failed++
		default:
			res.Retried++
		}
	}
	return res, nil
}

// deliver merender dan mengirim satu baris outbox, lalu memperbarui statusnya.
func (o *Outbox) deliver(ctx context.Context, row models.NotificationOutbox) error {
	// 1. Ambil template (dengan fallback agar notifikasi tetap terkirim).
	tplRec, err := o.store.GetTemplate(ctx, row.TemplateKey, row.Channel)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return o.markFailed(ctx, row, err, false)
		}
		tplRec = FallbackTemplate(row.TemplateKey)
	}

	parsed, err := ParseTemplate(tplRec)
	if err != nil {
		// Template rusak = kesalahan permanen; tidak ada gunanya retry.
		return o.markFailed(ctx, row, fmt.Errorf("template tidak valid: %w", err), false)
	}

	payload := payloadFromJSON(row.Payload)
	subject, body, err := parsed.Render(payload)
	if err != nil {
		return o.markFailed(ctx, row, fmt.Errorf("render template: %w", err), false)
	}

	// 2. Siapkan provider untuk kanal ini.
	provider, _, err := o.resolver.ProviderForChannel(ctx, row.Channel, row.ProviderID)
	if err != nil {
		// Provider belum dikonfigurasi bukan kesalahan sementara yang akan
		// sembuh sendiri, tetapi bisa diperbaiki operator kapan saja; karena
		// itu tetap dicoba ulang beberapa kali sebelum menyerah.
		return o.markFailed(ctx, row, err, true)
	}

	// 3. Simpan hasil render untuk audit sebelum mengirim, agar operator dapat
	//    melihat persis apa yang dikirim walau pengiriman gagal.
	if err := o.store.SaveOutboxRender(ctx, row.ID, subject, body); err != nil {
		return err
	}

	// 4. Kirim.
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, sendErr := provider.Send(sendCtx, providers.Message{
		Destination: row.Destination,
		Subject:     subject,
		Body:        body,
		Severity:    row.Severity,
		Metadata: map[string]string{
			"event_key": row.EventKey,
			"source":    row.SourceType,
			"offset":    row.OffsetLabel,
		},
	})

	if sendErr != nil {
		// Hormati penanda retryable dari provider: kalau provider bilang
		// kegagalan permanen (mis. nomor tidak valid), jangan buang percobaan.
		retryable := result.Retryable
		return o.markFailed(ctx, row, sendErr, retryable)
	}

	if err := o.store.MarkOutboxSent(ctx, row.ID, result.ProviderMessageID); err != nil {
		return err
	}
	return nil
}

// markFailed mencatat kegagalan dan menjadwalkan ulang bila masih layak coba.
func (o *Outbox) markFailed(ctx context.Context, row models.NotificationOutbox, cause error, retryable bool) error {
	attempts := row.Attempts + 1
	maxAttempts := row.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = o.cfg.OutboxMaxAttempts
	}

	// Bila tidak retryable, langsung tandai gagal permanen.
	if !retryable {
		if err := o.store.MarkOutboxFailed(ctx, row.ID, attempts, cause.Error()); err != nil {
			return err
		}
		o.logEvent(ctx, row, cause)
		return nil
	}

	if attempts >= maxAttempts {
		if err := o.store.MarkOutboxFailed(ctx, row.ID, attempts, cause.Error()); err != nil {
			return err
		}
		o.logEvent(ctx, row, cause)
		return nil
	}

	next := time.Now().UTC().Add(backoffFor(attempts))
	if err := o.store.RescheduleOutbox(ctx, row.ID, attempts, next, cause.Error()); err != nil {
		return err
	}
	return nil
}

// logEvent mencatat kegagalan pengiriman ke timeline work item.
func (o *Outbox) logEvent(ctx context.Context, row models.NotificationOutbox, cause error) {
	if row.WorkItemID == nil {
		return
	}
	detail := map[string]any{
		"channel": row.Channel,
		"error":   cause.Error(),
	}
	if err := o.store.AppendEventSimple(ctx, *row.WorkItemID, "notify_failed", detail); err != nil {
		log.Printf("notify: gagal mencatat event kegagalan outbox %d: %v", row.ID, err)
	}
}

// backoffFor mengembalikan jeda percobaan ulang: 1, 2, 5, 15, 60 menit.
//
// Jeda tumbuh agar penyedia yang sedang bermasalah tidak dibanjiri, tetapi
// tidak terlalu panjang agar notifikasi tetap sampai dalam hitungan menit.
func backoffFor(attempt int) time.Duration {
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

// normalizeSourceType memastikan nilai sesuai CHECK constraint.
func normalizeSourceType(s string) string {
	switch s {
	case "work_item", "todo", "reminder", "rfs", "ticket", "manual", "test", "system":
		return s
	default:
		return "system"
	}
}

// payloadToMap mengubah struct Payload menjadi map untuk disimpan sebagai JSONB.
//
// Field kosong tetap disertakan agar template tidak perlu menangani key hilang
// (helper {{dash}} sudah menangani string kosong).
func payloadToMap(p Payload) map[string]any {
	return map[string]any{
		"ref_no":          p.RefNo,
		"title":           p.Title,
		"item_type":       p.ItemType,
		"priority":        p.Priority,
		"status":          p.Status,
		"stage":           p.Stage,
		"owner":           p.Owner,
		"requester":       p.Requester,
		"team":            p.Team,
		"created_by":      p.CreatedBy,
		"due_at":          p.DueAt,
		"expire_at":       p.ExpireAt,
		"start_at":        p.StartAt,
		"remaining":       p.Remaining,
		"created_at":      p.CreatedAt,
		"offset_label":    p.OffsetLabel,
		"severity":        p.Severity,
		"description":     p.Description,
		"notes":           p.Notes,
		"customer_name":   p.CustomerName,
		"service_id":      p.ServiceID,
		"service_package": p.ServicePackage,
		"bandwidth":       p.Bandwidth,
		"pic_noc":         p.PicNOC,
		"pic_sales":       p.PicSales,
		"site":            p.Site,
		"device_ref":      p.DeviceRef,
		"service_ref":     p.ServiceRef,
		"customer_ref":    p.CustomerRef,
		"subject_name":    p.SubjectName,
		"category":        p.Category,
		"target_name":     p.TargetName,
		"channel":         p.Channel,
	}
}

// payloadFromJSON mengubah payload JSONB menjadi struct Payload.
//
// Field yang tidak ada dibiarkan kosong sehingga template tetap dapat dirender.
func payloadFromJSON(m map[string]any) Payload {
	p := Payload{}
	if m == nil {
		return p
	}
	get := func(k string) string {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			if v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}

	p.RefNo = get("ref_no")
	p.Title = get("title")
	p.ItemType = get("item_type")
	p.Priority = get("priority")
	p.Status = get("status")
	p.Stage = get("stage")
	p.Owner = get("owner")
	p.Requester = get("requester")
	p.Team = get("team")
	p.CreatedBy = get("created_by")
	p.DueAt = get("due_at")
	p.ExpireAt = get("expire_at")
	p.StartAt = get("start_at")
	p.Remaining = get("remaining")
	p.CreatedAt = get("created_at")
	p.OffsetLabel = get("offset_label")
	p.Severity = get("severity")
	p.Description = get("description")
	p.Notes = get("notes")
	p.CustomerName = get("customer_name")
	p.ServiceID = get("service_id")
	p.ServicePackage = get("service_package")
	p.Bandwidth = get("bandwidth")
	p.PicNOC = get("pic_noc")
	p.PicSales = get("pic_sales")
	p.Site = get("site")
	p.DeviceRef = get("device_ref")
	p.ServiceRef = get("service_ref")
	p.CustomerRef = get("customer_ref")
	p.SubjectName = get("subject_name")
	p.Category = get("category")
	p.TargetName = get("target_name")
	p.Channel = get("channel")

	return p
}

// enqueueAndLog adalah helper untuk mencatat hasil enqueue ke log server.
func (o *Outbox) enqueueAndLog(ctx context.Context, p EnqueueParams) {
	res, err := o.Enqueue(ctx, p)
	if err != nil {
		log.Printf("notify: enqueue %s gagal: %v", p.EventKey, err)
		return
	}
	if res.Added > 0 {
		log.Printf("notify: %s — %d pesan diantrikan", p.EventKey, res.Added)
	}
}

// pgxTxGuard memastikan pgx tetap terimpor bila file ini diubah.
var _ = pgx.ErrNoRows
