package api

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/workitems"
)

// updateSLACycleOnStatus memelihara siklus SLA tiket saat status berubah:
//   - mencatat respons pertama pada siklus aktif,
//   - menutup siklus aktif saat tiket mencapai status terminal,
//   - membuka siklus baru saat tiket dibuka kembali (reopen).
func (s *Server) updateSLACycleOnStatus(
	ctx context.Context,
	tx pgx.Tx,
	item *models.WorkItem,
	newStatus string,
	result workitems.TransitionResult,
	actor string,
	now time.Time,
	initialState string,
) error {
	// Catat respons pertama pada siklus aktif (status meninggalkan state awal).
	if newStatus != initialState {
		if err := s.store.SetSLACycleFirstResponse(ctx, tx, item.ID, now); err != nil {
			return err
		}
	}

	switch {
	case result.IsClosing:
		// Tiket ditutup: tutup siklus aktif dengan handler = owner saat ini.
		first := item.FirstResponseAt
		if first == nil && newStatus != initialState {
			first = &now
		}
		handler := item.OwnerUsername
		if handler == "" {
			handler = actor
		}
		return s.store.CloseCurrentSLACycle(ctx, tx, item.ID, &now, first, handler, actor)

	case result.IsReopen:
		// Tiket dibuka kembali: buka siklus baru + naikkan penghitung reopen.
		if _, err := s.store.OpenNewSLACycle(ctx, tx, item.ID, now); err != nil {
			return err
		}
		return s.store.IncrementReopenCount(ctx, tx, item.ID)
	}
	return nil
}

// defaultDailyTaskDue mengembalikan tenggat default Daily Task: 23:59:00 WIB
// pada tanggal `base` (dikonversi menurut zona WIB).
func defaultDailyTaskDue(base time.Time) *time.Time {
	wib := time.FixedZone("WIB", 7*3600)
	d := base.In(wib)
	due := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 0, 0, wib)
	return &due
}
