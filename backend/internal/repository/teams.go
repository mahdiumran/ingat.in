package repository

import (
	"context"

	"github.com/google/uuid"

	"ingatin/backend/internal/models"
)

// Team & Organization — entitas pendukung RBAC dan pengelompokan kerja.

const teamColumns = `id, name, description, is_active, created_at, updated_at`
const orgColumns = `id, name, code, contacts, is_active, created_at, updated_at`

// ListTeams mengembalikan seluruh tim.
func (s *Store) ListTeams(ctx context.Context) ([]models.Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+teamColumns+` FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Team{}
	for rows.Next() {
		var t models.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTeam membuat tim baru.
func (s *Store) CreateTeam(ctx context.Context, name, description string) (*models.Team, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO teams (name, description) VALUES ($1,$2)
		RETURNING `+teamColumns, name, description)

	t := &models.Team{}
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return t, nil
}

// UpdateTeam memperbarui nama/deskripsi/status tim.
func (s *Store) UpdateTeam(ctx context.Context, id uuid.UUID, name, description *string, isActive *bool) (*models.Team, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE teams SET
			name        = COALESCE($2, name),
			description = COALESCE($3, description),
			is_active   = COALESCE($4, is_active),
			updated_at  = now()
		WHERE id = $1
		RETURNING `+teamColumns, id, name, description, isActive)

	t := &models.Team{}
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return t, nil
}

// DeleteTeam menghapus tim (referensi user/work_item menjadi NULL).
func (s *Store) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM teams WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TeamHasMembers melaporkan apakah masih ada user aktif pada tim.
func (s *Store) TeamHasMembers(ctx context.Context, id uuid.UUID) (bool, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE team_id=$1`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListOrganizations mengembalikan seluruh organisasi.
func (s *Store) ListOrganizations(ctx context.Context) ([]models.Organization, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+orgColumns+` FROM organizations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Organization{}
	for rows.Next() {
		var o models.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Code, &o.Contacts, &o.IsActive, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetDefaultOrganization mengembalikan organisasi pertama yang aktif
// (dipakai untuk mengisi organization_id pada data baru).
func (s *Store) GetDefaultOrganization(ctx context.Context) (*models.Organization, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+orgColumns+` FROM organizations WHERE is_active ORDER BY created_at LIMIT 1`)

	o := &models.Organization{}
	if err := row.Scan(&o.ID, &o.Name, &o.Code, &o.Contacts, &o.IsActive, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return o, nil
}
