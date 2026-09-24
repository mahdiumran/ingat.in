package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   F22 — Ringkasan tugas harian (pending / in_progress / done)
   --------------------------------------------------------------------------- */

type dailySummarySettingsRequest struct {
	// Mode: on_change | interval | off
	Mode string `json:"mode"`
	// IntervalMinutes hanya dipakai bila mode=interval (5..1440).
	IntervalMinutes *int `json:"interval_minutes"`
}

// handleGetDailySummarySettings mengembalikan mode & interval ringkasan tugas.
//
// GET /api/settings/daily-summary
func (s *Server) handleGetDailySummarySettings(w http.ResponseWriter, r *http.Request) {
	mode := "interval"
	if v, err := s.store.GetSetting(r.Context(), "daily_task.summary_mode"); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(m) != "" {
			mode = strings.TrimSpace(strings.ToLower(m))
		}
	}
	interval := 60
	if v, err := s.store.GetSetting(r.Context(), "daily_task.summary_interval_min"); err == nil {
		switch n := v["value"].(type) {
		case float64:
			interval = int(n)
		case string:
			if p, e := strconv.Atoi(strings.TrimSpace(n)); e == nil {
				interval = p
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":             mode,
		"interval_minutes": interval,
		"modes":            []string{"on_change", "interval", "off"},
	})
}

// handleSetDailySummarySettings menyimpan preferensi ringkasan tugas.
//
// POST /api/settings/daily-summary
func (s *Server) handleSetDailySummarySettings(w http.ResponseWriter, r *http.Request) {
	var req dailySummarySettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	switch mode {
	case "on_change", "interval", "off":
	default:
		writeErr(w, http.StatusBadRequest, "mode harus salah satu: on_change, interval, off")
		return
	}

	interval := 60
	if req.IntervalMinutes != nil {
		interval = *req.IntervalMinutes
	}
	if mode == "interval" && (interval < 5 || interval > 1440) {
		writeErr(w, http.StatusBadRequest, "interval_minutes harus 5..1440")
		return
	}

	if err := s.store.SetSetting(r.Context(), "daily_task.summary_mode",
		map[string]any{"value": mode}, currentUsername(r)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), "daily_task.summary_interval_min",
		map[string]any{"value": interval}, currentUsername(r)); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "settings.daily_summary", "settings", "daily_task.summary", map[string]any{
		"mode": mode, "interval": interval,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "interval_minutes": interval})
}

// handlePreviewDailySummary mengirim ringkasan tugas sekarang (uji manual).
// Berguna agar operator dapat memverifikasi isi pesan tanpa menunggu jadwal.
//
// POST /api/settings/daily-summary/preview
func (s *Server) handlePreviewDailySummary(w http.ResponseWriter, r *http.Request) {
	res, err := s.sendDailyTaskSummaryNow(r.Context(), time.Now().UTC(), "manual:"+strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": res > 0, "added": res})
}

// maybeSendDailySummary dipanggil setelah perubahan status daily task; hanya
// mengirim bila mode = on_change.
func (s *Server) maybeSendDailySummary(ctx context.Context, now time.Time) {
	if v, err := s.store.GetSetting(ctx, "daily_task.summary_mode"); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(strings.ToLower(m)) == "on_change" {
			// Kunci per menit agar perubahan berturut tidak membanjiri outbox.
			key := "daily_summary:on_change:" + now.UTC().Format("2006-01-02-15-04")
			_, _ = s.sendDailyTaskSummaryNow(ctx, now, key)
		}
	}
}

// sendDailyTaskSummaryNow membangun & mengantrikan ringkasan tugas harian
// dengan kunci outbox tertentu. Dipakai pemicu manual maupun on-change.
func (s *Server) sendDailyTaskSummaryNow(ctx context.Context, now time.Time, eventKey string) (int, error) {
	targetID, err := s.store.DefaultTargetID(ctx)
	if err != nil {
		return 0, err
	}
	loc := time.FixedZone("WIB", 7*3600)
	local := now.In(loc)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	sum, err := s.store.ListDailyTaskSummary(ctx, dayStart.UTC(), dayEnd.UTC())
	if err != nil {
		return 0, err
	}

	group := notify.SummaryGroup{
		Pending:    toSummaryTasksAPI(sum.Pending),
		InProgress: toSummaryTasksAPI(sum.InProgress),
		Done:       toSummaryTasksAPI(sum.Done),
	}
	dayLabel := local.Format("02 Jan 2006 15:04")
	desc, notes := notify.BuildDailySummary(dayLabel, group)

	res, err := s.notifier.Outbox().Enqueue(ctx, notify.EnqueueParams{
		EventKey:    eventKey,
		SourceType:  "system",
		TemplateKey: notify.TemplateDailySummary,
		Severity:    models.SeverityInfo,
		TargetID:    *targetID,
		Payload: notify.Payload{
			Title:       "Ringkasan Tugas Harian",
			CreatedAt:   notify.FormatWIB(&now),
			Description: desc,
			Notes:       notes,
		},
	})
	if err != nil {
		return 0, err
	}
	return res.Added, nil
}

func toSummaryTasksAPI(in []repository.DailyTaskSummaryItem) []notify.SummaryTask {
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
