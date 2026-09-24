package repository

import (
	"context"

	"ingatin/backend/internal/models"
)

// Roles — registri peran dinamis (tabel roles).
//
// Peran tidak lagi hardcoded: operator dapat menambah/mengubah/menghapus peran
// dari panel. `admin` (is_super) selalu dapat seluruh izin dan tidak dapat
// dihapus. Peran `is_system` bawaan tidak dapat dihapus.

const roleColumns = `role, label, description, is_super, is_system, rank, created_at, updated_at`

// ListRoles mengembalikan seluruh peran terurut berdasarkan rank.
func (s *Store) ListRoles(ctx context.Context) ([]models.Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+roleColumns+` FROM roles ORDER BY rank, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Role{}
	for rows.Next() {
		var r models.Role
		if err := rows.Scan(&r.Role, &r.Label, &r.Description, &r.IsSuper, &r.IsSystem, &r.Rank, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRole mengambil satu peran.
func (s *Store) GetRole(ctx context.Context, role string) (*models.Role, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+roleColumns+` FROM roles WHERE role=$1`, role)
	r := &models.Role{}
	if err := row.Scan(&r.Role, &r.Label, &r.Description, &r.IsSuper, &r.IsSystem, &r.Rank, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return r, nil
}

// RoleExists melaporkan apakah role terdaftar.
func (s *Store) RoleExists(ctx context.Context, role string) (bool, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM roles WHERE role=$1`, role).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateRole menambah peran dinamis baru.
func (s *Store) CreateRole(ctx context.Context, role, label, description string, rank int) (*models.Role, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO roles (role, label, description, is_super, is_system, rank)
		VALUES ($1,$2,$3,FALSE,FALSE,$4)
		RETURNING `+roleColumns, role, label, description, rank)

	r := &models.Role{}
	if err := row.Scan(&r.Role, &r.Label, &r.Description, &r.IsSuper, &r.IsSystem, &r.Rank, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	s.InvalidateRoleCache()
	return r, nil
}

// UpdateRole memperbarui label/deskripsi/rank sebuah peran.
func (s *Store) UpdateRole(ctx context.Context, role string, label, description *string, rank *int) (*models.Role, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE roles SET
			label       = COALESCE($2, label),
			description = COALESCE($3, description),
			rank        = COALESCE($4, rank),
			updated_at  = now()
		WHERE role = $1
		RETURNING `+roleColumns, role, label, description, rank)

	r := &models.Role{}
	if err := row.Scan(&r.Role, &r.Label, &r.Description, &r.IsSuper, &r.IsSystem, &r.Rank, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return r, nil
}

// DeleteRole menghapus peran dinamis (bukan system/super).
func (s *Store) DeleteRole(ctx context.Context, role string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE role=$1 AND NOT is_system AND NOT is_super`, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.InvalidateRoleCache()
	return nil
}

// CountUsersByRoleName menghitung user (semua status) yang memakai sebuah role.
func (s *Store) CountUsersByRoleName(ctx context.Context, role string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role=$1`, role).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// roleCache menyimpan daftar nama peran untuk validasi cepat.
type roleNameCache struct {
	loaded bool
	set    map[string]bool
}

var roleCacheRef = &roleNameCache{}

// RoleNameSet melaporkan himpunan nama peran yang valid.
func (s *Store) RoleNameSet(ctx context.Context) map[string]bool {
	if !roleCacheRef.loaded {
		rows, err := s.ListRoles(ctx)
		if err == nil {
			m := map[string]bool{}
			for _, r := range rows {
				m[r.Role] = true
			}
			roleCacheRef.set = m
			roleCacheRef.loaded = true
		}
	}
	if roleCacheRef.set == nil {
		return map[string]bool{}
	}
	return roleCacheRef.set
}

// InvalidateRoleCache dipanggil setelah daftar peran berubah.
func (s *Store) InvalidateRoleCache() {
	roleCacheRef.loaded = false
	roleCacheRef.set = nil
}
