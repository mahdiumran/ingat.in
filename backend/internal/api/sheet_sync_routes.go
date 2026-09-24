package api

import (
	"net/http"
	"strings"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
)

/* ---------------------------------------------------------------------------
   F18 — Sinkronisasi Todo Task ke Google Spreadsheet (admin only).
   --------------------------------------------------------------------------- */

// sheetSyncRequest adalah isi POST /api/sheet-sync.
type sheetSyncRequest struct {
	Enabled         *bool   `json:"enabled"`
	SpreadsheetID   *string `json:"spreadsheet_id"`
	SheetName       *string `json:"sheet_name"`
	ServiceAccount  *string `json:"service_account_json"`
	ResetHeaderFlag *bool   `json:"reset_header"`
}

// handleGetSheetSync mengembalikan status konfigurasi sinkronisasi.
//
// Kredensial TIDAK pernah dikembalikan utuh; hanya client_email + penanda
// bahwa service account sudah tersimpan.
func (s *Server) handleGetSheetSync(w http.ResponseWriter, r *http.Request) {
	cfgRow, sa, err := sheets.LoadConfig(r.Context(), s.store, s.cfg.CredentialKey)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	clientEmail := ""
	if sa != nil {
		clientEmail = sa.ClientEmail
	}

	counts, err := s.store.SheetSyncQueueCounts(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config": map[string]any{
			"enabled":             cfgRow.Enabled,
			"spreadsheet_id":      cfgRow.SpreadsheetID,
			"sheet_name":          cfgRow.SheetName,
			"service_account_set": sa != nil,
			"client_email":        clientEmail,
			"header_written":      cfgRow.HeaderWritten,
			"last_sync_at":        cfgRow.LastSyncAt,
			"last_error":          cfgRow.LastError,
		},
		"queue": map[string]any{
			"pending": counts["pending"],
			"sending": counts["sending"],
			"sent":    counts["sent"],
			"failed":  counts["failed"],
		},
	})
}

// handleSaveSheetSync menyimpan konfigurasi sinkronisasi.
func (s *Server) handleSaveSheetSync(w http.ResponseWriter, r *http.Request) {
	var req sheetSyncRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Ambil konfigurasi saat ini agar field yang tidak dikirim tidak hilang.
	current, _, err := sheets.LoadConfig(r.Context(), s.store, s.cfg.CredentialKey)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	params := updateSheetParamsFrom(current, req)

	// Validasi nama sheet bila dikirim (dan tidak kosong).
	if req.SheetName != nil {
		if err := sheets.ValidateSheetName(*req.SheetName); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Validasi + enkripsi service account bila dikirim dan tidak kosong.
	if req.ServiceAccount != nil && strings.TrimSpace(*req.ServiceAccount) != "" {
		enc, _, err := sheets.EncryptServiceAccount(s.cfg.CredentialKey, *req.ServiceAccount)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		params.ServiceAccountEnc = &enc
	}

	if err := s.store.UpdateSheetSyncConfig(r.Context(), params); err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, currentUsername(r), "sheet_sync.update", "sheet_sync_config", "",
		map[string]any{"enabled": params.Enabled, "sheet_name": params.SheetName}, true)

	// Kembalikan status terbaru agar UI langsung sinkron.
	s.handleGetSheetSync(w, r)
}

// handleTestSheetSync menguji koneksi ke spreadsheet yang dikonfigurasi.
func (s *Server) handleTestSheetSync(w http.ResponseWriter, r *http.Request) {
	cfgRow, sa, err := sheets.LoadConfig(r.Context(), s.store, s.cfg.CredentialKey)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sa == nil {
		writeErr(w, http.StatusBadRequest, "service account belum diisi")
		return
	}
	if strings.TrimSpace(cfgRow.SpreadsheetID) == "" {
		writeErr(w, http.StatusBadRequest, "spreadsheet_id belum diisi")
		return
	}

	client, err := sheets.NewClient(r.Context(), sa.RawJSON(), cfgRow.SpreadsheetID, cfgRow.SheetName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := client.TestConnection(r.Context(), sa.ClientEmail); err != nil {
		s.audit(r, currentUsername(r), "sheet_sync.test", "sheet_sync_config", "", nil, false)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	// Header pasti sudah tertulis setelah uji berhasil.
	_ = s.store.MarkSheetHeaderWritten(r.Context())
	s.audit(r, currentUsername(r), "sheet_sync.test", "sheet_sync_config", "", nil, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"client_email": sa.ClientEmail,
		"sheet_name":   cfgRow.SheetName,
	})
}

// handleCreateSheetTab membuat tab sheet bila belum ada.
func (s *Server) handleCreateSheetTab(w http.ResponseWriter, r *http.Request) {
	cfgRow, sa, err := sheets.LoadConfig(r.Context(), s.store, s.cfg.CredentialKey)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sa == nil {
		writeErr(w, http.StatusBadRequest, "service account belum diisi")
		return
	}
	if strings.TrimSpace(cfgRow.SpreadsheetID) == "" {
		writeErr(w, http.StatusBadRequest, "spreadsheet_id belum diisi")
		return
	}

	client, err := sheets.NewClient(r.Context(), sa.RawJSON(), cfgRow.SpreadsheetID, cfgRow.SheetName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := client.CreateSheetTab(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// Pastikan header benar (self-healing bila kolom berubah antar versi).
	if err := client.EnsureHeader(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = s.store.MarkSheetHeaderWritten(r.Context())

	s.audit(r, currentUsername(r), "sheet_sync.create_tab", "sheet_sync_config", "",
		map[string]any{"sheet_name": cfgRow.SheetName}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sheet_name": cfgRow.SheetName})
}

// updateSheetParamsFrom menggabungkan konfigurasi saat ini dengan permintaan,
// sehingga field yang tidak dikirim klien tidak terhapus (PATCH semantics).
func updateSheetParamsFrom(current *models.SheetSyncConfig, req sheetSyncRequest) repository.UpdateSheetSyncConfigParams {
	p := repository.UpdateSheetSyncConfigParams{
		Enabled:       current.Enabled,
		SpreadsheetID: current.SpreadsheetID,
		SheetName:     current.SheetName,
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.SpreadsheetID != nil {
		p.SpreadsheetID = strings.TrimSpace(*req.SpreadsheetID)
	}
	if req.SheetName != nil {
		name := strings.TrimSpace(*req.SheetName)
		if name == "" {
			name = current.SheetName
		}
		p.SheetName = name
	}
	if req.ResetHeaderFlag != nil {
		p.ResetHeader = *req.ResetHeaderFlag
	}
	return p
}
