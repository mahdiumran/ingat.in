package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   Notification templates
   --------------------------------------------------------------------------- */

const templateColumns = `
	id, key, item_type, channel, subject_tpl, body_tpl, severity, description,
	is_active, updated_by, created_at, updated_at`

func scanTemplate(row pgx.Row) (*models.NotificationTemplate, error) {
	t := &models.NotificationTemplate{}
	err := row.Scan(&t.ID, &t.Key, &t.ItemType, &t.Channel, &t.SubjectTpl, &t.BodyTpl,
		&t.Severity, &t.Description, &t.IsActive, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return t, nil
}

// ListTemplates mengambil seluruh template.
func (s *Store) ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+templateColumns+` FROM notification_templates
		ORDER BY key, channel NULLS FIRST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NotificationTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTemplate mengambil satu template berdasarkan key dan kanal.
//
// Strategi pencarian: utamakan template dengan kanal spesifik; bila tidak ada,
// pakai template dengan channel IS NULL (berlaku untuk semua kanal).
func (s *Store) GetTemplate(ctx context.Context, key, channel string) (*models.NotificationTemplate, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+templateColumns+` FROM notification_templates
		WHERE key=$1 AND is_active AND (channel = $2 OR channel IS NULL)
		ORDER BY (channel = $2) DESC NULLS LAST
		LIMIT 1`, key, channel)
	return scanTemplate(row)
}

// UpdateTemplateParams adalah parameter update template (parsial).
type UpdateTemplateParams struct {
	SubjectTpl *string
	BodyTpl    *string
	Severity   *string
	IsActive   *bool
	UpdatedBy  string
}

// UpdateTemplate memperbarui template.
func (s *Store) UpdateTemplate(ctx context.Context, id uuid.UUID, p UpdateTemplateParams) (*models.NotificationTemplate, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE notification_templates SET
			subject_tpl = COALESCE($2, subject_tpl),
			body_tpl    = COALESCE($3, body_tpl),
			severity    = COALESCE($4, severity),
			is_active   = COALESCE($5, is_active),
			updated_by  = $6,
			updated_at  = now()
		WHERE id = $1
		RETURNING `+templateColumns,
		id, p.SubjectTpl, p.BodyTpl, p.Severity, p.IsActive, p.UpdatedBy)
	return scanTemplate(row)
}

/* ---------------------------------------------------------------------------
   Escalation policies
   --------------------------------------------------------------------------- */

const policyColumns = `
	id, name, description, offsets_json, applies_to_item_types, quiet_hours_from,
	quiet_hours_to, max_attempts, is_default, is_active, created_at, updated_at`

func scanPolicy(row pgx.Row) (*models.EscalationPolicy, error) {
	p := &models.EscalationPolicy{}

	var offsets []map[string]any
	var quietFrom, quietTo *string

	err := row.Scan(&p.ID, &p.Name, &p.Description, &offsets, &p.AppliesToItemTypes,
		&quietFrom, &quietTo, &p.MaxAttempts, &p.IsDefault, &p.IsActive,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}

	// Ubah offset mentah menjadi struktur bertipe.
	p.Offsets = make([]models.EscalationOffset, 0, len(offsets))
	for _, o := range offsets {
		eo := models.EscalationOffset{
			Label:    asString(o["label"]),
			Severity: asString(o["severity"]),
		}
		if v, ok := asIntPtr(o["hours_before"]); ok {
			eo.HoursBefore = v
		}
		if v, ok := asIntPtr(o["hours_after"]); ok {
			eo.HoursAfter = v
		}
		p.Offsets = append(p.Offsets, eo)
	}

	p.QuietHoursFrom = quietFrom
	p.QuietHoursTo = quietTo
	return p, nil
}

// ListPolicies mengambil seluruh policy eskalasi.
func (s *Store) ListPolicies(ctx context.Context) ([]models.EscalationPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+policyColumns+` FROM escalation_policies
		ORDER BY is_default DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.EscalationPolicy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPolicy mengambil policy berdasarkan id.
func (s *Store) GetPolicy(ctx context.Context, id uuid.UUID) (*models.EscalationPolicy, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM escalation_policies WHERE id=$1`, id)
	return scanPolicy(row)
}

// GetPolicyByName mengambil policy berdasarkan nama (dipakai seed/fallback).
func (s *Store) GetPolicyByName(ctx context.Context, name string) (*models.EscalationPolicy, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM escalation_policies WHERE name=$1`, name)
	return scanPolicy(row)
}

// CreatePolicyParams adalah parameter pembuatan policy.
type CreatePolicyParams struct {
	Name        string
	Description string
	Offsets     []map[string]any
	ItemTypes   []string
	QuietFrom   *string
	QuietTo     *string
	MaxAttempts int
	IsDefault   bool
}

// CreatePolicy membuat policy eskalasi baru.
func (s *Store) CreatePolicy(ctx context.Context, p CreatePolicyParams) (*models.EscalationPolicy, error) {
	offsets := p.Offsets
	if offsets == nil {
		offsets = []map[string]any{}
	}
	itemTypes := p.ItemTypes
	if itemTypes == nil {
		itemTypes = []string{}
	}
	attempts := p.MaxAttempts
	if attempts <= 0 {
		attempts = 5
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO escalation_policies
			(name, description, offsets_json, applies_to_item_types, quiet_hours_from,
			 quiet_hours_to, max_attempts, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+policyColumns,
		p.Name, p.Description, offsets, itemTypes, p.QuietFrom, p.QuietTo, attempts, p.IsDefault)
	return scanPolicy(row)
}

// UpdatePolicyParams adalah parameter update policy (parsial).
type UpdatePolicyParams struct {
	Name        *string
	Description *string
	Offsets     []map[string]any
	ItemTypes   []string
	QuietFrom   *string
	QuietTo     *string
	MaxAttempts *int
	IsDefault   *bool
	IsActive    *bool
}

// UpdatePolicy memperbarui policy.
func (s *Store) UpdatePolicy(ctx context.Context, id uuid.UUID, p UpdatePolicyParams) (*models.EscalationPolicy, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE escalation_policies SET
			name                  = COALESCE($2, name),
			description           = COALESCE($3, description),
			offsets_json          = COALESCE($4, offsets_json),
			applies_to_item_types = COALESCE($5, applies_to_item_types),
			quiet_hours_from      = COALESCE($6, quiet_hours_from),
			quiet_hours_to        = COALESCE($7, quiet_hours_to),
			max_attempts          = COALESCE($8, max_attempts),
			is_default            = COALESCE($9, is_default),
			is_active             = COALESCE($10, is_active),
			updated_at            = now()
		WHERE id = $1
		RETURNING `+policyColumns,
		id, p.Name, p.Description, p.Offsets, p.ItemTypes, p.QuietFrom, p.QuietTo,
		p.MaxAttempts, p.IsDefault, p.IsActive)
	return scanPolicy(row)
}

// DeletePolicy menghapus policy.
func (s *Store) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM escalation_policies WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PolicyInUse melaporkan berapa reminder yang memakai policy ini.
func (s *Store) PolicyInUse(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reminder_details WHERE escalation_policy_id=$1`, id).Scan(&n)
	return n, err
}

/* ---------------------------------------------------------------------------
   Helper konversi tipe dari JSONB
   --------------------------------------------------------------------------- */

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asIntPtr(v any) (*int, bool) {
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i, true
	case int:
		return &n, true
	case int64:
		i := int(n)
		return &i, true
	default:
		return nil, false
	}
}
