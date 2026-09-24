package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// Fanout mematerialisasi offset peringatan yang jatuh tempo menjadi pesan
// di outbox.
//
// Prinsip: engine ini TIDAK mengirim apa pun. Ia hanya menghitung "apakah
// offset X untuk item Y sudah waktunya?" lalu menyerahkan ke outbox. Dengan
// pemisahan ini, restart worker tidak pernah menghilangkan atau menggandakan
// peringatan (event_key unik).
type Fanout struct {
	store  *repository.Store
	outbox *Outbox
}

// NewFanout membuat Fanout.
func NewFanout(store *repository.Store, outbox *Outbox) *Fanout {
	return &Fanout{store: store, outbox: outbox}
}

// Run mengevaluasi seluruh item aktif dari tipe reminder & rfs.
func (f *Fanout) Run(ctx context.Context) error {
	var errs []error

	if err := f.runReminders(ctx); err != nil {
		errs = append(errs, fmt.Errorf("reminder: %w", err))
	}
	if err := f.runRFS(ctx); err != nil {
		errs = append(errs, fmt.Errorf("rfs: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// runReminders mengevaluasi offset untuk item_type=reminder.
func (f *Fanout) runReminders(ctx context.Context) error {
	items, err := f.store.WorkItemsDueOffsets(ctx, models.ItemReminder, 500)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, item := range items {
		if item.ExpireAt == nil {
			continue
		}
		// F25: resolusi target: item → tim → default.
		targetID, terr := f.store.ResolveItemTargetID(ctx, &item)
		if terr != nil || targetID == nil {
			continue
		}

		details, err := f.store.GetReminderDetails(ctx, item.ID)
		if err != nil {
			log.Printf("fanout: detail reminder %s: %v", item.RefNo, err)
			continue
		}
		if details.ActivatedAt != nil {
			// Sudah diaktivasi: tidak perlu diingatkan lagi.
			continue
		}

		policy, err := f.reminderPolicy(ctx, details)
		if err != nil {
			log.Printf("fanout: policy reminder %s: %v", item.RefNo, err)
			continue
		}

		for _, offset := range policy.Offsets {
			dueAt, label := offsetTime(*item.ExpireAt, offset)
			if label == "" {
				continue
			}
			// Belum waktunya.
			if now.Before(dueAt) {
				continue
			}
			// Jangan kirim offset yang sudah lewat sangat lama (mis. item lama
			// yang baru diimpor) kecuali offset LATE.
			if offset.HoursBefore != nil && now.Sub(dueAt) > 24*time.Hour {
				continue
			}
			// Untuk offset LATE, kirim hanya dalam jendela 7 hari.
			if offset.HoursAfter != nil && now.Sub(dueAt) > 7*24*time.Hour {
				continue
			}
			// Hindari mengirim offset yang sama dua kali untuk item yang sama.
			if details.LastOffsetFired == label {
				continue
			}
			// Jangan lompati urutan: offset LATE hanya setelah semua H-* lewat.
			if offset.HoursAfter != nil {
				if !allBeforeOffsetsPassed(*item.ExpireAt, policy.Offsets, now) {
					continue
				}
			}

			tplKey, severity := reminderTemplateFor(offset)
			payload := f.basePayload(item, offset.Label, severity)
			payload.SubjectName = details.SubjectName
			payload.Category = details.Category
			payload.Remaining = HumanRemaining(*item.ExpireAt, now)

			res, err := f.outbox.Enqueue(ctx, EnqueueParams{
				EventKey:    fmt.Sprintf("reminder:%s:%s", item.ID, label),
				WorkItemID:  &item.ID,
				SourceType:  "reminder",
				TemplateKey: tplKey,
				OffsetLabel: label,
				Severity:    severity,
				TargetID:    *targetID,
				Payload:     payload,
			})
			if err != nil {
				log.Printf("fanout: enqueue reminder %s offset %s: %v", item.RefNo, label, err)
				continue
			}
			if res.Added > 0 {
				// Catat offset terakhir agar tidak diulang pada putaran berikutnya.
				if err := f.store.SetLastReminderOffset(ctx, item.ID, label); err != nil {
					log.Printf("fanout: simpan offset reminder %s: %v", item.RefNo, err)
				}
				// Transisi status: active -> expiring -> expired.
				f.advanceReminderStatus(ctx, item, now)
			}
		}
	}
	return nil
}

// reminderPolicy mengambil policy eskalasi reminder, dengan fallback ke
// TRIAL-3D bila tidak ada yang ditentukan.
func (f *Fanout) reminderPolicy(ctx context.Context, details *models.ReminderDetails) (*models.EscalationPolicy, error) {
	if details.EscalationPolicyID != nil {
		return f.store.GetPolicy(ctx, *details.EscalationPolicyID)
	}

	// Fallback berdasarkan kategori.
	name := "TRIAL-3D"
	if details.Category == models.ReminderCategoryRFS {
		name = "RFS-DEFAULT"
	}
	policy, err := f.store.GetPolicyByName(ctx, name)
	if err == nil {
		return policy, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// Fallback terakhir: policy default apa pun, atau offset bawaan.
	def, err := f.store.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range def {
		if p.IsDefault && p.IsActive {
			pp := p
			return &pp, nil
		}
	}

	// Tidak ada policy sama sekali: pakai offset aman bawaan.
	return &models.EscalationPolicy{
		Name: "FALLBACK-24H",
		Offsets: []models.EscalationOffset{
			{Label: "H-1", Severity: models.SeverityWarning, HoursBefore: intPtr(24)},
			{Label: "H-0", Severity: models.SeverityCritical, HoursBefore: intPtr(0)},
		},
		MaxAttempts: 5,
	}, nil
}

// runRFS mengevaluasi offset untuk item_type=rfs.
func (f *Fanout) runRFS(ctx context.Context) error {
	items, err := f.store.WorkItemsDueOffsets(ctx, models.ItemRFS, 500)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	policy, err := f.store.GetPolicyByName(ctx, "RFS-DEFAULT")
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		policy = &models.EscalationPolicy{
			Name: "RFS-FALLBACK",
			Offsets: []models.EscalationOffset{
				{Label: "H-3", Severity: models.SeverityWarning, HoursBefore: intPtr(72)},
				{Label: "H-1", Severity: models.SeverityWarning, HoursBefore: intPtr(24)},
				{Label: "H-0", Severity: models.SeverityCritical, HoursBefore: intPtr(0)},
			},
		}
	}

	for _, item := range items {
		if item.ExpireAt == nil {
			continue
		}
		// F25: resolusi target: item → tim PIC RFS → default.
		targetID, terr := f.store.ResolveItemTargetID(ctx, &item)
		if terr != nil || targetID == nil {
			continue
		}

		details, err := f.store.GetRFSDetails(ctx, item.ID)
		if err != nil {
			log.Printf("fanout: detail rfs %s: %v", item.RefNo, err)
			continue
		}

		for _, offset := range policy.Offsets {
			dueAt, label := offsetTime(*item.ExpireAt, offset)
			if label == "" || now.Before(dueAt) {
				continue
			}
			if offset.HoursBefore != nil && now.Sub(dueAt) > 24*time.Hour {
				continue
			}
			if offset.HoursAfter != nil && now.Sub(dueAt) > 7*24*time.Hour {
				continue
			}

			tplKey, severity := rfsTemplateFor(offset)
			payload := f.basePayload(item, offset.Label, severity)
			payload.CustomerName = details.CustomerName
			payload.ServicePackage = details.ServicePackage
			payload.Bandwidth = details.Bandwidth
			payload.PicNOC = details.PicNOC
			payload.PicSales = details.PicSales
			payload.Site = details.Site
			payload.Remaining = HumanRemaining(*item.ExpireAt, now)

			res, err := f.outbox.Enqueue(ctx, EnqueueParams{
				EventKey:    fmt.Sprintf("rfs:%s:%s", item.ID, label),
				WorkItemID:  &item.ID,
				SourceType:  "rfs",
				TemplateKey: tplKey,
				OffsetLabel: label,
				Severity:    severity,
				TargetID:    *targetID,
				Payload:     payload,
			})
			if err != nil {
				log.Printf("fanout: enqueue rfs %s offset %s: %v", item.RefNo, label, err)
				continue
			}
			if res.Added > 0 {
				log.Printf("fanout: %s — RFS %s diantrikan", item.RefNo, label)
			}
		}
	}
	return nil
}

// advanceReminderStatus memindahkan status reminder sesuai waktu.
func (f *Fanout) advanceReminderStatus(ctx context.Context, item models.WorkItem, now time.Time) {
	if item.ExpireAt == nil {
		return
	}
	target := ""
	switch {
	case now.After(*item.ExpireAt):
		target = "expired"
	case now.Add(24 * time.Hour).After(*item.ExpireAt):
		target = "expiring"
	default:
		return
	}

	if item.Status == target {
		return
	}
	// Hanya transisi yang sah menurut workflow.
	wf := workflowStates[models.ItemReminder]
	if !wf.valid(target) {
		return
	}
	if err := f.store.SetWorkItemStatusSystem(ctx, item.ID, target); err != nil {
		log.Printf("fanout: ubah status reminder %s -> %s: %v", item.RefNo, target, err)
		return
	}
	if err := f.store.AppendEventSimple(ctx, item.ID, "status_changed", map[string]any{
		"from": item.Status, "to": target, "by": "fanout",
	}); err != nil {
		log.Printf("fanout: catat event status reminder %s: %v", item.RefNo, err)
	}
}

// basePayload membangun payload umum dari work item.
func (f *Fanout) basePayload(item models.WorkItem, offsetLabel, severity string) Payload {
	return Payload{
		RefNo:       item.RefNo,
		Title:       item.Title,
		ItemType:    item.ItemType,
		Priority:    item.Priority,
		Status:      item.Status,
		Stage:       item.Stage,
		Owner:       item.OwnerUsername,
		Requester:   item.RequesterUsername,
		CreatedBy:   item.CreatedBy,
		DueAt:       FormatWIB(item.DueAt),
		ExpireAt:    FormatWIB(item.ExpireAt),
		StartAt:     FormatWIB(item.StartAt),
		CreatedAt:   FormatWIB(&item.CreatedAt),
		OffsetLabel: offsetLabel,
		Severity:    severity,
		Description: item.Description,
		DeviceRef:   item.DeviceRef,
		ServiceRef:  item.ServiceRef,
		CustomerRef: item.CustomerRef,
	}
}

/* ---------------------------------------------------------------------------
   Helper offset & template
   --------------------------------------------------------------------------- */

// offsetTime menghitung waktu absolut sebuah offset dan mengembalikan labelnya.
func offsetTime(expireAt time.Time, o models.EscalationOffset) (time.Time, string) {
	label := o.Label
	if label == "" {
		return time.Time{}, ""
	}
	if o.HoursBefore != nil {
		return expireAt.Add(-time.Duration(*o.HoursBefore) * time.Hour), label
	}
	if o.HoursAfter != nil {
		return expireAt.Add(time.Duration(*o.HoursAfter) * time.Hour), label
	}
	return time.Time{}, ""
}

// reminderTemplateFor memilih kunci template dan severity untuk sebuah offset.
func reminderTemplateFor(o models.EscalationOffset) (string, string) {
	severity := o.Severity
	if severity == "" {
		severity = models.SeverityWarning
	}

	// Offset LATE (setelah expire) memakai template eskalasi.
	if o.HoursAfter != nil {
		return TemplateReminderLate, models.SeverityCritical
	}
	// H-0 (tepat saat expire) memakai template hari-H.
	if o.HoursBefore != nil && *o.HoursBefore == 0 {
		return TemplateReminderDueToday, models.SeverityCritical
	}
	return TemplateReminderOffset, severity
}

// rfsTemplateFor memilih kunci template dan severity untuk offset RFS.
func rfsTemplateFor(o models.EscalationOffset) (string, string) {
	severity := o.Severity
	if severity == "" {
		severity = models.SeverityWarning
	}
	if o.HoursAfter != nil {
		return TemplateRFSLate, models.SeverityCritical
	}
	if o.HoursBefore != nil && *o.HoursBefore == 0 {
		return TemplateRFSToday, models.SeverityCritical
	}
	return TemplateRFSUpcoming, severity
}

// allBeforeOffsetsPassed melaporkan apakah semua offset H-* sudah lewat.
//
// Dipakai agar eskalasi LATE tidak dikirim sebelum peringatan H-0.
func allBeforeOffsetsPassed(expireAt time.Time, offsets []models.EscalationOffset, now time.Time) bool {
	for _, o := range offsets {
		if o.HoursBefore == nil {
			continue
		}
		due := expireAt.Add(-time.Duration(*o.HoursBefore) * time.Hour)
		if now.Before(due) {
			return false
		}
	}
	return true
}

func intPtr(v int) *int { return &v }

// workflowStates adalah salinan minimal daftar state per tipe agar paket notify
// tidak bergantung pada paket workitems (menghindari impor melingkar).
var workflowStates = map[string]stateSet{
	models.ItemReminder: {"scheduled": true, "active": true, "expiring": true, "expired": true, "cancelled": true},
	models.ItemRFS:      {"planned": true, "in_progress": true, "in_progress_field": true, "activated": true, "postponed": true, "cancelled": true},
}

type stateSet map[string]bool

func (s stateSet) valid(state string) bool { return s[state] }
