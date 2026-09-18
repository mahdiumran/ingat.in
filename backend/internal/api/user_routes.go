package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type userCreateRequest struct {
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	FullName       string  `json:"full_name"`
	Password       string  `json:"password"`
	Role           string  `json:"role"`
	TeamID         *string `json:"team_id"`
	OrganizationID *string `json:"organization_id"`
	TelegramChatID string  `json:"telegram_chat_id"`
	WANumber       string  `json:"wa_number"`
}

type userUpdateRequest struct {
	Email          *string `json:"email"`
	FullName       *string `json:"full_name"`
	Role           *string `json:"role"`
	TeamID         *string `json:"team_id"`
	OrganizationID *string `json:"organization_id"`
	TelegramChatID *string `json:"telegram_chat_id"`
	WANumber       *string `json:"wa_number"`
	IsActive       *bool   `json:"is_active"`
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

type teamCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type teamUpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}

// ---------------------------------------------------------------------------
// Validasi
// ---------------------------------------------------------------------------

var validRoles = map[string]bool{
	models.RoleAdmin:    true,
	models.RoleAgent:    true,
	models.RoleNOC:      true,
	models.RoleSales:    true,
	models.RoleViewer:   true,
	models.RoleCustomer: true,
}

// parseOptionalUUID mengubah string kosong menjadi nil.
func parseOptionalUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// handleListUsers mengembalikan daftar user (admin).
//
// GET /api/users
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "total": len(users)})
}

// handleCreateUser membuat user baru (admin).
//
// POST /api/users
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
		writeErr(w, http.StatusBadRequest, "username tidak valid (3-64 karakter: huruf, angka, _ - .)")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password minimal 8 karakter")
		return
	}
	if !validRoles[req.Role] {
		writeErr(w, http.StatusBadRequest, "role tidak dikenal")
		return
	}

	teamID, err := parseOptionalUUID(req.TeamID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "team_id tidak valid")
		return
	}
	orgID, err := parseOptionalUUID(req.OrganizationID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "organization_id tidak valid")
		return
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	user, err := s.store.CreateUser(r.Context(), repository.CreateUserParams{
		Username:       req.Username,
		Email:          req.Email,
		FullName:       req.FullName,
		PasswordHash:   hash,
		Role:           req.Role,
		TeamID:         teamID,
		OrganizationID: orgID,
		TelegramChatID: req.TelegramChatID,
		WANumber:       req.WANumber,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "user.create", "user", user.ID.String(), map[string]any{
		"username": user.Username,
		"role":     user.Role,
	}, true)
	writeJSON(w, http.StatusCreated, user)
}

// handleUpdateUser memperbarui user (admin).
//
// PATCH /api/users/{id}
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id user tidak valid")
		return
	}

	var req userUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Role != nil && !validRoles[*req.Role] {
		writeErr(w, http.StatusBadRequest, "role tidak dikenal")
		return
	}

	params := repository.UpdateUserParams{
		Email:          req.Email,
		FullName:       req.FullName,
		Role:           req.Role,
		TelegramChatID: req.TelegramChatID,
		WANumber:       req.WANumber,
		IsActive:       req.IsActive,
	}

	if req.TeamID != nil {
		tid, err := parseOptionalUUID(req.TeamID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "team_id tidak valid")
			return
		}
		params.TeamID = tid
	}
	if req.OrganizationID != nil {
		oid, err := parseOptionalUUID(req.OrganizationID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "organization_id tidak valid")
			return
		}
		params.OrganizationID = oid
	}

	// Cegah admin menurunkan/menonaktifkan dirinya sendiri sehingga sistem
	// tidak pernah kehilangan administrator terakhir.
	current := userFrom(r)
	if current != nil && current.ID == id {
		if req.Role != nil && *req.Role != models.RoleAdmin {
			writeErr(w, http.StatusBadRequest, "tidak dapat mengubah role akun sendiri")
			return
		}
		if req.IsActive != nil && !*req.IsActive {
			writeErr(w, http.StatusBadRequest, "tidak dapat menonaktifkan akun sendiri")
			return
		}
	}

	user, err := s.store.UpdateUser(r.Context(), id, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "user tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	// Bila role/status berubah, cabut sesi lama agar perubahan berlaku segera.
	if req.Role != nil || (req.IsActive != nil && !*req.IsActive) {
		if err := s.auth.LogoutAll(r.Context(), id); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	s.audit(r, "", "user.update", "user", id.String(), nil, true)
	writeJSON(w, http.StatusOK, user)
}

// handleResetUserPassword mereset password user (admin).
//
// POST /api/users/{id}/reset-password
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id user tidak valid")
		return
	}

	var req resetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "password minimal 8 karakter")
		return
	}

	hash, err := crypto.HashPassword(req.NewPassword)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := s.store.UpdateUserPassword(r.Context(), id, hash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "user tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := s.auth.LogoutAll(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "user.reset_password", "user", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "password direset; seluruh sesi user dicabut"})
}

