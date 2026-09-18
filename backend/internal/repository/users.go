package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

const userColumns = `
	id, username, email, full_name, password_hash, role, team_id, organization_id,
	telegram_chat_id, wa_number, token_version, is_active, last_login_at,
	created_at, updated_at`

func scanUser(row pgx.Row) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.FullName, &u.PasswordHash, &u.Role,
		&u.TeamID, &u.OrganizationID, &u.TelegramChatID, &u.WANumber,
		&u.TokenVersion, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, mapErr(err)
	}
	return u, nil
}

// GetUserByUsername mengambil user berdasarkan username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username=$1`, username)
	return scanUser(row)
}

// GetUserByID mengambil user berdasarkan ID.
func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id)
	return scanUser(row)
}

// UserExists melaporkan apakah username sudah terdaftar.
func (s *Store) UserExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)`, username).Scan(&exists)
	return exists, err
}

// CreateAdminUser membuat user admin awal (dipakai bootstrap).
func (s *Store) CreateAdminUser(ctx context.Context, username, email, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (username, email, password_hash, role, is_active)
		VALUES ($1, $2, $3, 'admin', TRUE)
		ON CONFLICT (username) DO NOTHING`, username, email, passwordHash)
	if err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}
	return nil
}

// ListUsers mengembalikan seluruh user (tanpa hash password).
func (s *Store) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// TouchLastLogin memperbarui waktu login terakhir.
func (s *Store) TouchLastLogin(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id=$1`, userID)
	return err
}

// BumpTokenVersion menaikkan versi token user (mencabut seluruh akses token lama).
func (s *Store) BumpTokenVersion(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET token_version = token_version + 1, updated_at = now() WHERE id=$1`, userID)
	return err
}

// CountUsersByRole mengembalikan jumlah user per role.
func (s *Store) CountUsersByRole(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT role, count(*) FROM users WHERE is_active GROUP BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return nil, err
		}
		out[role] = n
	}
	return out, rows.Err()
}

// UpdateUserPassword mengganti hash password user.
func (s *Store) UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, userID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateUserParams adalah parameter pembuatan user.
type CreateUserParams struct {
	Username       string
	Email          string
	FullName       string
	PasswordHash   string
	Role           string
	TeamID         *uuid.UUID
	OrganizationID *uuid.UUID
	TelegramChatID string
	WANumber       string
}

// CreateUser membuat user baru.
func (s *Store) CreateUser(ctx context.Context, p CreateUserParams) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users
			(username, email, full_name, password_hash, role, team_id, organization_id,
			 telegram_chat_id, wa_number)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+userColumns,
		p.Username, p.Email, p.FullName, p.PasswordHash, p.Role,
		p.TeamID, p.OrganizationID, p.TelegramChatID, p.WANumber)

	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	return u, nil
}

// UpdateUserParams adalah parameter update user (parsial).
type UpdateUserParams struct {
	Email          *string
	FullName       *string
	Role           *string
	TeamID         *uuid.UUID
	OrganizationID *uuid.UUID
	TelegramChatID *string
	WANumber       *string
	IsActive       *bool
}

// UpdateUser memperbarui sebagian field user.
func (s *Store) UpdateUser(ctx context.Context, userID uuid.UUID, p UpdateUserParams) (*models.User, error) {
	allowed := map[string]any{}
	if p.Email != nil {
		allowed["email"] = *p.Email
	}
	if p.FullName != nil {
		allowed["full_name"] = *p.FullName
	}
	if p.Role != nil {
		allowed["role"] = *p.Role
	}
	if p.TeamID != nil {
		allowed["team_id"] = *p.TeamID
	}
	if p.OrganizationID != nil {
		allowed["organization_id"] = *p.OrganizationID
	}
	if p.TelegramChatID != nil {
		allowed["telegram_chat_id"] = *p.TelegramChatID
	}
	if p.WANumber != nil {
		allowed["wa_number"] = *p.WANumber
	}
	if p.IsActive != nil {
		allowed["is_active"] = *p.IsActive
	}

	if len(allowed) == 0 {
		return s.GetUserByID(ctx, userID)
	}

	cols := make([]string, 0, len(allowed))
	args := make([]any, 0, len(allowed)+1)
	i := 1
	for col, val := range allowed {
		cols = append(cols, col+" = $"+itoa(i))
		args = append(args, val)
		i++
	}
	args = append(args, userID)

	query := "UPDATE users SET " + joinComma(cols) + ", updated_at = now() WHERE id = $" + itoa(i) +
		" RETURNING " + userColumns

	u, err := scanUser(s.pool.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	return u, nil
}

// DeleteUser menghapus user. Sesi terkait ikut terhapus karena FK CASCADE.
func (s *Store) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// helper kecil lokal agar tidak menarik dependensi tambahan.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
