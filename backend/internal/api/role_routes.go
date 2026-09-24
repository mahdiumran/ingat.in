package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   F22 — Peran dinamis (roles)
   --------------------------------------------------------------------------- */

type roleCreateRequest struct {
	Role        string `json:"role"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Rank        *int   `json:"rank"`
}

type roleUpdateRequest struct {
	Label       *string `json:"label"`
	Description *string `json:"description"`
	Rank        *int    `json:"rank"`
}

// normalizeRole memaksa slug peran ke huruf kecil, angka, underscore.
func normalizeRole(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		case r == '_' || r == '-' || r == ' ':
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func validRoleSlug(s string) bool {
	if len(s) < 2 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

// handleListRoles mengembalikan daftar peran dinamis.
//
// GET /api/roles
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.ListRoles(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles, "total": len(roles)})
}

// handleCreateRole menambah peran baru.
//
// POST /api/roles
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req roleCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role := normalizeRole(req.Role)
	if !validRoleSlug(role) {
		writeErr(w, http.StatusBadRequest, "slug role tidak valid (2-40: huruf kecil, angka, underscore)")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = role
	}
	if exists, _ := s.store.RoleExists(r.Context(), role); exists {
		writeErr(w, http.StatusConflict, "role "+role+" sudah ada")
		return
	}
	rank := 100
	if req.Rank != nil {
		rank = *req.Rank
	}

	def, err := s.store.CreateRole(r.Context(), role, label, strings.TrimSpace(req.Description), rank)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "role.create", "role", role, map[string]any{"label": label}, true)
	writeJSON(w, http.StatusCreated, def)
}

// handleUpdateRole memperbarui label/deskripsi/rank peran.
//
// PATCH /api/roles/{role}
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	role := chi.URLParam(r, "role")
	if !s.isKnownRole(r.Context(), role) {
		writeErr(w, http.StatusNotFound, "role tidak ditemukan")
		return
	}
	var req roleUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	def, err := s.store.UpdateRole(r.Context(), role, trimPtr(req.Label), req.Description, req.Rank)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "role tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "role.update", "role", role, nil, true)
	writeJSON(w, http.StatusOK, def)
}

// handleDeleteRole menghapus peran dinamis.
//
// DELETE /api/roles/{role}
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	role := chi.URLParam(r, "role")

	def, err := s.store.GetRole(r.Context(), role)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "role tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	if def.IsSuper || def.IsSystem {
		writeErr(w, http.StatusBadRequest, "peran bawaan tidak dapat dihapus")
		return
	}

	n, err := s.store.CountUsersByRoleName(r.Context(), role)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "masih ada "+strconv.Itoa(n)+" pengguna dengan peran ini; pindahkan terlebih dahulu")
		return
	}

	if err := s.store.DeleteRole(r.Context(), role); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "role tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "role.delete", "role", role, nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
