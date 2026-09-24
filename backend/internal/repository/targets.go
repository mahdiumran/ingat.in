package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Notification targets
   --------------------------------------------------------------------------- */

const targetColumns = `id, name, kind, notes, organization_id, is_active, created_at, updated_at`

func scanTarget(row pgx.Row) (*models.NotificationTarget, error) {
	t := &models.NotificationTarget{}
	err := row.Scan(&t.ID, &t.Name, &t.Kind, &t.Notes, &t.OrganizationID,
		&t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return t, nil
}

// CreateTargetParams adalah parameter pembuatan target.
type CreateTargetParams struct {
	Name           string
	Kind           string
	Notes          string
	OrganizationID *uuid.UUID
	IsActive       bool
}

// CreateTarget membuat target notifikasi baru.
func (s *Store) CreateTarget(ctx context.Context, p CreateTargetParams) (*models.NotificationTarget, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO notification_targets (name, kind, notes, organization_id, is_active)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING `+targetColumns,
		p.Name, p.Kind, p.Notes, p.OrganizationID, p.IsActive)
	return scanTarget(row)
}

// GetTarget mengambil target berdasarkan id, termasuk binding-nya.
func (s *Store) GetTarget(ctx context.Context, id uuid.UUID) (*models.NotificationTarget, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+targetColumns+` FROM notification_targets WHERE id=$1`, id)
	t, err := scanTarget(row)
	if err != nil {
		return nil, err
	}
	bindings, err := s.ListBindingsForTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	t.Bindings = bindings
	return t, nil
}

// ListTargets mengambil seluruh target beserta binding-nya.
func (s *Store) ListTargets(ctx context.Context) ([]models.NotificationTarget, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+targetColumns+` FROM notification_targets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NotificationTarget{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		index[t.ID] = len(out)
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Ambil semua binding sekali, lalu kelompokkan (menghindari N+1).
	bindings, err := s.listAllBindings(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if i, ok := index[b.TargetID]; ok {
			out[i].Bindings = append(out[i].Bindings, b)
		}
	}
	return out, nil
}

// TargetOption adalah ringkasan target untuk dropdown form (id + nama),
// tanpa binding/destination agar aman dibaca semua role (F32).
type TargetOption struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	IsActive bool      `json:"is_active"`
}

// ListTargetOptions mengembalikan daftar target ringan (tanpa binding).
func (s *Store) ListTargetOptions(ctx context.Context) ([]TargetOption, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, kind, is_active FROM notification_targets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TargetOption{}
	for rows.Next() {
		var o TargetOption
		if err := rows.Scan(&o.ID, &o.Name, &o.Kind, &o.IsActive); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpdateTargetParams adalah parameter update target (parsial).
type UpdateTargetParams struct {
	Name     *string
	Kind     *string
	Notes    *string
	IsActive *bool
}

// UpdateTarget memperbarui target.
func (s *Store) UpdateTarget(ctx context.Context, id uuid.UUID, p UpdateTargetParams) (*models.NotificationTarget, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE notification_targets SET
			name       = COALESCE($2, name),
			kind       = COALESCE($3, kind),
			notes      = COALESCE($4, notes),
			is_active  = COALESCE($5, is_active),
			updated_at = now()
		WHERE id = $1
		RETURNING `+targetColumns, id, p.Name, p.Kind, p.Notes, p.IsActive)
	return scanTarget(row)
}

// DeleteTarget menghapus target beserta binding-nya (FK CASCADE).
func (s *Store) DeleteTarget(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_targets WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

/* ---------------------------------------------------------------------------
   Target bindings
   --------------------------------------------------------------------------- */

const bindingColumns = `
	id, target_id, channel, destination, provider_id, label, is_primary,
	is_active, verified_at, created_at, updated_at`

func scanBinding(row pgx.Row) (*models.TargetBinding, error) {
	b := &models.TargetBinding{}
	err := row.Scan(&b.ID, &b.TargetID, &b.Channel, &b.Destination, &b.ProviderID,
		&b.Label, &b.IsPrimary, &b.IsActive, &b.VerifiedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return b, nil
}

// ListBindingsForTarget mengambil binding satu target.
func (s *Store) ListBindingsForTarget(ctx context.Context, targetID uuid.UUID) ([]models.TargetBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bindingColumns+` FROM target_bindings
		WHERE target_id=$1 ORDER BY channel, is_primary DESC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.TargetBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// listAllBindings mengambil seluruh binding untuk pengelompokan di memori.
func (s *Store) listAllBindings(ctx context.Context) ([]models.TargetBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bindingColumns+` FROM target_bindings ORDER BY channel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.TargetBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// CreateBindingParams adalah parameter pembuatan binding.
type CreateBindingParams struct {
	TargetID    uuid.UUID
	Channel     string
	Destination string
	ProviderID  *uuid.UUID
	Label       string
	IsPrimary   bool
	IsActive    bool
}

// CreateBinding menambahkan binding tujuan pada target.
func (s *Store) CreateBinding(ctx context.Context, p CreateBindingParams) (*models.TargetBinding, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO target_bindings
			(target_id, channel, destination, provider_id, label, is_primary, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+bindingColumns,
		p.TargetID, p.Channel, p.Destination, p.ProviderID, p.Label, p.IsPrimary, p.IsActive)
	return scanBinding(row)
}

// GetBinding mengambil binding berdasarkan id.
func (s *Store) GetBinding(ctx context.Context, id uuid.UUID) (*models.TargetBinding, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+bindingColumns+` FROM target_bindings WHERE id=$1`, id)
	return scanBinding(row)
}

// UpdateBindingParams adalah parameter update binding (parsial).
type UpdateBindingParams struct {
	Destination *string
	ProviderID  *uuid.UUID
	Label       *string
	IsPrimary   *bool
	IsActive    *bool
}

// UpdateBinding memperbarui binding.
func (s *Store) UpdateBinding(ctx context.Context, id uuid.UUID, p UpdateBindingParams) (*models.TargetBinding, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE target_bindings SET
			destination = COALESCE($2, destination),
			provider_id = COALESCE($3, provider_id),
			label       = COALESCE($4, label),
			is_primary  = COALESCE($5, is_primary),
			is_active   = COALESCE($6, is_active),
			updated_at  = now()
		WHERE id = $1
		RETURNING `+bindingColumns, id, p.Destination, p.ProviderID, p.Label, p.IsPrimary, p.IsActive)
	return scanBinding(row)
}

// DeleteBinding menghapus binding.
func (s *Store) DeleteBinding(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM target_bindings WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkBindingVerified menandai binding sudah lolos uji kirim.
func (s *Store) MarkBindingVerified(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE target_bindings SET verified_at=$2, updated_at=now() WHERE id=$1`, id, at.UTC())
	return err
}

// ActiveBindingsForTarget mengambil binding aktif sebuah target beserta
// provider default kanal bila binding tidak menunjuk provider tertentu.
func (s *Store) ActiveBindingsForTarget(ctx context.Context, targetID uuid.UUID) ([]models.TargetBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bindingColumns+` FROM target_bindings
		WHERE target_id=$1 AND is_active ORDER BY is_primary DESC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.TargetBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}
