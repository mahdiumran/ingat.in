package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"ingatin/backend/internal/backup"
)

/* ---------------------------------------------------------------------------
   F34 — Cadangan & pemulihan database + unggah otomatis ke FTP (admin only).
   --------------------------------------------------------------------------- */

// backupDir mengembalikan direktori penyimpanan cadangan efektif.
func (s *Server) backupDir() string {
	if d := strings.TrimSpace(s.cfg.BackupDir); d != "" {
		return d
	}
	return "/app/data/backups"
}

// handleListBackups mengembalikan konfigurasi + daftar berkas cadangan.
//
// GET /api/backup
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	cfg, err := backup.LoadConfig(r.Context(), s.store)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	items, err := backup.List(s.backupDir())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// Sinkronkan metadata unggah dari catatan konfigurasi bila nama cocok.
	if cfg.LastFile != "" {
		for i := range items {
			if items[i].Name == cfg.LastFile {
				if cfg.LastRunAt != nil {
					if t, perr := time.Parse(time.RFC3339, *cfg.LastRunAt); perr == nil {
						items[i].UploadedAt = &t
					}
				}
				items[i].Uploaded = cfg.LastStatus == "ok"
				if cfg.LastStatus == "error" {
					items[i].UploadErr = cfg.LastError
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":  cfg,
		"backups": items,
		"dir":     s.backupDir(),
		"tools": map[string]any{
			"pg_dump":    whichTool("pg_dump"),
			"pg_restore": whichTool("pg_restore"),
		},
	})
}

// whichTool melaporkan apakah sebuah executable tersedia di PATH.
func whichTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// int64String mengubah int64 menjadi string basis-10.
func int64String(v int64) string { return strconv.FormatInt(v, 10) }

// validateCron memverifikasi ekspresi cron 5-field standar.
func validateCron(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(expr)
	return err
}

// handleCreateBackup membuat cadangan sekarang (manual).
//
// POST /api/backup
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	cfg, err := backup.LoadConfig(r.Context(), s.store)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	info, err := backup.Create(ctx, backup.DumpOptions{
		DBURL: s.cfg.DBURL,
		Dir:   s.backupDir(),
		Label: "manual",
	})
	if err != nil {
		s.audit(r, "", "backup.create", "backup", "", map[string]any{"error": err.Error()}, false)
		writeErr(w, http.StatusInternalServerError, "gagal membuat cadangan: "+err.Error())
		return
	}

	s.audit(r, "", "backup.create", "backup", info.Name,
		map[string]any{"size": info.SizeBytes}, true)

	// Unggah otomatis ke FTP bila diaktifkan.
	uploadMsg := ""
	if cfg.FTP.Enabled {
		fctx, fcancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer fcancel()
		if uerr := s.uploadBackup(fctx, cfg, info.Name); uerr != nil {
			uploadMsg = "unggah FTP gagal: " + uerr.Error()
			s.audit(r, "", "backup.upload", "backup", info.Name,
				map[string]any{"error": uerr.Error()}, false)
		} else {
			uploadMsg = "cadangan juga diunggah ke FTP"
			s.audit(r, "", "backup.upload", "backup", info.Name, nil, true)
		}
	}

	resp := map[string]any{"ok": true, "backup": info}
	if uploadMsg != "" {
		resp["upload"] = uploadMsg
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleBackupConfig menyimpan konfigurasi cadangan + FTP.
//
// POST /api/backup/config
func (s *Server) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	var req backupConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	cfg, err := backup.LoadConfig(r.Context(), s.store)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Schedule != nil {
		cfg.Schedule = strings.TrimSpace(*req.Schedule)
	}
	if req.KeepDays != nil {
		if *req.KeepDays < 0 {
			writeErr(w, http.StatusBadRequest, "keep_days tidak boleh negatif")
			return
		}
		cfg.KeepDays = *req.KeepDays
	}
	if req.FTP != nil {
		f := req.FTP
		if f.Enabled != nil {
			cfg.FTP.Enabled = *f.Enabled
		}
		if f.Host != nil {
			cfg.FTP.Host = strings.TrimSpace(*f.Host)
		}
		if f.Port != nil {
			if *f.Port < 0 || *f.Port > 65535 {
				writeErr(w, http.StatusBadRequest, "port FTP tidak valid")
				return
			}
			cfg.FTP.Port = *f.Port
		}
		if f.Username != nil {
			cfg.FTP.Username = strings.TrimSpace(*f.Username)
		}
		if f.Dir != nil {
			cfg.FTP.Dir = strings.TrimSpace(*f.Dir)
		}
		if f.Passive != nil {
			cfg.FTP.Passive = *f.Passive
		}
	}

	// Validasi jadwal cron bila diisi.
	if cfg.Enabled {
		if err := validateCron(cfg.Schedule); err != nil {
			writeErr(w, http.StatusBadRequest, "jadwal cron tidak valid: "+err.Error())
			return
		}
	}
	if cfg.FTP.Enabled && strings.TrimSpace(cfg.FTP.Host) == "" {
		writeErr(w, http.StatusBadRequest, "host FTP wajib diisi bila FTP diaktifkan")
		return
	}

	if err := backup.SaveConfig(r.Context(), s.store, cfg, req.FTPPassword, s.cfg.CredentialKey, currentUsername(r)); err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "backup.config", "settings", backup.SettingKey, map[string]any{
		"enabled": cfg.Enabled, "ftp_enabled": cfg.FTP.Enabled, "schedule": cfg.Schedule,
	}, true)

	// Kembalikan status terbaru agar UI sinkron.
	s.handleListBackups(w, r)
}