// handleDeleteUser menghapus user (admin).
//
// DELETE /api/users/{id}
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id user tidak valid")
		return
	}

	if current := userFrom(r); current != nil && current.ID == id {
		writeErr(w, http.StatusBadRequest, "tidak dapat menghapus akun sendiri")
		return
	}

	// Pastikan selalu ada minimal satu admin aktif.
	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "user tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	if target.Role == models.RoleAdmin {
		counts, err := s.store.CountUsersByRole(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if counts[models.RoleAdmin] <= 1 {
			writeErr(w, http.StatusBadRequest, "tidak dapat menghapus administrator terakhir")
			return
		}
	}

	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "user tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "user.delete", "user", id.String(), map[string]any{
		"username": target.Username,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Tim & organisasi
// ---------------------------------------------------------------------------

// handleListTeams mengembalikan daftar tim.
//
// GET /api/teams
func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.ListTeams(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams, "total": len(teams)})
}

// handleCreateTeam membuat tim (admin).
//
// POST /api/teams
func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req teamCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "nama tim wajib diisi")
		return
	}

	team, err := s.store.CreateTeam(r.Context(), req.Name, req.Description)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "team.create", "team", team.ID.String(), map[string]any{"name": team.Name}, true)
	writeJSON(w, http.StatusCreated, team)
}

// handleUpdateTeam memperbarui tim (admin).
//
// PATCH /api/teams/{id}
func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tim tidak valid")
		return
	}
	var req teamUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	team, err := s.store.UpdateTeam(r.Context(), id, req.Name, req.Description, req.IsActive)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "tim tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "team.update", "team", id.String(), nil, true)
	writeJSON(w, http.StatusOK, team)
}

// handleDeleteTeam menghapus tim (admin).
//
// DELETE /api/teams/{id}
func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tim tidak valid")
		return
	}

	hasMembers, err := s.store.TeamHasMembers(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if hasMembers {
		writeErr(w, http.StatusConflict, "tim masih memiliki anggota; pindahkan anggota terlebih dahulu")
		return
	}

	if err := s.store.DeleteTeam(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "tim tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "team.delete", "team", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleListOrganizations mengembalikan daftar organisasi.
//
// GET /api/organizations
func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.store.ListOrganizations(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organizations": orgs, "total": len(orgs)})
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

// handleListAudit mengembalikan audit trail (admin).
//
// GET /api/audit?username=&action=&entity_type=&success=&from=&to=&limit=&offset=
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := repository.ListAuditLogsParams{
		Username:   q.Get("username"),
		Action:     q.Get("action"),
		EntityType: q.Get("entity_type"),
		Limit:      50,
	}

	if raw := q.Get("success"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "parameter success harus true/false")
			return
		}
		params.Success = &v
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > 500 {
			writeErr(w, http.StatusBadRequest, "parameter limit harus 1..500")
			return
		}
		params.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeErr(w, http.StatusBadRequest, "parameter offset tidak valid")
			return
		}
		params.Offset = v
	}
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "parameter from harus format RFC3339")
			return
		}
		params.From = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "parameter to harus format RFC3339")
			return
		}
		params.To = &t
	}

	logs, total, err := s.store.ListAuditLogs(r.Context(), params)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total})
}

/* ---------------------------------------------------------------------------
   Outbox (riwayat pengiriman notifikasi)
   --------------------------------------------------------------------------- */

// handleListOutbox mengembalikan riwayat pengiriman (admin).
//
// GET /api/outbox?status=&channel=&limit=&offset=
func (s *Server) handleListOutbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := repository.ListOutboxParams{
		Status:  q.Get("status"),
		Channel: q.Get("channel"),
		Limit:   50,
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > 500 {
			writeErr(w, http.StatusBadRequest, "parameter limit harus 1..500")
			return
		}
		params.Limit = v
	}
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeErr(w, http.StatusBadRequest, "parameter offset tidak valid")
			return
		}
		params.Offset = v
	}

	rows, total, err := s.store.ListOutbox(r.Context(), params)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	byStatus, err := s.store.CountOutboxByStatus(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outbox":    rows,
		"total":     total,
		"by_status": byStatus,
	})
}
