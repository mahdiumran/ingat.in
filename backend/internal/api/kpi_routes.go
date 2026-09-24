package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ingatin/backend/internal/kpi"
)

/* ---------------------------------------------------------------------------
   F20 — KPI & SLA
   --------------------------------------------------------------------------- */

// parsePeriod mengurai ?from=YYYY-MM-DD&to=YYYY-MM-DD (zona WIB).
//
// Default: 30 hari terakhir hingga besok (mencakup hari ini penuh).
func parsePeriod(r *http.Request) (time.Time, time.Time, error) {
	wib := time.FixedZone("WIB", 7*3600)
	now := time.Now().In(wib)

	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wib).AddDate(0, 0, -29)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wib).AddDate(0, 0, 1)

	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		t, err := time.ParseInLocation("2006-01-02", raw, wib)
		if err != nil {
			return from, to, fmt.Errorf("from tidak valid (YYYY-MM-DD)")
		}
		from = t
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		t, err := time.ParseInLocation("2006-01-02", raw, wib)
		if err != nil {
			return from, to, fmt.Errorf("to tidak valid (YYYY-MM-DD)")
		}
		to = t.AddDate(0, 0, 1) // inklusif
	}
	if !to.After(from) {
		return from, to, fmt.Errorf("rentang tanggal tidak valid")
	}
	return from.UTC(), to.UTC(), nil
}

// computeKPI memuat data mentah lalu menghitung agregat.
func (s *Server) computeKPI(r *http.Request) (kpi.Result, error) {
	from, to, err := parsePeriod(r)
	if err != nil {
		return kpi.Result{}, err
	}
	itemType := strings.TrimSpace(r.URL.Query().Get("type"))
	person := strings.TrimSpace(r.URL.Query().Get("person"))

	cycles, err := s.store.ListKPICycles(r.Context(), from, to, itemType, person)
	if err != nil {
		return kpi.Result{}, err
	}
	collabs, err := s.store.ListKPICollaborators(r.Context())
	if err != nil {
		return kpi.Result{}, err
	}
	tasks, err := s.store.ListKPISimpleTasks(r.Context(), from, to, person)
	if err != nil {
		return kpi.Result{}, err
	}
	return kpi.Compute(from, to, cycles, collabs, tasks, time.Now().UTC()), nil
}

// handleKPIUsers mengembalikan daftar username yang dapat dipakai sebagai
// filter KPI (owner/collaborator). Ringan dan hanya tersedia bagi yang berizin
// kpi.view, sehingga manager tidak perlu akses penuh /users.
//
// GET /api/kpi/users
func (s *Server) handleKPIUsers(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.ListKPIUsernames(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": names, "total": len(names)})
}

// handleKPI mengembalikan agregat KPI/SLA.
//
// GET /api/kpi/sla?from=&to=&type=&person=
func (s *Server) handleKPI(w http.ResponseWriter, r *http.Request) {
	res, err := s.computeKPI(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleKPIExportCSV mengunduh KPI (ringkasan + per person) sebagai CSV.
//
// GET /api/kpi/sla/export.csv?from=&to=&type=&person=
func (s *Server) handleKPIExportCSV(w http.ResponseWriter, r *http.Request) {
	res, err := s.computeKPI(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="kpi-sla-%s_%s.csv"`,
			res.Period.From.Format("20060102"), res.Period.To.Format("20060102")))
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"Ringkasan KPI"})
	_ = cw.Write([]string{"Periode", res.Period.From.Format("02 Jan 2006"), res.Period.To.Format("02 Jan 2006")})
	row := func(k, v string) { _ = cw.Write([]string{k, v}) }
	row("Total tiket", strconv.Itoa(res.Summary.TicketsTotal))
	row("Tiket selesai", strconv.Itoa(res.Summary.TicketsClosed))
	row("Tiket berjalan", strconv.Itoa(res.Summary.TicketsOpen))
	row("Respons tepat waktu", fmt.Sprintf("%.1f%%", res.Summary.ResponseMetPct))
	row("Penyelesaian tepat waktu", fmt.Sprintf("%.1f%%", res.Summary.ResolutionMetPct))
	row("Rata-rata penyelesaian (detik)", fmt.Sprintf("%.0f", res.Summary.AvgResolutionSec))
	row("Median penyelesaian (detik)", fmt.Sprintf("%.0f", res.Summary.MedianResSec))
	row("P90 penyelesaian (detik)", fmt.Sprintf("%.0f", res.Summary.P90ResSec))
	row("Pelanggaran SLA", strconv.Itoa(res.Summary.Breached))
	row("Skor SLA", fmt.Sprintf("%.1f", res.Summary.SLAScore))
	row("Todo tepat waktu", fmt.Sprintf("%.1f%%", res.Summary.TodoOnTimePct))
	row("Daily tepat waktu", fmt.Sprintf("%.1f%%", res.Summary.DailyOnTimePct))
	_ = cw.Write(nil)

	_ = cw.Write([]string{"Per Person"})
	_ = cw.Write([]string{
		"Username", "Owner", "Selesai", "Berjalan", "Respons tepat (%)",
		"Penyelesaian tepat (%)", "Avg penyelesaian (detik)", "Pelanggaran",
		"Todo (%)", "Daily (%)", "Skor SLA",
	})
	for _, p := range res.PerPerson {
		_ = cw.Write([]string{
			p.Username,
			strconv.Itoa(p.AssignedTotal),
			strconv.Itoa(p.ResolvedTotal),
			strconv.Itoa(p.OpenTotal),
			fmt.Sprintf("%.1f", p.ResponseMetPct),
			fmt.Sprintf("%.1f", p.ResolutionMetPct),
			fmt.Sprintf("%.0f", p.AvgResolutionSeconds),
			strconv.Itoa(p.Breached),
			fmt.Sprintf("%.1f", pctInt(p.TodoOnTime, p.TodoTotal)),
			fmt.Sprintf("%.1f", pctInt(p.DailyOnTime, p.DailyTotal)),
			fmt.Sprintf("%.1f", p.SLAScore),
		})
	}
}

func pctInt(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}
