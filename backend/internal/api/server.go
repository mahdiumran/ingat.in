// Package api menyusun HTTP API (chi router).
//
// Konvensi (diadopsi dari mcnvpn/internal/api):
//   - Server struct menampung dependency; handler adalah method pada *Server
//   - middleware: RequestID -> Recoverer -> RealIP -> Timeout -> securityHeaders -> cors
//   - respons error selalu berbentuk {"error":"pesan"}
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"ingatin/backend/internal/attachments"
	"ingatin/backend/internal/auth"
	"ingatin/backend/internal/bot"
	"ingatin/backend/internal/config"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
)

// Server menampung seluruh dependency HTTP.
type Server struct {
	cfg         *config.Config
	store       *repository.Store
	auth        *auth.Manager
	notifier    *notify.Resolver
	sheets      *sheets.Queue
	attachments *attachments.Store
	bot         *bot.Dispatcher
	version     string
}

// New membuat Server baru.
func New(cfg *config.Config, store *repository.Store, authMgr *auth.Manager, notifier *notify.Resolver, sheetQueue *sheets.Queue, attachStore *attachments.Store, botDisp *bot.Dispatcher) *Server {
	return &Server{
		cfg:         cfg,
		store:       store,
		auth:        authMgr,
		notifier:    notifier,
		sheets:      sheetQueue,
		attachments: attachStore,
		bot:         botDisp,
		version:     cfg.AppVersion,
	}
}