// handleDownloadBackup mengirim berkas cadangan sebagai unduhan.
//
// GET /api/backup/{name}
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	full, err := backup.Path(s.backupDir(), name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := os.Stat(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, "berkas cadangan tidak ditemukan")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+path.Base(name)+"\"")
	w.Header().Set("Content-Length", int64String(st.Size()))
	http.ServeFile(w, r, full)

	s.audit(r, "", "backup.download", "backup", name, nil, true)
}

// handleDeleteBackup menghapus sebuah berkas cadangan.
//
// DELETE /api/backup/{name}
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := backup.Delete(s.backupDir(), name); err != nil {
		if err == backup.ErrNoBackup {
			writeErr(w, http.StatusNotFound, "berkas cadangan tidak ditemukan")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "", "backup.delete", "backup", name, nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUploadBackup mengunggah berkas cadangan ke FTP sekarang.
//
// POST /api/backup/{name}/upload
func (s *Server) handleUploadBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, err := backup.Path(s.backupDir(), name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg, err := backup.LoadConfig(r.Context(), s.store)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !cfg.FTP.Enabled || strings.TrimSpace(cfg.FTP.Host) == "" {
		writeErr(w, http.StatusBadRequest, "FTP belum dikonfigurasi/aktif")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	if err := s.uploadBackup(ctx, cfg, name); err != nil {
		s.audit(r, "", "backup.upload", "backup", name, map[string]any{"error": err.Error()}, false)
		writeErr(w, http.StatusBadGateway, "unggah FTP gagal: "+err.Error())
		return
	}
	s.audit(r, "", "backup.upload", "backup", name, nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "berkas diunggah ke FTP"})
}

// handleRestoreBackup memulihkan database dari sebuah berkas cadangan.
//
// POST /api/backup/{name}/restore
//
// Aksi ini DESTRUKTIF: data saat ini pada tabel yang ada di cadangan akan
// ditimpa. Klien wajib mengirim {"confirm":"RESTORE"}.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	full, err := backup.Path(s.backupDir(), name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var req backupRestoreRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if strings.TrimSpace(req.Confirm) != "RESTORE" {
		writeErr(w, http.StatusBadRequest, `konfirmasi diperlukan: kirim {"confirm":"RESTORE"}`)
		return
	}
	if _, err := os.Stat(full); err != nil {
		writeErr(w, http.StatusNotFound, "berkas cadangan tidak ditemukan")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	if err := backup.Restore(ctx, s.cfg.DBURL, full); err != nil {
		s.audit(r, "", "backup.restore", "backup", name, map[string]any{"error": err.Error()}, false)
		writeErr(w, http.StatusInternalServerError, "pemulihan gagal: "+err.Error())
		return
	}
	s.audit(r, "", "backup.restore", "backup", name, map[string]any{"ok": true}, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Database dipulihkan dari cadangan. Muat ulang halaman dan login kembali bila sesi terputus.",
	})
}

