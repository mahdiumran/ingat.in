package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type triggerNotificationRequest struct {
	// TemplateKey opsional: paksa template tertentu (mis. TODO_CREATED).
	TemplateKey string `json:"template_key"`
	// OffsetLabel opsional: label offset yang ditampilkan pada pesan.
	OffsetLabel string `json:"offset_label"`
	// Severity opsional: info | warning | critical.
	Severity string `json:"severity"`
	// Note opsional ditambahkan sebagai catatan pada payload.
	Note string `json:"note"`
	// ResetReminderOffset, bila true, mengosongkan last_offset_fired sehingga
	// fanout dapat mengirim ulang offset yang sudah lewat pada putaran berikut.
	ResetReminderOffset *bool `json:"reset_reminder_offset"`
}

/* ---------------------------------------------------------------------------
   Trigger notifikasi manual
   --------------------------------------------------------------------------- */

// handleTriggerNotification mengirim notifikasi untuk sebuah work item sekarang.
//
// POST /api/items/{id}/notify
//
// Notifikasi tetap melalui outbox (bukan dikirim langsung dari handler) agar
// memperoleh retry, pencatatan rendered_body, dan pemulihan saat worker mati.
// Setiap panggilan memakai event_key unik bertimestamp sehingga tidak
// tertelan sebagai duplikat, tetapi tetap tercatat di timeline.
func (s *Server) handleTriggerNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := s.itemID(w, r)
	if !ok {
		return
	}

	var req triggerNotificationRequest
	// Body bersifat opsional: kirim ulang dengan pengaturan default bila kosong.
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	item, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		s.itemError(w, err)
		return
	}

	targetID := item.TargetID
	if targetID == nil {
		def, err := s.store.DefaultTargetID(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadRequest, "tidak ada target notifikasi: item belum punya target dan tidak ada target default")
			return
		}
		targetID = def
	}

	// Template & severity: default sesuai tipe bila tidak dipaksa.
	tplKey, severity := createdTemplateFor(item.ItemType)
	if strings.TrimSpace(req.TemplateKey) != "" {
		tplKey = strings.TrimSpace(req.TemplateKey)
	}
	if strings.TrimSpace(req.Severity) != "" {
		severity = strings.TrimSpace(req.Severity)
	}

	offsetLabel := req.OffsetLabel
	if offsetLabel == "" {
		offsetLabel = "MANUAL"
	}

	payload := s.buildNotifyPayload(r, item, offsetLabel, severity)
	if req.Note != "" {
		if payload.Notes != "" {
			payload.Notes += "\n" + req.Note
		} else {
			payload.Notes = req.Note
		}
	}

	// Kunci unik per detik + tipe: cukup mencegah dobel-tap, tetapi tetap
	// memungkinkan kirim ulang yang disengaja.
	eventKey := fmt.Sprintf("manual:%s:%d", item.ID, time.Now().Unix())

	res, err := s.notifier.Outbox().Enqueue(r.Context(), notify.EnqueueParams{
		EventKey:    eventKey,
		WorkItemID:  &item.ID,
		SourceType:  "manual",
		TemplateKey: tplKey,
		OffsetLabel: offsetLabel,
		Severity:    severity,
		TargetID:    *targetID,
		Payload:     payload,
	})
	if err != nil {
		// Tidak ada binding aktif bukan kesalahan server; beri pesan yang jelas
		// agar operator tahu harus menambahkan binding di halaman Targets.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Reset offset (opsional) agar fanout dapat menjadwalkan ulang offset.
	if req.ResetReminderOffset != nil && *req.ResetReminderOffset && item.ItemType == models.ItemReminder {
		if err := s.store.ResetReminderLastOffset(r.Context(), item.ID); err != nil {
			log.Printf("api: reset offset reminder %s: %v", item.RefNo, err)
		}
	}

	// Catat di timeline agar terlihat di detail item.
	_ = s.store.Tx(r.Context(), func(tx pgx.Tx) error {
		return workitems.AppendEvent(r.Context(), tx, workitems.EventInput{
			WorkItemID: item.ID.String(),
			EventType:  "notified",
			Actor:      currentUsername(r),
			ToValue:    offsetLabel,
			Detail:     map[string]any{"manual": true, "template": tplKey, "added": res.Added},
		})
	})

	s.audit(r, "", "item.notify", "work_item", item.ID.String(), map[string]any{
		"ref_no": item.RefNo, "added": res.Added, "skipped": res.Skipped,
	}, true)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      res.Added > 0,
		"added":   res.Added,
		"skipped": res.Skipped,
		"message": fmt.Sprintf("%d pesan diantrikan untuk dikirim.", res.Added),
	})
}