// Handler membangun router beserta middleware.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(s.securityHeaders)
	r.Use(s.cors)

	r.Route("/api", func(api chi.Router) {
		// ---------------------------------------------------------------
		// Publik
		// ---------------------------------------------------------------
		api.Get("/health", s.handleHealth)
		api.Get("/version", s.handleVersion)

		api.Post("/auth/login", s.handleLogin)
		api.Post("/auth/refresh", s.handleRefresh)
		api.Post("/auth/logout", s.handleLogout)

		// F12: webhook Telegram command bot (publik, diamankan secret).
		// Hanya didaftarkan bila bot dikonfigurasi (secret terisi).
		s.registerTelegramHookRoute(api)

		// ---------------------------------------------------------------
		// Terautentikasi
		// ---------------------------------------------------------------
		api.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)

			pr.Get("/auth/me", s.handleMe)
			pr.Post("/auth/change-password", s.handleChangePassword)
			pr.Get("/auth/sessions", s.handleListSessions)
			pr.Post("/auth/logout-all", s.handleLogoutAll)

			// Referensi umum (semua role boleh membaca)
			pr.Get("/teams", s.handleListTeams)
			pr.Get("/organizations", s.handleListOrganizations)

			// F4: notifikasi. Baca Notification Center (providers/targets/templates)
			// dibatasi izin providers.view (F32). Escalation Policies tetap terbuka.
			// Daftar target ringan (id+nama) untuk dropdown form tetap terbuka.
			pr.Get("/targets/options", s.handleListTargetOptions)
			pr.Get("/policies", s.handleListPolicies)

			pr.Group(func(nv chi.Router) {
				nv.Use(s.requirePermission("providers.view"))
				nv.Get("/providers", s.handleListProviders)
				nv.Get("/targets", s.handleListTargets)
				nv.Get("/templates", s.handleListTemplates)
			})

			// F5–F7: work items
			pr.Get("/items", s.handleListWorkItems)
			pr.Post("/items", s.handleCreateWorkItem)
			pr.Get("/items/{id}", s.handleGetWorkItem)
			pr.Patch("/items/{id}", s.handleUpdateWorkItem)
			pr.Delete("/items/{id}", s.handleDeleteWorkItem)
			pr.Post("/items/{id}/status", s.handleChangeStatus)
			pr.Post("/items/{id}/notify", s.handleTriggerNotification)
			pr.Get("/items/{id}/events", s.handleListEvents)
			pr.Post("/items/{id}/comments", s.handleAddComment)

			// F20: assign & penangan tambahan.
			pr.Post("/items/{id}/assign", s.handleAssignItem)
			pr.Post("/items/{id}/collaborators", s.handleAddCollaborator)
			pr.Delete("/items/{id}/collaborators/{username}", s.handleRemoveCollaborator)

			// F23: Aktivasi/EWO (RFS) — cancel & delete ber-alasan.
			pr.Post("/items/{id}/rfs-cancel", s.handleRFSCancel)
			pr.Post("/items/{id}/rfs-delete", s.handleRFSDelete)

			// F24: eskalasi Daily Task "bermasalah" menjadi tiket.
			pr.Post("/items/{id}/escalate-ticket", s.handleEscalateTicket)

			// F21: lampiran pendukung.
			pr.Get("/items/{id}/attachments", s.handleListAttachments)
			pr.Post("/items/{id}/attachments", s.handleUploadAttachment)
			pr.Get("/attachments/{id}", s.handleDownloadAttachment)
			pr.Delete("/attachments/{id}", s.handleDeleteAttachment)

			// F20/F22: KPI & SLA — khusus peran manajerial (izin kpi.view).
			// Admin (super user) selalu lolos. Lihat grup izin di bawah.

			pr.Get("/dashboard", s.handleDashboard)

			// Feed notifikasi untuk lonceng sidebar.
			pr.Get("/notifications", s.handleListNotifications)
			pr.Post("/notifications/read", s.handleMarkNotificationsRead)

			// F22: konfigurasi ringkasan tugas harian (baca untuk semua; tulis admin).
			pr.Get("/settings/daily-summary", s.handleGetDailySummarySettings)

			// F12: master data (baca untuk semua; tulis dijaga izin masterdata.write)
			pr.Get("/master-data", s.handleListMasterData)

			// F26: catatan (sticky notes). Baca dijaga izin notes.view;
			// tambah/ubah/hapus dijaga izin notes.write (grup di bawah).
			pr.With(s.requirePermission("notes.view")).Get("/notes", s.handleListNotes)
			pr.With(s.requirePermission("notes.view")).Get("/notes/{id}", s.handleGetNote)

			pr.Group(func(nr chi.Router) {
				nr.Use(s.requirePermission("notes.write"))
				nr.Post("/notes", s.handleCreateNote)
				nr.Patch("/notes/{id}", s.handleUpdateNote)
				nr.Patch("/notes/{id}/shares", s.handleSetNoteShares)
				nr.Delete("/notes/{id}", s.handleDeleteNote)
			})
		})

		// ---------------------------------------------------------------
		// Admin
		// ---------------------------------------------------------------
		// ---------------------------------------------------------------
		// Manajemen pengguna & tim — izin pengguna.write / tim.write.
		// Admin (super user) selalu lolos; manager yang diberi izin juga.
		// ---------------------------------------------------------------
		api.Group(func(ur chi.Router) {
			ur.Use(s.requireAuth)
			ur.Use(s.requirePermission("users.write"))
			ur.Get("/users", s.handleListUsers)
			ur.Post("/users", s.handleCreateUser)
			ur.Patch("/users/{id}", s.handleUpdateUser)
			ur.Post("/users/{id}/reset-password", s.handleResetUserPassword)
			ur.Delete("/users/{id}", s.handleDeleteUser)
		})
		api.Group(func(tr chi.Router) {
			tr.Use(s.requireAuth)
			tr.Use(s.requirePermission("teams.write"))
			tr.Post("/teams", s.handleCreateTeam)
			tr.Patch("/teams/{id}", s.handleUpdateTeam)
			tr.Delete("/teams/{id}", s.handleDeleteTeam)
		})

		api.Group(func(ar chi.Router) {
			ar.Use(s.requireAuth)
			ar.Use(s.requireAdmin)

			// Force status (admin saja) — koreksi darurat tanpa aturan transisi.
			ar.Post("/items/{id}/force-status", s.handleForceStatus)

			// F20: force unlock (buka tiket closed/completed, tanpa aturan transisi).
			ar.Post("/items/{id}/force-unlock", s.handleForceUnlock)

			ar.Get("/audit", s.handleListAudit)
			ar.Get("/outbox", s.handleListOutbox)

			// F22: ringkasan tugas harian (tulis + preview uji).
			ar.Post("/settings/daily-summary", s.handleSetDailySummarySettings)
			ar.Post("/settings/daily-summary/preview", s.handlePreviewDailySummary)

			// F3/F4: konfigurasi notifikasi (admin & noc)
			ar.Post("/providers", s.handleCreateProvider)
			ar.Patch("/providers/{id}", s.handleUpdateProvider)
			ar.Delete("/providers/{id}", s.handleDeleteProvider)
			ar.Post("/providers/{id}/test", s.handleTestProvider)
			ar.Get("/providers/{id}/session", s.handleProviderSessionInfo)
			ar.Post("/providers/{id}/session/{action}", s.handleProviderSessionAction)
			ar.Post("/providers/{id}/send-test", s.handleSendTestMessage)

			ar.Post("/targets", s.handleCreateTarget)
			ar.Patch("/targets/{id}", s.handleUpdateTarget)
			ar.Delete("/targets/{id}", s.handleDeleteTarget)
			ar.Post("/targets/{id}/bindings", s.handleCreateBinding)
			ar.Patch("/bindings/{id}", s.handleUpdateBinding)
			ar.Delete("/bindings/{id}", s.handleDeleteBinding)
			ar.Post("/bindings/{id}/test", s.handleTestBinding)

			ar.Patch("/templates/{id}", s.handleUpdateTemplate)
			ar.Post("/policies", s.handleCreatePolicy)
			ar.Patch("/policies/{id}", s.handleUpdatePolicy)
			ar.Delete("/policies/{id}", s.handleDeletePolicy)

			// F18: sinkronisasi spreadsheet (admin only).
			ar.Get("/sheet-sync", s.handleGetSheetSync)
			ar.Post("/sheet-sync", s.handleSaveSheetSync)
			ar.Post("/sheet-sync/test", s.handleTestSheetSync)
			ar.Post("/sheet-sync/create-tab", s.handleCreateSheetTab)

			// F34: cadangan & pemulihan database + unggah FTP (admin only).
			ar.Get("/backup", s.handleListBackups)
			ar.Post("/backup", s.handleCreateBackup)
			ar.Post("/backup/config", s.handleBackupConfig)
			ar.Post("/backup/upload", s.handleUploadBackupFile)
			ar.Post("/backup/ftp/test", s.handleTestFTP)
			ar.Get("/backup/{name}", s.handleDownloadBackup)
			ar.Delete("/backup/{name}", s.handleDeleteBackup)
			ar.Post("/backup/{name}/upload", s.handleUploadBackup)
			ar.Post("/backup/{name}/restore", s.handleRestoreBackup)
		})

		// ---------------------------------------------------------------
		// Izin berbasis matriks (Edit Role Permission) — F12
		//
		// Grup ini hanya menuntut autentikasi; apakah role tertentu boleh
		// menulis ditentukan middleware requirePermission yang membaca
		// matriks role_permissions (admin selalu boleh).
		// ---------------------------------------------------------------
		api.Group(func(mw chi.Router) {
			mw.Use(s.requireAuth)
			mw.Use(s.requirePermission("masterdata.write"))

			mw.Post("/master-data", s.handleCreateMasterData)
			mw.Patch("/master-data/{id}", s.handleUpdateMasterData)
			mw.Delete("/master-data/{id}", s.handleDeleteMasterData)
			mw.Post("/master-data/kinds", s.handleCreateMasterDataKind)
		})

		api.Group(func(rp chi.Router) {
			rp.Use(s.requireAuth)
			rp.Use(s.requireAdmin)

			rp.Get("/role-permissions", s.handleListRolePermissions)
			rp.Post("/role-permissions", s.handleSetRolePermission)
		})

		// ---------------------------------------------------------------
		// F22: peran dinamis (roles)
		//
		// Baca untuk semua yang terautentikasi; tulis dijaga izin roles.write.
		// ---------------------------------------------------------------
		api.Group(func(rr chi.Router) {
			rr.Use(s.requireAuth)
			rr.Get("/roles", s.handleListRoles)
		})
		api.Group(func(rw chi.Router) {
			rw.Use(s.requireAuth)
			rw.Use(s.requirePermission("roles.write"))
			rw.Post("/roles", s.handleCreateRole)
			rw.Patch("/roles/{role}", s.handleUpdateRole)
			rw.Delete("/roles/{role}", s.handleDeleteRole)
		})

		// ---------------------------------------------------------------
		// F22: KPI & SLA (izin kpi.view — manajerial + super user)
		// ---------------------------------------------------------------
		api.Group(func(kp chi.Router) {
			kp.Use(s.requireAuth)
			kp.Use(s.requirePermission("kpi.view"))
			kp.Get("/kpi/users", s.handleKPIUsers)
			kp.Get("/kpi/sla", s.handleKPI)
			kp.Get("/kpi/sla/export.csv", s.handleKPIExportCSV)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "endpoint tidak ditemukan")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusMethodNotAllowed, "method tidak diizinkan")
	})

	return r
}