// handleUploadBackupFile menerima unggahan berkas cadangan dari komputer
// (multipart field "file"). Bila form mengirim restore=true (dan confirm=RESTORE),
// berkas langsung dipulihkan setelah disimpan.
//
// POST /api/backup/upload
func (s *Server) handleUploadBackupFile(w http.ResponseWriter, r *http.Request) {
	// Batas ukuran unggahan: 2 GB (cadangan dapat berukuran besar).
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErr(w, http.StatusRequestEntityTooLarge, "berkas melebihi batas 2 GB")
			return
		}
		writeErr(w, http.StatusBadRequest, "form unggahan tidak valid: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "berkas tidak ditemukan pada field 'file'")
		return
	}
	defer file.Close()

	restoreNow := r.FormValue("restore") == "true"
	if restoreNow && strings.TrimSpace(r.FormValue("confirm")) != "RESTORE" {
		writeErr(w, http.StatusBadRequest, `konfirmasi diperlukan: kirim confirm="RESTORE"`)
		return
	}

	info, err := backup.SaveUpload(s.backupDir(), "upload", file, time.Now().UTC())
	if err != nil {
		if err == backup.ErrNotDump {
			writeErr(w, http.StatusBadRequest,
				"berkas bukan dump PostgreSQL (format -Fc). Pastikan mengunggah berkas .dump hasil fitur ini.")
			return
		}
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan berkas: "+err.Error())
		return
	}

	s.audit(r, "", "backup.upload_file", "backup", info.Name,
		map[string]any{"size": info.SizeBytes, "original": header.Filename, "restore": restoreNow}, true)

	resp := map[string]any{"ok": true, "backup": info}

	if restoreNow {
		full, perr := backup.DumpPath(s.backupDir(), info.Name)
		if perr != nil {
			writeErr(w, http.StatusInternalServerError, perr.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		if rerr := backup.Restore(ctx, s.cfg.DBURL, full); rerr != nil {
			s.audit(r, "", "backup.restore", "backup", info.Name, map[string]any{"error": rerr.Error()}, false)
			writeErr(w, http.StatusInternalServerError, "berkas tersimpan tetapi pemulihan gagal: "+rerr.Error())
			return
		}
		s.audit(r, "", "backup.restore", "backup", info.Name, map[string]any{"from_upload": true}, true)
		resp["restored"] = true
		resp["message"] = "Berkas diunggah dan database berhasil dipulihkan."
	} else {
		resp["message"] = "Berkas cadangan tersimpan di server."
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleTestFTP menguji koneksi FTP memakai konfigurasi tersimpan.
//
// POST /api/backup/ftp/test
func (s *Server) handleTestFTP(w http.ResponseWriter, r *http.Request) {
	cfg, err := backup.LoadConfig(r.Context(), s.store)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if strings.TrimSpace(cfg.FTP.Host) == "" {
		writeErr(w, http.StatusBadRequest, "host FTP belum diisi")
		return
	}

	ftpCfg, err := cfg.FTPConfig(s.store, s.cfg.CredentialKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- backup.TestConnection(ftpCfg) }()
	select {
	case err := <-errCh:
		if err != nil {
			s.audit(r, "", "backup.ftp_test", "settings", backup.SettingKey, map[string]any{"error": err.Error()}, false)
			writeErr(w, http.StatusBadGateway, "koneksi FTP gagal: "+err.Error())
			return
		}
	case <-ctx.Done():
		writeErr(w, http.StatusGatewayTimeout, "koneksi FTP timeout")
		return
	}

	s.audit(r, "", "backup.ftp_test", "settings", backup.SettingKey, nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "koneksi FTP berhasil"})
}

// uploadBackup mengunggah berkas cadangan ke FTP dan memperbarui status.
func (s *Server) uploadBackup(ctx context.Context, cfg *backup.Config, name string) error {
	full, err := backup.Path(s.backupDir(), name)
	if err != nil {
		return err
	}
	ftpCfg, err := cfg.FTPConfig(s.store, s.cfg.CredentialKey)
	if err != nil {
		_ = backup.TouchRun(context.Background(), s.store, "error", err.Error(), name, time.Now().UTC().Format(time.RFC3339), "")
		return err
	}
	if err := backup.UploadFile(ftpCfg, full, name); err != nil {
		_ = backup.TouchRun(context.Background(), s.store, "error", err.Error(), name, time.Now().UTC().Format(time.RFC3339), "")
		return err
	}
	return backup.TouchRun(context.Background(), s.store, "ok", "", name, time.Now().UTC().Format(time.RFC3339), "")
}

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type backupConfigRequest struct {
	Enabled     *bool   `json:"enabled"`
	Schedule    *string `json:"schedule"`
	KeepDays    *int    `json:"keep_days"`
	FTPPassword *string `json:"ftp_password"`
	FTP         *struct {
		Enabled *bool   `json:"enabled"`
		Host    *string `json:"host"`
		Port    *int    `json:"port"`
		Username *string `json:"username"`
		Dir     *string `json:"dir"`
		Passive *bool   `json:"passive"`
	} `json:"ftp"`
}

type backupRestoreRequest struct {
	Confirm string `json:"confirm"`
}
