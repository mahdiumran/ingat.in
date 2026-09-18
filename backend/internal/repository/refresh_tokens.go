package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

const refreshTokenColumns = `
	id, user_id, token_hash, user_agent, ip, expires_at, revoked_at, rotated_from, created_at`

func scanRefreshToken(row pgx.Row) (*models.RefreshToken, error) {
	rt := &models.RefreshToken{}
	err := row.Scan(
		&rt.ID, &rt.UserID, &rt.TokenHash, &rt.UserAgent, &rt.IP,
		&rt.ExpiresAt, &rt.RevokedAt, &rt.RotatedFrom, &rt.CreatedAt,
	)
	if err != nil {
		return nil, mapErr(err)
	}
	return rt, nil
}

// CreateRefreshTokenParams adalah parameter pembuatan refresh token.
type CreateRefreshTokenParams struct {
	UserID      uuid.UUID
	TokenHash   string
	UserAgent   string
	IP          string
	ExpiresAt   time.Time
	RotatedFrom *uuid.UUID
}

// CreateRefreshTokenTx menyimpan refresh token di dalam transaksi.
func (s *Store) CreateRefreshTokenTx(ctx context.Context, tx pgx.Tx, p CreateRefreshTokenParams) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens
			(user_id, token_hash, user_agent, ip, expires_at, rotated_from)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		p.UserID, p.TokenHash, p.UserAgent, p.IP, p.ExpiresAt.UTC(), p.RotatedFrom)
	return err
}

// GetRefreshTokenByHash mengambil refresh token berdasarkan hash-nya.
func (s *Store) GetRefreshTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+refreshTokenColumns+` FROM refresh_tokens WHERE token_hash=$1`, hash)
	return scanRefreshToken(row)
}

// RevokeRefreshToken mencabut satu refresh token.
func (s *Store) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return err
}

// RevokeRefreshTokenTx mencabut satu refresh token di dalam transaksi.
func (s *Store) RevokeRefreshTokenTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return err
}

// RevokeAllUserTokens mencabut seluruh refresh token milik user,
// dipakai saat logout-all, ganti password, atau deteksi penyalahgunaan token.
func (s *Store) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

// ListActiveRefreshTokens mengembalikan sesi aktif (belum dicabut & belum kedaluwarsa).
func (s *Store) ListActiveRefreshTokens(ctx context.Context, userID uuid.UUID) ([]models.RefreshToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+refreshTokenColumns+` FROM refresh_tokens
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.RefreshToken{}
	for rows.Next() {
		rt, err := scanRefreshToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rt)
	}
	return out, rows.Err()
}

// PurgeExpiredRefreshTokens menghapus token yang sudah kedaluwarsa/dicabut
// lebih tua dari rentang retensi (dipakai job retensi F9).
func (s *Store) PurgeExpiredRefreshTokens(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM refresh_tokens
		WHERE revoked_at IS NOT NULL
		  AND revoked_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
