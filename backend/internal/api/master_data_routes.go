package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type masterDataCreateRequest struct {
	Kind        string         `json:"kind"`
	Code        string         `json:"code"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	ParentID    *string        `json:"parent_id"`
	SortOrder   *int           `json:"sort_order"`
	Meta        map[string]any `json:"meta"`
	IsActive    *bool          `json:"is_active"`
}

type masterDataUpdateRequest struct {
	Code        *string        `json:"code"`
	Label       *string        `json:"label"`
	Description *string        `json:"description"`
	ParentID    *string        `json:"parent_id"`
	ClearParent *bool          `json:"clear_parent"`
	SortOrder   *int           `json:"sort_order"`
	Meta        map[string]any `json:"meta"`
	IsActive    *bool          `json:"is_active"`
}

type masterDataKindRequest struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

type rolePermissionRequest struct {
	Role    string `json:"role"`
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
}

/* ---------------------------------------------------------------------------
   Master data
   --------------------------------------------------------------------------- */

// handleListMasterData mengembalikan entri master data + registri kind.
//
// GET /api/master-data?kind=&q=&active=
func (s *Server) handleListMasterData(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := repository.MasterDataFilter{
		Kind:       q.Get("kind"),
		Search:     q.Get("q"),
		ActiveOnly: q.Get("active") == "true" || q.Get("active") == "1",
	}

	if raw := q.Get("parent_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "parent_id tidak valid")
			return
		}
		filter.ParentID = &id
	}

	entries, err := s.store.ListMasterData(r.Context(), filter)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	kinds, err := s.store.ListMasterDataKinds(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"kinds":   kinds,
		"total":   len(entries),
	})
}

// handleCreateMasterData menambah entri master data.
//
// POST /api/master-data
func (s *Server) handleCreateMasterData(w http.ResponseWriter, r *http.Request) {
	var req masterDataCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Kind = strings.TrimSpace(req.Kind)
	req.Code = strings.TrimSpace(req.Code)
	req.Label = strings.TrimSpace(req.Label)

	if req.Kind == "" || !isValidKindSlug(req.Kind) {
		writeErr(w, http.StatusBadRequest, "kind wajib diisi (huruf kecil, angka, underscore)")
		return
	}
	if req.Label == "" {
		writeErr(w, http.StatusBadRequest, "label wajib diisi")
		return
	}
	// Kode opsional: bila kosong, turunkan dari label agar operator tidak
	// dipaksa mengisi dua kali untuk kebutuhan sederhana. Khusus customer,
	// kode dibuat otomatis berformat CUST-<tahun>-<urut>.
	if req.Code == "" {
		if req.Kind == models.KindCustomer {
			code, err := s.store.NextMasterDataCode(r.Context(), "CUST")
			if err != nil {
				writeInternalError(w, err)
				return
			}
			req.Code = code
		} else {
			req.Code = slugify(req.Label)
		}
	}
	if req.Code == "" {
		writeErr(w, http.StatusBadRequest, "kode wajib diisi")
		return
	}

	// Validasi meta khusus customer (jenis personal/corporate, dll.).
	if req.Kind == models.KindCustomer {
		if err := validateCustomerMeta(req.Meta); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	parentID, err := parseOptionalUUID(req.ParentID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "parent_id tidak valid")
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	m, err := s.store.CreateMasterData(r.Context(), repository.CreateMasterDataParams{
		Kind:        req.Kind,
		Code:        req.Code,
		Label:       req.Label,
		Description: req.Description,
		ParentID:    parentID,
		SortOrder:   sortOrder,
		Meta:        req.Meta,
		IsActive:    isActive,
		CreatedBy:   currentUsername(r),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "kode "+req.Code+" sudah dipakai pada kelompok "+req.Kind)
			return
		}
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "masterdata.create", "master_data", m.ID.String(), map[string]any{
		"kind": m.Kind, "code": m.Code,
	}, true)
	writeJSON(w, http.StatusCreated, m)
}

// handleUpdateMasterData memperbarui entri master data.
//
// PATCH /api/master-data/{id}
func (s *Server) handleUpdateMasterData(w http.ResponseWriter, r *http.Request) {
	id, ok := s.masterDataID(w, r)
	if !ok {
		return
	}

	var req masterDataUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	params := repository.UpdateMasterDataParams{
		Code:        trimPtr(req.Code),
		Label:       trimPtr(req.Label),
		Description: req.Description,
		SortOrder:   req.SortOrder,
		Meta:        req.Meta,
		IsActive:    req.IsActive,
	}

	// Validasi meta customer bila diubah: gabungkan dengan meta lama agar field
	// yang tidak dikirim tetap dinilai konsisten (mis. corporate tanpa PIC).
	if req.Meta != nil {
		if existing, err := s.store.GetMasterData(r.Context(), id); err == nil && existing.Kind == models.KindCustomer {
			merged := map[string]any{}
			for k, v := range existing.Meta {
				merged[k] = v
			}
			for k, v := range req.Meta {
				merged[k] = v
			}
			if err := validateCustomerMeta(merged); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if req.ClearParent != nil && *req.ClearParent {
		params.ClearParent = true
	} else if req.ParentID != nil {
		pid, err := parseOptionalUUID(req.ParentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "parent_id tidak valid")
			return
		}
		params.ParentID = pid
	}

	m, err := s.store.UpdateMasterData(r.Context(), id, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "entri master data tidak ditemukan")
			return
		}
		if isUniqueViolation(err) {
			writeErr(w, http.StatusConflict, "kode sudah dipakai pada kelompok ini")
			return
		}
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "masterdata.update", "master_data", id.String(), nil, true)
	writeJSON(w, http.StatusOK, m)
}

// handleDeleteMasterData menghapus entri master data.
//
// DELETE /api/master-data/{id}
func (s *Server) handleDeleteMasterData(w http.ResponseWriter, r *http.Request) {
	id, ok := s.masterDataID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteMasterData(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "entri master data tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "masterdata.delete", "master_data", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCreateMasterDataKind mendaftarkan kelompok master data baru.
//
// POST /api/master-data/kinds
func (s *Server) handleCreateMasterDataKind(w http.ResponseWriter, r *http.Request) {
	var req masterDataKindRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.Label = strings.TrimSpace(req.Label)

	if req.Kind == "" || !isValidKindSlug(req.Kind) {
		writeErr(w, http.StatusBadRequest, "kind wajib diisi (huruf kecil, angka, underscore)")
		return
	}
	if req.Label == "" {
		writeErr(w, http.StatusBadRequest, "label kelompok wajib diisi")
		return
	}

	if err := s.store.CreateMasterDataKind(r.Context(), req.Kind, req.Label, req.Description, req.Icon); err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "masterdata.kind.create", "master_data_kind", req.Kind, nil, true)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "kind": req.Kind})
}

/* ---------------------------------------------------------------------------
   Role permissions
   --------------------------------------------------------------------------- */

// handleListRolePermissions mengembalikan matriks izin + daftar aksi dikenali.
//
// GET /api/role-permissions
func (s *Server) handleListRolePermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := s.store.ListRolePermissions(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"permissions": perms,
		"roles":       []string{models.RoleAdmin, models.RoleNOC, models.RoleAgent, models.RoleSales, models.RoleViewer},
		"actions":     []string{"items.write", "providers.write", "masterdata.write", "users.write"},
	})
}

// handleSetRolePermission mengubah satu sel matriks izin.
//
// POST /api/role-permissions
func (s *Server) handleSetRolePermission(w http.ResponseWriter, r *http.Request) {
	var req rolePermissionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !isKnownRole(req.Role) {
		writeErr(w, http.StatusBadRequest, "role tidak dikenal")
		return
	}
	if strings.TrimSpace(req.Action) == "" {
		writeErr(w, http.StatusBadRequest, "action wajib diisi")
		return
	}
	// Admin tidak dapat dikunci dari panel (lihat PermissionAllowed).
	if req.Role == models.RoleAdmin && !req.Allowed {
		writeErr(w, http.StatusBadRequest, "izin admin tidak dapat dicabut")
		return
	}

	if err := s.store.SetRolePermission(r.Context(), req.Role, req.Action, req.Allowed, currentUsername(r)); err != nil {
		writeInternalError(w, err)
		return
	}
	s.store.InvalidatePermissionCache()
	s.audit(r, "", "role.permission.update", "role_permission", req.Role+":"+req.Action, map[string]any{
		"allowed": req.Allowed,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/* ---------------------------------------------------------------------------
   Helper
   --------------------------------------------------------------------------- */

func (s *Server) masterDataID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id master data tidak valid")
		return uuid.Nil, false
	}
	return id, true
}

func isKnownRole(role string) bool {
	switch role {
	case models.RoleAdmin, models.RoleAgent, models.RoleNOC, models.RoleSales, models.RoleViewer, models.RoleCustomer:
		return true
	default:
		return false
	}
}

// isValidKindSlug membatasi kind pada huruf kecil, angka, dan underscore agar
// aman dipakai sebagai kunci dan tidak membingungkan di URL/UI.
func isValidKindSlug(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// slugify mengubah label menjadi kode: "Instalasi Baru" -> "instalasi_baru".
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// isUniqueViolation melaporkan apakah error berasal dari pelanggaran UNIQUE
// (PostgreSQL SQLSTATE 23505). Dipakai untuk mengubah konflik kode menjadi
// pesan 409 yang ramah, bukan 500.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// validateCustomerMeta memvalidasi atribut pelanggan.
//
// Aturan: jenis wajib "personal" atau "corporate". Bila corporate, nama PIC
// wajib diisi. Field lain opsional.
func validateCustomerMeta(meta map[string]any) error {
	jenis := metaString(meta, "jenis")
	if jenis == "" {
		return errors.New("jenis pelanggan wajib diisi (personal atau corporate)")
	}
	if jenis != models.CustomerPersonal && jenis != models.CustomerCorporate {
		return errors.New("jenis pelanggan harus personal atau corporate")
	}
	if jenis == models.CustomerCorporate && metaString(meta, "pic") == "" {
		return errors.New("nama PIC wajib diisi untuk pelanggan corporate")
	}
	return nil
}

// metaString mengambil nilai string dari map meta secara aman.
func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}
