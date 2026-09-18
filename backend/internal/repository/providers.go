package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

const providerColumns = `
	id, channel, kind, label, base_url, api_key_enc, extra_json, is_default,
	is_active, last_test_at, last_test_ok, last_test_error, created_at, updated_at`

func scanProvider(row pgx.Row) (*models.NotificationProvider, error) {
	p := &models.NotificationProvider{}
	err := row.Scan(
		&p.ID, &p.Channel, &p.Kind, &p.Label, &p.BaseURL, &p.APIKeyEnc, &p.Extra,
		&p.IsDefault, &p.IsActive, &p.LastTestAt, &p.LastTestOK, &p.LastTestError,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, mapErr(err)
	}
	p.HasAPIKey = p.APIKeyEnc != ""
	// Jangan pernah mengirim ciphertext ke luar lapisan repository.
	p.APIKeyEnc = ""
	return p, nil
}

// CreateProviderParams adalah parameter pembuatan provider.
type CreateProviderParams struct {
	Channel   string
	Kind      string
	Label     string
	BaseURL   string
	APIKeyEnc string
	Extra     map[string]any
	IsDefault bool
	IsActive  bool
}

// CreateProvider menambahkan provider notifikasi.
func (s *Store) CreateProvider(ctx context.Context, p CreateProviderParams) (*models.NotificationProvider, error) {
	extra := p.Extra
	if extra == nil {
		extra = map[string]any{}
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO notification_providers
			(channel, kind, label, base_url, api_key_enc, extra_json, is_default, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+providerColumns,
		p.Channel, p.Kind, p.Label, p.BaseURL, p.APIKeyEnc, extra, p.IsDefault, p.IsActive)
	return scanProvider(row)
}

// GetProvider mengambil provider berdasarkan id (tanpa ciphertext).
func (s *Store) GetProvider(ctx context.Context, id uuid.UUID) (*models.NotificationProvider, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+providerColumns+` FROM notification_providers WHERE id=$1`, id)
	return scanProvider(row)
}

// ProviderExtraWithSecret mengambil provider beserta ciphertext API key.
//
// Dipakai oleh outbox saat akan mengirim; ciphertext didekripsi oleh pemanggil.
func (s *Store) ProviderExtraWithSecret(ctx context.Context, id uuid.UUID) (*models.NotificationProvider, string, error) {
	var (
		p   models.NotificationProvider
		enc string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, channel, kind, label, base_url, api_key_enc, extra_json, is_default,
		       is_active, last_test_at, last_test_ok, last_test_error, created_at, updated_at
		FROM notification_providers WHERE id=$1`, id,
	).Scan(&p.ID, &p.Channel, &p.Kind, &p.Label, &p.BaseURL, &enc, &p.Extra,
		&p.IsDefault, &p.IsActive, &p.LastTestAt, &p.LastTestOK, &p.LastTestError,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, "", mapErr(err)
	}
	p.HasAPIKey = enc != ""
	p.APIKeyEnc = ""
	return &p, enc, nil
}

// ListProviders mengambil seluruh provider.
func (s *Store) ListProviders(ctx context.Context) ([]models.NotificationProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+providerColumns+` FROM notification_providers
		ORDER BY channel, is_default DESC, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NotificationProvider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// CountProvidersByChannel menghitung provider (aktif maupun tidak) pada kanal.
//
// Dipakai bootstrap untuk memutuskan apakah perlu membuat provider WAHA awal
// dari konfigurasi environment.
func (s *Store) CountProvidersByChannel(ctx context.Context, channel string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notification_providers WHERE channel=$1`, channel).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// GetDefaultProviderForChannel mengambil provider default aktif untuk kanal.
func (s *Store) GetDefaultProviderForChannel(ctx context.Context, channel string) (*models.NotificationProvider, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+providerColumns+` FROM notification_providers
		WHERE channel=$1 AND is_active
		ORDER BY is_default DESC, created_at
		LIMIT 1`, channel)
	return scanProvider(row)
}

// UpdateProviderParams adalah parameter update provider (parsial).
type UpdateProviderParams struct {
	Label     *string
	BaseURL   *string
	APIKeyEnc *string // nil = jangan ubah; "" = hapus
	Extra     map[string]any
	IsDefault *bool
	IsActive  *bool
}

// UpdateProvider memperbarui provider.
func (s *Store) UpdateProvider(ctx context.Context, id uuid.UUID, p UpdateProviderParams) (*models.NotificationProvider, error) {
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		// Bila provider ini dijadikan default, cabut default lain pada kanal
		// yang sama supaya hanya ada satu default per kanal.
		if p.IsDefault != nil && *p.IsDefault {
			var channel string
			if err := tx.QueryRow(ctx,
				`SELECT channel FROM notification_providers WHERE id=$1`, id).Scan(&channel); err != nil {
				return mapErr(err)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE notification_providers SET is_default=FALSE WHERE channel=$1 AND id<>$2`,
				channel, id); err != nil {
				return err
			}
		}

		_, err := tx.Exec(ctx, `
			UPDATE notification_providers SET
				label       = COALESCE($2, label),
				base_url    = COALESCE($3, base_url),
				api_key_enc = CASE WHEN $4::text IS NULL THEN api_key_enc ELSE $4 END,
				extra_json  = COALESCE($5, extra_json),
				is_default  = COALESCE($6, is_default),
				is_active   = COALESCE($7, is_active),
				updated_at  = now()
			WHERE id = $1`,
			id, p.Label, p.BaseURL, p.APIKeyEnc, p.Extra, p.IsDefault, p.IsActive)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, id)
}

// DeleteProvider menghapus provider. Binding yang memakainya menjadi NULL.
func (s *Store) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_providers WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordProviderTest menyimpan hasil uji koneksi provider.
func (s *Store) RecordProviderTest(ctx context.Context, id uuid.UUID, ok bool, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notification_providers
		SET last_test_at = $2, last_test_ok = $3, last_test_error = $4, updated_at = now()
		WHERE id = $1`, id, time.Now().UTC(), ok, errMsg)
	return err
}