// ---------------------------------------------------------------------------
// Handlers dasar
// ---------------------------------------------------------------------------

// handleHealth memverifikasi proses dan koneksi database.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dbOK := true
	dbErr := ""
	if err := s.store.Ping(ctx); err != nil {
		dbOK = false
		dbErr = err.Error()
	}

	status := http.StatusOK
	if !dbOK {
		status = http.StatusServiceUnavailable
	}
	resp := map[string]any{
		"status":   map[bool]string{true: "ok", false: "degraded"}[dbOK],
		"app":      s.cfg.AppName,
		"version":  s.version,
		"mode":     string(s.cfg.Mode),
		"database": map[bool]string{true: "ok", false: "error"}[dbOK],
		"time":     time.Now().UTC().Format(time.RFC3339),
	}
	if dbErr != "" {
		resp["database_error"] = dbErr
	}
	writeJSON(w, status, resp)
}

// handleVersion mengembalikan informasi versi aplikasi dan skema.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	schema := int64(0)
	if v, err := s.store.SchemaVersionSafe(ctx); err == nil {
		schema = v
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"app":                  s.cfg.AppName,
		"version":              s.version,
		"mode":                 string(s.cfg.Mode),
		"schema_version":       schema,
		"timezone":             s.cfg.Timezone,
		"access_token_minutes": s.cfg.AccessTokenMinutes,
		"refresh_token_days":   s.cfg.RefreshTokenDays,
	})
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		next.ServeHTTP(w, r)
	})
}

// cors mengizinkan origin frontend yang dikonfigurasi.
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	if s.cfg.FrontendOrigin != "" {
		allowed[s.cfg.FrontendOrigin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowed[origin] || s.cfg.FrontendOrigin == "*") {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Helper respons & decoding
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: tulis respons json: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("api: internal error: %v", err)
	writeErr(w, http.StatusInternalServerError, "terjadi kesalahan internal")
}

// decodeJSON membaca body JSON ke dst dengan batas ukuran dan penolakan field
// yang tidak dikenal, sehingga kesalahan ketik pada klien cepat terdeteksi.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "body JSON tidak valid: "+err.Error())
		return false
	}
	return true
}

// Pool mengembalikan pool database (dipakai pengecekan di test).
func (s *Server) Pool() *pgxpool.Pool { return s.store.Pool() }
