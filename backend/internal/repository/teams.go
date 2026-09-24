package repository

import (
	"context"

	"github.com/google/uuid"

	"ingatin/backend/internal/models"
)

// Team & Organization — entitas pendukung RBAC dan pengelompokan kerja.

const teamColumns = `id, name, description, category, target_id, is_active, created_at, updated_at`
const orgColumns = `id, name, code, contacts, is_active, created_at, updated_at`

// scanTeam memindai satu baris tim (kolom mengikuti teamColumns).
func scanTeam(row interface{ Scan(...any) error }) (*models.Team, error) {
	t := &models.Team{}
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Category, &t.TargetID,
		&t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// ListTeams mengembalikan seluruh tim.
func (s *Store) ListTeams(ctx context.Context) ([]models.Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+teamColumns+` FROM teams ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Team{}
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// CreateTeam membuat tim baru.
func (s *Store) CreateTeam(ctx context.Context, name, description, category string, targetID *uuid.UUID) (*models.Team, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO teams (name, description, category, target_id) VALUES ($1,$2,$3,$4)
		RETURNING `+teamColumns, name, description, category, targetID)
	return scanTeam(row)
}

// UpdateTeam memperbarui nama/deskripsi/kategori/status/target tim.
// targetID & setTarget memakai pola set-flag agar dapat dikosongkan (NULL).
func (s *Store) UpdateTeam(ctx context.Context, id uuid.UUID, name, description, category *string, isActive *bool, targetID *uuid.UUID, setTarget bool) (*models.Team, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE teams SET
			name        = COALESCE($2, name),
			description = COALESCE($3, description),
			category    = COALESCE($4, category),
			is_active   = COALESCE($5, is_active),
			target_id   = CASE WHEN $7::boolean THEN $6 ELSE target_id END,
			updated_at  = now()
		WHERE id = $1
		RETURNING `+teamColumns, id, name, description, category, isActive, targetID, setTarget)
	return scanTeam(row)
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
