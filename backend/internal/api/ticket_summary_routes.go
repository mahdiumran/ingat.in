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
   F44 — Ringkasan tiket terbuka (incident/request/change).
   --------------------------------------------------------------------------- */

type ticketSummarySettingsRequest struct {
	Mode            string `json:"mode"`
	IntervalMinutes *int   `json:"interval_minutes"`
}

// GET /api/settings/ticket-summary
func (s *Server) handleGetTicketSummarySettings(w http.ResponseWriter, r *http.Request) {
	mode := "interval"
	if v, err := s.store.GetSetting(r.Context(), "ticket.summary_mode"); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(m) != "" {
			mode = strings.TrimSpace(strings.ToLower(m))
		}
	}
	interval := 60
	if v, err := s.store.GetSetting(r.Context(), "ticket.summary_interval_min"); err == nil {
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

// POST /api/settings/ticket-summary
func (s *Server) handleSetTicketSummarySettings(w http.ResponseWriter, r *http.Request) {
	var req ticketSummarySettingsRequest
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
	actor := currentUsername(r)
	if err := s.store.SetSetting(r.Context(), "ticket.summary_mode", map[string]any{"value": mode}, actor); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := s.store.SetSetting(r.Context(), "ticket.summary_interval_min", map[string]any{"value": interval}, actor); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "settings.ticket_summary", "settings", "ticket.summary", map[string]any{
		"mode": mode, "interval": interval,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "interval_minutes": interval})
}

// POST /api/settings/ticket-summary/preview
func (s *Server) handlePreviewTicketSummary(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	added, err := s.sendTicketSummaryNow(r.Context(), now, "ticket_summary:manual:"+strconv.FormatInt(now.Unix(), 10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": added > 0, "added": added})
}

// maybeSendTicketSummary dipanggil setelah pembuatan/perubahan tiket. Mode
// on_change dibatasi satu event per menit agar mutasi beruntun tidak membanjiri.
func (s *Server) maybeSendTicketSummary(ctx context.Context, now time.Time) {
	if v, err := s.store.GetSetting(ctx, "ticket.summary_mode"); err == nil {
		if m, ok := v["value"].(string); ok && strings.TrimSpace(strings.ToLower(m)) == "on_change" {
			key := "ticket_summary:on_change:" + now.UTC().Format("2006-01-02-15-04")
			_, _ = s.sendTicketSummaryNow(ctx, now, key)
		}
	}
}

func (s *Server) sendTicketSummaryNow(ctx context.Context, now time.Time, eventKey string) (int, error) {
	targetID, err := s.store.DefaultTargetID(ctx)
	if err != nil {
		return 0, err
	}
	sum, err := s.store.ListOpenTicketSummary(ctx)
	if err != nil {
		return 0, err
	}

	symbol := map[string]string{"incident": "🔴", "request": "🟡", "change": "🔵"}
	lines := make([]string, 0, sum.Total()+3)
	appendGroup := func(title string, items []repository.OpenTicketSummaryItem) {
		lines = append(lines, fmt.Sprintf("— %s (%d) —", title, len(items)))
		if len(items) == 0 {
			lines = append(lines, "(tidak ada)")
			return
		}
		for _, it := range items {
			line := fmt.Sprintf("• %s %s %s", it.RefNo, it.Title, symbol[it.ItemType])
			if it.Owner != "" {
				line += " — " + it.Owner
			}
			if it.Overdue {
				line += " [TERLAMBAT]"
			}
			lines = append(lines, line)
		}
	}
	appendGroup("Tiket baru", sum.New)
	appendGroup("Sedang dikerjakan", sum.Progress)
	appendGroup("Selesai — menunggu penutupan", sum.Done)

	local := now.In(time.FixedZone("WIB", 7*3600))
	desc, notes := notify.BuildTicketSummary(local.Format("02 Jan 2006 15:04"), len(sum.New), len(sum.Progress), len(sum.Done), lines)
	res, err := s.notifier.Outbox().Enqueue(ctx, notify.EnqueueParams{
		EventKey: eventKey, SourceType: "system", TemplateKey: notify.TemplateTicketSummary,
		Severity: models.SeverityInfo, TargetID: *targetID,
		Payload: notify.Payload{Title: "Ringkasan Tiket Terbuka", CreatedAt: notify.FormatWIB(&now), Description: desc, Notes: notes},
	})
	if err != nil {
		return 0, err
	}
	return res.Added, nil
}
