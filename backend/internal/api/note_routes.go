package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// errInvalid membuat kesalahan validasi catatan.
func errInvalid(msg string) error { return fmt.Errorf("%s: %w", msg, errValidation) }

/* ---------------------------------------------------------------------------
   F26 — Catatan (sticky notes)
   --------------------------------------------------------------------------- */

type noteCreateRequest struct {
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Visibility  string   `json:"visibility"`
	OwnerTeamID *string  `json:"owner_team_id"`
	Color       string   `json:"color"`
	Pinned      bool     `json:"pinned"`
	SharedTeams []string `json:"shared_team_ids"`
}

type noteUpdateRequest struct {
	Title       *string  `json:"title"`
	Body        *string  `json:"body"`
	Visibility  *string  `json:"visibility"`
	OwnerTeamID *string  `json:"owner_team_id"`
	Color       *string  `json:"color"`
	Pinned      *bool    `json:"pinned"`
	SharedTeams []string `json:"shared_team_ids"`
}

type noteSharesRequest struct {
	TeamIDs []string `json:"team_ids"`
}

// handleListNotes mengembalikan catatan yang ter-scope untuk tim pengguna.
//
// GET /api/notes?visibility=&q=&limit=
func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := repository.ListNotesParams{
		Visibility: strings.TrimSpace(q.Get("visibility")),
		Search:     strings.TrimSpace(q.Get("q")),
		Limit:      200,
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			params.Limit = n
		}
	}

	// Scope tim: admin/super melihat semua; selain itu dibatasi ke tim pengguna.
	user := userFrom(r)
	if !s.isSuper(r, user) && user != nil && user.TeamID != nil {
		params.TeamScope = user.TeamID
	}

	notes, err := s.store.ListNotes(r.Context(), params)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes, "total": len(notes)})
}

// handleGetNote mengembalikan satu catatan.
//
// GET /api/notes/{id}
func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	n, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		s.noteError(w, err)
		return
	}
	if !s.canViewNote(r, userFrom(r), n) {
		writeErr(w, http.StatusForbidden, "tidak berhak melihat catatan ini")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// handleCreateNote membuat catatan baru.
//
// POST /api/notes
func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req noteCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	if req.Title == "" && req.Body == "" {
		writeErr(w, http.StatusBadRequest, "judul atau isi catatan wajib diisi")
		return
	}
	vis := strings.TrimSpace(req.Visibility)
	if vis == "" {
		vis = models.NoteVisibilityInternal
	}
	if !validNoteVisibility(vis) {
		writeErr(w, http.StatusBadRequest, "visibilitas harus 'internal' atau 'eksternal'")
		return
	}

	user := userFrom(r)
	// Tim pemilik default = tim pengguna; hanya admin yang boleh memilih tim lain.
	ownerTeam, err := s.resolveNoteOwnerTeam(r, user, req.OwnerTeamID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := currentUsername(r)
	n, err := s.store.CreateNote(r.Context(), repository.CreateNoteParams{
		Title:       req.Title,
		Body:        req.Body,
		Visibility:  vis,
		OwnerTeamID: ownerTeam,
		Color:       strings.TrimSpace(req.Color),
		Pinned:      req.Pinned,
		CreatedBy:   actor,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if len(req.SharedTeams) > 0 {
		ids, perr := parseUUIDList(req.SharedTeams)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "shared_team_ids tidak valid")
			return
		}
		if err := s.store.SetNoteShares(r.Context(), n.ID, ids, actor); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	full, err := s.store.GetNote(r.Context(), n.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, actor, "note.create", "note", full.ID.String(),
		map[string]any{"title": full.Title, "visibility": full.Visibility}, true)
	writeJSON(w, http.StatusCreated, full)
}

// handleUpdateNote mengubah catatan.
//
// PATCH /api/notes/{id}
func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	var req noteUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		s.noteError(w, err)
		return
	}
	actor := currentUsername(r)
	if !s.canEditNote(r, userFrom(r), existing) {
		writeErr(w, http.StatusForbidden, "tidak berhak mengubah catatan ini")
		return
	}

	if req.Visibility != nil {
		v := strings.TrimSpace(*req.Visibility)
		if !validNoteVisibility(v) {
			writeErr(w, http.StatusBadRequest, "visibilitas harus 'internal' atau 'eksternal'")
			return
		}
	}

	params := repository.UpdateNoteParams{
		Title:      trimPtr(req.Title),
		Body:       req.Body,
		Visibility: trimPtr(req.Visibility),
		Color:      trimPtr(req.Color),
		Pinned:     req.Pinned,
		UpdatedBy:  actor,
	}
	// Pindah tim pemilik hanya untuk admin.
	if req.OwnerTeamID != nil && s.isSuper(r, userFrom(r)) {
		team, perr := s.resolveNoteOwnerTeam(r, userFrom(r), req.OwnerTeamID)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr.Error())
			return
		}
		params.OwnerTeamID = team
		params.SetOwner = true
	}

	if _, err := s.store.UpdateNote(r.Context(), id, params); err != nil {
		s.noteError(w, err)
		return
	}

	// Update share bila dikirim.
	if req.SharedTeams != nil {
		ids, perr := parseUUIDList(req.SharedTeams)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "shared_team_ids tidak valid")
			return
		}
		if err := s.store.SetNoteShares(r.Context(), id, ids, actor); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	full, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, actor, "note.update", "note", id.String(), nil, true)
	writeJSON(w, http.StatusOK, full)
}

