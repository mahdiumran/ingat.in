package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   F45 — Ringkasan Aktivasi/EWO (RFS) terbuka.

   Pola sama dengan ringkasan tiket (F44): mode on_change/interval/off,
   interval 5..1440 menit, pemicu manual (preview) untuk admin.
   --------------------------------------------------------------------------- */

// Kunci setting ringkasan RFS.
const (
	settingRFSSummaryMode     = "rfs.summary_mode"
	settingRFSSummaryInterval = "rfs.summary_interval_min"
)

type rfsSummarySettingsRequest struct {
	Mode            string `json:"mode"`
	IntervalMinutes *int   `json:"interval_minutes"`
}

// GET /api/settings/rfs-summary
func (s *Server) handleGetRFSSummarySettings(w http.ResponseWriter, r *http.Request) {
	mode := "interval"
	if v, err := s.store.GetSetting(r.Context(), settingRFSSummaryMode); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(m) != "" {
			mode = strings.TrimSpace(strings.ToLower(m))
		}
	}
	interval := 60
	if v, err := s.store.GetSetting(r.Context(), settingRFSSummaryInterval); err == nil {
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
		"mode": mode, "interval_minutes": interval,
		"modes": []string{"on_change", "interval", "off"},
	})
}

// POST /api/settings/rfs-summary
func (s *Server) handleSetRFSSummarySettings(w http.ResponseWriter, r *http.Request) {
	var req rfsSummarySettingsRequest
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
	// Simpan interval yang selalu valid agar pergantian mode ke interval tidak
	// mengaktifkan nilai rusak yang pernah dikirim saat mode off/on_change.
	if mode != "interval" && (interval < 5 || interval > 1440) {
		interval = 60
	}
	actor := currentUsername(r)
	if err := s.store.SetSetting(r.Context(), settingRFSSummaryMode, map[string]any{"value": mode}, actor); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), settingRFSSummaryInterval, map[string]any{"value": interval}, actor); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "settings.rfs_summary", "settings", "rfs.summary", map[string]any{
		"mode": mode, "interval": interval,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "interval_minutes": interval})
}

// POST /api/settings/rfs-summary/preview
func (s *Server) handlePreviewRFSSummary(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	added, err := s.sendRFSSummaryNow(r.Context(), now,
		"rfs_summary:manual:"+strconv.FormatInt(now.Unix(), 10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": added > 0, "added": added})
}

// maybeSendRFSSummary dipanggil setelah pembuatan/perubahan RFS yang bersifat
// material. Mode on_change dibatasi satu event per menit agar mutasi beruntun
// tidak membanjiri outbox. Best-effort: kegagalan tidak memengaruhi operasi.
func (s *Server) maybeSendRFSSummary(ctx context.Context, now time.Time) {
	if v, err := s.store.GetSetting(ctx, settingRFSSummaryMode); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(strings.ToLower(m)) == "on_change" {
			key := "rfs_summary:on_change:" + now.UTC().Format("2006-01-02-15-04")
			_, _ = s.sendRFSSummaryNow(ctx, now, key)
		}
	}
}

// sendRFSSummaryNow membangun & mengantrikan ringkasan RFS terbuka dengan kunci
// outbox tertentu (idempoten per kunci).
func (s *Server) sendRFSSummaryNow(ctx context.Context, now time.Time, eventKey string) (int, error) {
	targetID, err := s.store.DefaultTargetID(ctx)
	if err != nil {
		return 0, err
	}
	sum, err := s.store.ListOpenRFSSummary(ctx)
	if err != nil {
		return 0, err
	}

	lines := make([]string, 0, sum.Total()+3)
	appendGroup := func(title string, items []repository.OpenRFSSummaryItem) {
		lines = append(lines, fmt.Sprintf("— %s (%d) —", title, len(items)))
		if len(items) == 0 {
			lines = append(lines, "(tidak ada)")
			return
		}
		for _, it := range items {
			lines = append(lines, notify.FormatRFSSummaryLine(notify.RFSSummaryRow{
				RefNo:    it.RefNo,
				Title:    it.Title,
				Customer: it.Customer,
				Category: it.Category,
				Owner:    it.Owner,
				Schedule: notify.FormatWIB(it.Schedule),
				Overdue:  it.Overdue,
				Tags:     it.Tags,
			}))
		}
	}
	appendGroup("Direncanakan", sum.Planned)
	appendGroup("Sedang dikerjakan", sum.InProgress)
	appendGroup("Menunggu troubleshooting", sum.Pending)

	local := now.In(s.location())
	desc, notes := notify.BuildRFSSummary(
		local.Format("02 Jan 2006 15:04"),
		len(sum.Planned), len(sum.InProgress), len(sum.Pending), lines)

	res, err := s.notifier.Outbox().Enqueue(ctx, notify.EnqueueParams{
		EventKey:    eventKey,
		SourceType:  "system",
		TemplateKey: notify.TemplateRFSSummary,
		Severity:    models.SeverityInfo,
		TargetID:    *targetID,
		Payload: notify.Payload{
			Title:       "Ringkasan Aktivasi/EWO Terbuka",
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
