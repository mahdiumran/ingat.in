package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

const masterDataColumns = `
	id, kind, code, label, description, parent_id, sort_order, meta_json,
	is_active, created_by, created_at, updated_at`

func scanMasterData(row pgx.Row) (*models.MasterData, error) {
	m := &models.MasterData{}
	err := row.Scan(
		&m.ID, &m.Kind, &m.Code, &m.Label, &m.Description, &m.ParentID,
		&m.SortOrder, &m.Meta, &m.IsActive, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, mapErr(err)
	}
	return m, nil
}

// MasterDataKind adalah registri kelompok master data.
func (s *Store) ListMasterDataKinds(ctx context.Context) ([]models.MasterDataKind, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, label, description, icon, is_system, sort_order
		FROM master_data_kinds ORDER BY sort_order, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MasterDataKind{}
	for rows.Next() {
		var k models.MasterDataKind
		if err := rows.Scan(&k.Kind, &k.Label, &k.Description, &k.Icon, &k.IsSystem, &k.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// CreateMasterDataKind mendaftarkan kelompok baru (dinamis dari panel).
func (s *Store) CreateMasterDataKind(ctx context.Context, kind, label, description, icon string) error {
	if strings.TrimSpace(icon) == "" {
		icon = "category"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order)
		VALUES ($1,$2,$3,$4,FALSE, 999)
		ON CONFLICT (kind) DO UPDATE SET label=EXCLUDED.label, description=EXCLUDED.description`,
		kind, label, description, icon)
	return err
}

// MasterDataFilter adalah filter daftar master data.
type MasterDataFilter struct {
	Kind       string
	Search     string
	ParentID   *uuid.UUID
	ActiveOnly bool
}

// ListMasterData mengambil entri master data sesuai filter.
func (s *Store) ListMasterData(ctx context.Context, f MasterDataFilter) ([]models.MasterData, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + itoa(len(args))
	}

	if f.Kind != "" {
		where = append(where, "kind = "+arg(f.Kind))
	}
	if f.ActiveOnly {
		where = append(where, "is_active")
	}
	if f.ParentID != nil {
		where = append(where, "parent_id = "+arg(*f.ParentID))
	}
	if f.Search != "" {
		like := "%" + strings.ToLower(f.Search) + "%"
		where = append(where, "(lower(label) LIKE "+arg(like)+" OR lower(code) LIKE "+arg(like)+")")
	}

	rows, err := s.pool.Query(ctx, `SELECT `+masterDataColumns+` FROM master_data WHERE `+
		strings.Join(where, " AND ")+` ORDER BY kind, sort_order, label`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MasterData{}
	for rows.Next() {
		m, err := scanMasterData(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetMasterData mengambil satu entri.
func (s *Store) GetMasterData(ctx context.Context, id uuid.UUID) (*models.MasterData, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+masterDataColumns+` FROM master_data WHERE id=$1`, id)
	return scanMasterData(row)
}

// NextMasterDataCode mengalokasikan kode berikutnya untuk sebuah kind memakai
// tabel ref_counters, mis. NextMasterDataCode(ctx, "customer") => "CUST-2026-0001".
//
// Aman untuk konkurensi (UPSERT + RETURNING). Dipakai agar nomor pelanggan
// tidak perlu diisi manual dan tidak pernah bentrok.
func (s *Store) NextMasterDataCode(ctx context.Context, prefix string) (string, error) {
	year := time.Now().UTC().Year()
	var seq int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ref_counters (prefix, year, last_value)
		VALUES ($1, $2, 1)
		ON CONFLICT (prefix, year)
		DO UPDATE SET last_value = ref_counters.last_value + 1
		RETURNING last_value`, prefix, year).Scan(&seq)
	if err != nil {
		return "", fmt.Errorf("alokasi kode %s: %w", prefix, err)
	}
	return fmt.Sprintf("%s-%d-%04d", prefix, year, seq), nil
}

// CreateMasterDataParams adalah parameter pembuatan entri.
type CreateMasterDataParams struct {
	Kind        string
	Code        string
	Label       string
	Description string
	ParentID    *uuid.UUID
	SortOrder   int
	Meta        map[string]any
	IsActive    bool
	CreatedBy   string
}

// CreateMasterData menambahkan entri master data.
func (s *Store) CreateMasterData(ctx context.Context, p CreateMasterDataParams) (*models.MasterData, error) {
	meta := p.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO master_data
			(kind, code, label, description, parent_id, sort_order, meta_json, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+masterDataColumns,
		p.Kind, p.Code, p.Label, p.Description, p.ParentID, p.SortOrder, meta, p.IsActive, p.CreatedBy)
	return scanMasterData(row)
}

// UpdateMasterDataParams adalah parameter update parsial.
type UpdateMasterDataParams struct {
	Code        *string
	Label       *string
	Description *string
	ParentID    *uuid.UUID
	ClearParent bool
	SortOrder   *int
	Meta        map[string]any
	IsActive    *bool
}

// UpdateMasterData memperbarui entri.
//
// meta_json digabung (JSONB ||) alih-alih ditimpa: PATCH parsial tidak akan
// menghapus atribut lain yang tidak disertakan (mis. mengubah PIC customer
// tidak boleh menghapus telepon/email/kapasitas).
func (s *Store) UpdateMasterData(ctx context.Context, id uuid.UUID, p UpdateMasterDataParams) (*models.MasterData, error) {
	_, err := s.pool.Exec(ctx, `
		UPDATE master_data SET
			code        = COALESCE($2, code),
			label       = COALESCE($3, label),
			description = COALESCE($4, description),
			parent_id   = CASE WHEN $5::boolean THEN NULL ELSE COALESCE($6, parent_id) END,
			sort_order  = COALESCE($7, sort_order),
			meta_json   = CASE WHEN $8::jsonb IS NULL THEN meta_json
			                   ELSE COALESCE(meta_json, '{}'::jsonb) || $8::jsonb END,
			is_active   = COALESCE($9, is_active),
			updated_at  = now()
		WHERE id = $1`,
		id, p.Code, p.Label, p.Description, p.ClearParent, p.ParentID, p.SortOrder, p.Meta, p.IsActive)
	if err != nil {
		return nil, err
	}
	return s.GetMasterData(ctx, id)
}

// DeleteMasterData menghapus entri (hard delete; anak dihapus berjenjang).
func (s *Store) DeleteMasterData(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM master_data WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

/* ---------------------------------------------------------------------------
   Role permissions
   --------------------------------------------------------------------------- */

// ListRolePermissions mengembalikan matriks izin seluruh peran.
func (s *Store) ListRolePermissions(ctx context.Context) ([]models.RolePermission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, action, allowed, updated_by, updated_at
		FROM role_permissions ORDER BY role, action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.RolePermission{}
	for rows.Next() {
		var r models.RolePermission
		if err := rows.Scan(&r.Role, &r.Action, &r.Allowed, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRolePermission menetapkan izin satu (role, action).
func (s *Store) SetRolePermission(ctx context.Context, role, action string, allowed bool, by string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_permissions (role, action, allowed, updated_by, updated_at)
		VALUES ($1,$2,$3,$4, now())
		ON CONFLICT (role, action) DO UPDATE
			SET allowed = EXCLUDED.allowed, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		role, action, allowed, by)
	return err
}

// rolePermissionCache memuat matriks izin ke memori agar pengecekan pada
// middleware tidak menambah query per permintaan.
type rolePermissionCache struct {
	loaded bool
	m      map[string]bool // key: role + "|" + action
}

var permCache = &rolePermissionCache{}

// PermissionAllowed melaporkan apakah role boleh melakukan action.
//
// Aturan: peran super user (is_super, mis. admin) SELALU boleh (dijaga di sini
// agar tidak dapat dikunci sendiri); peran lain mengikuti matriks
// role_permissions. Bila cache belum termuat, nilainya diambil dari database.
func (s *Store) PermissionAllowed(ctx context.Context, role, action string) bool {
	if s.RoleIsSuper(ctx, role) {
		return true
	}
	if !permCache.loaded {
		rows, err := s.ListRolePermissions(ctx)
		if err == nil {
			m := map[string]bool{}
			for _, r := range rows {
				m[r.Role+"|"+r.Action] = r.Allowed
			}
			permCache.m = m
			permCache.loaded = true
		}
	}
	if permCache.m == nil {
		return false
	}
	return permCache.m[role+"|"+action]
}

// EffectivePermissions mengembalikan daftar action yang diizinkan untuk role.
// Super user selalu mendapat seluruh action yang dikenal panel.
func (s *Store) EffectivePermissions(ctx context.Context, role string) []string {
	if s.RoleIsSuper(ctx, role) {
		return append([]string(nil), knownActions...)
	}
	rows, err := s.ListRolePermissions(ctx)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, r := range rows {
		if r.Role == role && r.Allowed {
			out = append(out, r.Action)
		}
	}
	return out
}

// knownActions mencerminkan daftar aksi pada matriks izin (lihat api.PermissionActions).
var knownActions = []string{
	"items.write", "providers.view", "providers.write", "masterdata.write", "users.write",
	"teams.write", "roles.write", "kpi.view",
}

// RoleIsSuper melaporkan apakah role adalah super user (is_super).
// Fallback: role "admin" diperlakukan super meski tabel roles belum termigrasi.
func (s *Store) RoleIsSuper(ctx context.Context, role string) bool {
	if role == models.RoleAdmin {
		return true
	}
	r, err := s.GetRole(ctx, role)
	if err != nil {
		return false
	}
	return r.IsSuper
}

// InvalidatePermissionCache dipanggil setelah matriks izin diubah.
func (s *Store) InvalidatePermissionCache() {
	permCache.loaded = false
	permCache.m = nil
}
