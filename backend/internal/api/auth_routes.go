package api

import (
	"errors"
	"log"
	"net/http"

	"ingatin/backend/internal/auth"
	"ingatin/backend/internal/repository"
)

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type sessionInfo struct {
	ID        string `json:"id"`
	UserAgent string `json:"user_agent"`
	IP        string `json:"ip"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleLogin memverifikasi kredensial dan membuat sesi.
//
// POST /api/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "username dan password wajib diisi")
		return
	}

	ip := s.clientIP(r)
	ua := r.UserAgent()

	session, err := s.auth.Login(r.Context(), req.Username, req.Password, ua, ip)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			s.audit(r, req.Username, "auth.login", "user", "", map[string]any{
				"reason": "kredensial_salah",
			}, false)
			writeErr(w, http.StatusUnauthorized, "username atau password salah")
		case errors.Is(err, auth.ErrUserInactive):
			s.audit(r, req.Username, "auth.login", "user", "", map[string]any{
				"reason": "akun_nonaktif",
			}, false)
			writeErr(w, http.StatusForbidden, "akun tidak aktif")
		default:
			writeInternalError(w, err)
		}
		return
	}

	s.audit(r, req.Username, "auth.login", "user", session.User.ID.String(), nil, true)
	writeJSON(w, http.StatusOK, session)
}

// handleRefresh menukar refresh token menjadi sesi baru (rotasi).
//
// POST /api/auth/refresh
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "refresh_token wajib diisi")
		return
	}

	session, err := s.auth.Refresh(r.Context(), req.RefreshToken, r.UserAgent(), s.clientIP(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTokenRevoked):
			// Token yang sudah dicabut dipakai lagi: seluruh sesi telah diamankan.
			s.audit(r, "", "auth.refresh_reuse", "user", "", map[string]any{
				"reason": "token_dicabut_dipakai_ulang",
			}, false)
			writeErr(w, http.StatusUnauthorized, "sesi sudah dicabut; seluruh sesi diamankan, silakan login ulang")
		case errors.Is(err, auth.ErrTokenExpired):
			writeErr(w, http.StatusUnauthorized, "refresh token kedaluwarsa")
		case errors.Is(err, auth.ErrUserInactive):
			writeErr(w, http.StatusForbidden, "akun tidak aktif")
		case errors.Is(err, auth.ErrInvalidToken):
			writeErr(w, http.StatusUnauthorized, "refresh token tidak valid")
		default:
			writeInternalError(w, err)
		}
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// handleLogout mencabut refresh token yang diberikan.
//
// POST /api/auth/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "refresh_token wajib diisi")
		return
	}
	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "auth.logout", "user", "", nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMe mengembalikan profil user yang sedang login.
//
// GET /api/auth/me
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
		return
	}

	// F22: sertakan izin efektif + penanda super user agar UI dapat menyembunyikan
	// menu/aksi yang tidak diizinkan.
	perms := s.store.EffectivePermissions(r.Context(), user.Role)

	resp := map[string]any{
		"id":          user.ID,
		"username":    user.Username,
		"email":       user.Email,
		"full_name":   user.FullName,
		"role":        user.Role,
		"is_active":   user.IsActive,
		"created_at":  user.CreatedAt,
		"permissions": perms,
		"is_super":    s.store.RoleIsSuper(r.Context(), user.Role),
	}
	if user.TeamID != nil {
		resp["team_id"] = user.TeamID
	}
	if user.OrganizationID != nil {
		resp["organization_id"] = user.OrganizationID
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleChangePassword mengganti password lalu mencabut seluruh sesi.
//
// POST /api/auth/change-password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
		return
	}

	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "password lama dan baru wajib diisi")
		return
	}
	if len(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "password baru minimal 8 karakter")
		return
	}

	if err := s.auth.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.audit(r, user.Username, "auth.change_password", "user", user.ID.String(), map[string]any{
				"reason": "password_lama_salah",
			}, false)
			writeErr(w, http.StatusUnauthorized, "password lama salah")
			return
		}
		writeInternalError(w, err)
		return
	}

	s.audit(r, user.Username, "auth.change_password", "user", user.ID.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "password diganti; seluruh sesi dicabut, silakan login ulang",
	})
}

// handleListSessions menampilkan sesi aktif user.
//
// GET /api/auth/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
		return
	}

	sessions, err := s.auth.ActiveSessions(r.Context(), user.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	out := make([]sessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sessionInfo{
			ID:        sess.ID.String(),
			UserAgent: sess.UserAgent,
			IP:        sess.IP,
			ExpiresAt: sess.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "total": len(out)})
}

// handleLogoutAll mencabut seluruh sesi user.
//
// POST /api/auth/logout-all
func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
		return
	}
	if err := s.auth.LogoutAll(r.Context(), user.ID); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, user.Username, "auth.logout_all", "user", user.ID.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "seluruh sesi dicabut",
	})
}

// ---------------------------------------------------------------------------
// Audit helper
// ---------------------------------------------------------------------------

// audit mencatat aksi ke audit_logs.
//
// Kegagalan pencatatan audit tidak boleh menggagalkan permintaan pengguna,
// jadi error hanya dicatat ke log server.
func (s *Server) audit(r *http.Request, username, action, entityType, entityID string, detail map[string]any, success bool) {
	if username == "" {
		if u := userFrom(r); u != nil {
			username = u.Username
		}
	}
	err := s.store.CreateAuditLog(r.Context(), repository.AuditLogParams{
		Username:   username,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Detail:     detail,
		IP:         s.clientIP(r),
		Success:    success,
	})
	if err != nil {
		log.Printf("api: gagal mencatat audit action=%s: %v", action, err)
	}
}