// handleSetNoteShares mengganti daftar tim yang di-share (tombol "Bagikan").
//
// PATCH /api/notes/{id}/shares
func (s *Server) handleSetNoteShares(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	var req noteSharesRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		s.noteError(w, err)
		return
	}
	actor := currentUsername(r)
	if !s.canEditNote(r, userFrom(r), existing) {
		writeErr(w, http.StatusForbidden, "tidak berhak membagikan catatan ini")
		return
	}

	ids, perr := parseUUIDList(req.TeamIDs)
	if perr != nil {
		writeErr(w, http.StatusBadRequest, "team_ids tidak valid")
		return
	}
	if err := s.store.SetNoteShares(r.Context(), id, ids, actor); err != nil {
		writeInternalError(w, err)
		return
	}
	full, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, actor, "note.share", "note", id.String(), map[string]any{"teams": len(ids)}, true)
	writeJSON(w, http.StatusOK, full)
}

// handleDeleteNote menghapus catatan (admin, pembuat, atau anggota tim pemilik).
//
// DELETE /api/notes/{id}
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetNote(r.Context(), id)
	if err != nil {
		s.noteError(w, err)
		return
	}
	actor := currentUsername(r)
	if !s.canEditNote(r, userFrom(r), existing) {
		writeErr(w, http.StatusForbidden, "tidak berhak menghapus catatan ini")
		return
	}
	if err := s.store.DeleteNote(r.Context(), id); err != nil {
		s.noteError(w, err)
		return
	}
	s.audit(r, actor, "note.delete", "note", id.String(), map[string]any{"title": existing.Title}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/* ---------------------------------------------------------------------------
   Helper
   --------------------------------------------------------------------------- */

func noteID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id catatan tidak valid")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) noteError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "catatan tidak ditemukan")
		return
	}
	writeInternalError(w, err)
}

// isSuper melaporkan apakah user adalah super user (admin atau role bertanda
// is_super pada tabel roles).
func (s *Server) isSuper(r *http.Request, user *models.User) bool {
	if user == nil {
		return false
	}
	if user.Role == models.RoleAdmin {
		return true
	}
	ok := s.store.RoleIsSuper(r.Context(), user.Role)
	return ok
}

func validNoteVisibility(v string) bool {
	return v == models.NoteVisibilityInternal || v == models.NoteVisibilityEksternal
}

// parseUUIDList memvalidasi dan mengubah daftar string menjadi UUID unik.
func parseUUIDList(in []string) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// resolveNoteOwnerTeam menentukan tim pemilik catatan.
// Non-admin hanya boleh memakai tim sendiri; admin boleh memilih tim mana pun
// (kosong = tanpa tim).
func (s *Server) resolveNoteOwnerTeam(r *http.Request, user *models.User, raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		if s.isSuper(r, user) {
			return nil, nil
		}
		return user.TeamID, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, errInvalid("owner_team_id tidak valid")
	}
	if !s.isSuper(r, user) {
		if user.TeamID == nil || *user.TeamID != id {
			return nil, errInvalid("hanya admin yang dapat memilih tim pemilik lain")
		}
	}
	return &id, nil
}

// canViewNote: admin/super → semua; selain itu hanya bila tim pengguna adalah
// tim pemilik atau tim yang di-share. Tanpa tim → hanya pembuat.
func (s *Server) canViewNote(r *http.Request, user *models.User, n *models.Note) bool {
	if s.isSuper(r, user) {
		return true
	}
	if user == nil {
		return false
	}
	if user.TeamID == nil {
		return strings.EqualFold(n.CreatedBy, user.Username)
	}
	if n.OwnerTeamID != nil && *n.OwnerTeamID == *user.TeamID {
		return true
	}
	for _, tid := range n.SharedTeamIDs {
		if tid == *user.TeamID {
			return true
		}
	}
	return false
}

// canEditNote: admin/super, pembuat, atau anggota tim pemilik.
func (s *Server) canEditNote(r *http.Request, user *models.User, n *models.Note) bool {
	if s.isSuper(r, user) {
		return true
	}
	if user == nil {
		return false
	}
	if strings.EqualFold(n.CreatedBy, user.Username) {
		return true
	}
	if user.TeamID != nil && n.OwnerTeamID != nil && *n.OwnerTeamID == *user.TeamID {
		return true
	}
	return false
}