// buildNotifyPayload menyusun payload berdasarkan tipe item.
func (s *Server) buildNotifyPayload(r *http.Request, item *models.WorkItem, offsetLabel, severity string) notify.Payload {
	p := notify.Payload{
		RefNo:       item.RefNo,
		Title:       item.Title,
		ItemType:    item.ItemType,
		Priority:    item.Priority,
		Status:      item.Status,
		Stage:       item.Stage,
		Owner:       item.OwnerUsername,
		Requester:   item.RequesterUsername,
		CreatedBy:   item.CreatedBy,
		DueAt:       notify.FormatWIB(item.DueAt),
		ExpireAt:    notify.FormatWIB(item.ExpireAt),
		StartAt:     notify.FormatWIB(item.StartAt),
		CreatedAt:   notify.FormatWIB(&item.CreatedAt),
		OffsetLabel: offsetLabel,
		Severity:    severity,
		Description: item.Description,
		DeviceRef:   item.DeviceRef,
		ServiceRef:  item.ServiceRef,
		CustomerRef: item.CustomerRef,
		SubjectName: item.Title,
		Category:    item.ItemType,
	}
	if item.ExpireAt != nil {
		p.Remaining = notify.HumanRemaining(*item.ExpireAt, time.Now().UTC())
	}
	if item.RFS != nil {
		p.CustomerName = item.RFS.CustomerName
		p.ServicePackage = item.RFS.ServicePackage
		p.Bandwidth = item.RFS.Bandwidth
		p.PicNOC = item.RFS.PicNOC
		p.PicSales = item.RFS.PicSales
		p.Site = item.RFS.Site
	}
	if item.Reminder != nil {
		p.SubjectName = item.Reminder.SubjectName
		p.Category = item.Reminder.Category
	}
	if item.TargetID != nil {
		if name, err := s.store.GetTargetName(r.Context(), *item.TargetID); err == nil {
			p.TargetName = name
		}
	}
	return p
}

/* ---------------------------------------------------------------------------
   Feed notifikasi sidebar
   --------------------------------------------------------------------------- */

// handleListNotifications mengembalikan feed aktivitas untuk lonceng sidebar.
//
// GET /api/notifications?type=&limit=&since_id=
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	var sinceID int64
	if raw := q.Get("since_id"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			sinceID = v
		}
	}

	feed, err := s.store.ListNotificationFeed(r.Context(), q.Get("type"), limit, sinceID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	maxID, err := s.store.MaxEventID(r.Context())
	if err != nil {
		writeInternalError(w, err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": feed,
		"total":         len(feed),
		"max_id":        maxID,
		"server_time":   time.Now().UTC().Format(time.RFC3339),
	})
}

type markReadRequest struct {
	// LastSeenID: seluruh notifikasi dengan id <= nilai ini dianggap dibaca.
	LastSeenID int64 `json:"last_seen_id"`
}

// handleMarkNotificationsRead mencatat posisi baca feed.
//
// POST /api/notifications/read
//
// Pos disimpan per-user pada settings sehingga jumlah "belum dibaca" bertahan
// antar sesi tanpa tabel baru.
func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req markReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.LastSeenID <= 0 {
		writeErr(w, http.StatusBadRequest, "last_seen_id wajib > 0")
		return
	}
	if err := s.store.SetUserSettingInt(r.Context(), currentUsername(r), "notifications_last_seen_id", req.LastSeenID); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "last_seen_id": req.LastSeenID})
}
