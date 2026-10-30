package api

import (
	"context"
	"fmt"
	"strings"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/workitems"
)

/* ---------------------------------------------------------------------------
   F45 — Master data Aktivasi/EWO: kategori & tag.

   Kategori (`ewo_category`) dipakai sebagai pilihan pada form RFS dan nilainya
   disalin (snapshot code+label) ke rfs_details agar riwayat tetap terbaca.
   Tag (`ewo_tag`) memvalidasi work_items.tags khusus untuk item_type=rfs.
   --------------------------------------------------------------------------- */

// resolveRFSCategory menyelesaikan snapshot kategori EWO dari code/label yang
// dikirim klien.
//
//   - code kosong  -> kategori dikosongkan; label kosong diharapkan. Pemanggil
//     hanya boleh mempertahankan label bebas untuk snapshot legacy yang sama.
//   - code terisi  -> harus cocok dengan code ATAU label ewo_category aktif;
//     label kanonik master data yang dipakai sebagai snapshot.
func (s *Server) resolveRFSCategory(ctx context.Context, code, label string) (string, string, error) {
	code = strings.TrimSpace(code)
	label = strings.TrimSpace(label)
	// code kosong hanya mengosongkan kategori; label bebas baru ditolak oleh
	// handler. Snapshot legacy dipertahankan langsung tanpa memanggil fungsi ini.
	if code == "" {
		return "", "", nil
	}
	entries, err := s.store.ListMasterData(ctx, repository.MasterDataFilter{
		Kind:       models.KindEWOCategory,
		ActiveOnly: true,
	})
	if err != nil {
		return "", "", fmt.Errorf("gagal memuat kategori EWO: %w", err)
	}
	for _, e := range entries {
		if strings.EqualFold(e.Code, code) || strings.EqualFold(e.Label, code) {
			return e.Code, e.Label, nil
		}
	}
	return "", "", fmt.Errorf("kategori EWO %q tidak dikenal: %w", code, errValidation)
}

// validateRFSTags memastikan setiap tag RFS termasuk master data `ewo_tag`
// yang aktif.
//
// Nilai pada allowLegacy (tag yang sudah tersimpan pada item) selalu diizinkan
// agar data lama tidak hilang saat edit. Bila belum ada entri `ewo_tag` aktif
// sama sekali, validasi dilewati untuk kompatibilitas database lama.
func (s *Server) validateRFSTags(ctx context.Context, tags, allowLegacy []string) error {
	normalized := workitems.NormalizeTags(tags)
	if len(normalized) == 0 {
		return nil
	}
	entries, err := s.store.ListMasterData(ctx, repository.MasterDataFilter{
		Kind:       models.KindEWOTag,
		ActiveOnly: true,
	})
	if err != nil {
		return fmt.Errorf("gagal memuat tag EWO: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	allowed := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		if strings.TrimSpace(e.Code) != "" {
			allowed = append(allowed, e.Code)
		}
		if strings.TrimSpace(e.Label) != "" {
			allowed = append(allowed, e.Label)
		}
	}
	for _, t := range normalized {
		if !rfsTagAllowed(t, allowed, allowLegacy) {
			return fmt.Errorf("tag %q tidak dikenal untuk Aktivasi/EWO: %w", t, errValidation)
		}
	}
	return nil
}

// rfsTagAllowed melaporkan apakah tag termasuk daftar yang diizinkan (kode/
// label ewo_tag aktif) atau nilai legacy yang sudah tersimpan pada item.
// Perbandingan tanpa memandang besar-kecil huruf; tag kosong selalu dianggap sah.
func rfsTagAllowed(tag string, allowed, legacy []string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return true
	}
	for _, v := range allowed {
		if strings.EqualFold(strings.TrimSpace(v), tag) {
			return true
		}
	}
	for _, v := range legacy {
		if strings.EqualFold(strings.TrimSpace(v), tag) {
			return true
		}
	}
	return false
}

// rfsMaterialUpdate melaporkan apakah sebuah PATCH menyentuh data yang
// ditampilkan pada ringkasan RFS (judul, owner, tim, jadwal, tag, kategori,
// customer/PIC) — dipakai untuk pemicu ringkasan mode on_change.
func rfsMaterialUpdate(req workItemUpdateRequest) bool {
	if req.RFS != nil {
		return true
	}
	return req.Title != nil || req.Owner != nil || req.TeamID != nil ||
		req.DueAt != nil || req.StartAt != nil || req.ExpireAt != nil || req.Tags != nil
}
