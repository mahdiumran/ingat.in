package repository

import (
	"context"
	"time"

	"ingatin/backend/internal/models"
)

// AuditLogParams adalah parameter pencatatan audit.
type AuditLogParams struct {
	Username   string
	Action     string
	EntityType string
	EntityID   string
	Detail     map[string]any
	IP         string
	Success    bool
}

// CreateAuditLog mencatat satu aksi ke audit_logs.
//
// Pencatatan audit tidak boleh menggagalkan operasi utama, jadi pemanggil
// dianjurkan mengabaikan error (lihat services/audit).
func (s *Store) CreateAuditLog(ctx context.Context, p AuditLogParams) error {
	detail := p.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (username, action, entity_type, entity_id, detail, ip, success)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.Username, p.Action, p.EntityType, p.EntityID, detail, p.IP, p.Success)
	return err
}

// ListAuditLogsParams adalah filter daftar audit.
type ListAuditLogsParams struct {
	Username   string
	Action     string
	EntityType string
	Success    *bool
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// ListAuditLogs mengambil daftar audit dengan filter.
func (s *Store) ListAuditLogs(ctx context.Context, p ListAuditLogsParams) ([]models.AuditLog, int, error) {
	where := []string{"TRUE"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + itoa(len(args))
	}

	if p.Username != "" {
		where = append(where, "username = "+arg(p.Username))
	}
	if p.Action != "" {
		where = append(where, "action = "+arg(p.Action))
	}
	if p.EntityType != "" {
		where = append(where, "entity_type = "+arg(p.EntityType))
	}
	if p.Success != nil {
		where = append(where, "success = "+arg(*p.Success))
	}
	if p.From != nil {
		where = append(where, "created_at >= "+arg(p.From.UTC()))
	}
	if p.To != nil {
		where = append(where, "created_at <= "+arg(p.To.UTC()))
	}

	clause := " WHERE " + joinAnd(where)

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := p.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	query := `SELECT id, username, action, entity_type, entity_id, detail, ip, success, created_at
	          FROM audit_logs` + clause +
		" ORDER BY created_at DESC LIMIT " + arg(limit) + " OFFSET " + arg(offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []models.AuditLog{}
	for rows.Next() {
		var a models.AuditLog
		if err := rows.Scan(&a.ID, &a.Username, &a.Action, &a.EntityType, &a.EntityID,
			&a.Detail, &a.IP, &a.Success, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// PurgeAuditLogs menghapus audit lebih tua dari batas retensi (job F9).
func (s *Store) PurgeAuditLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audit_logs WHERE created_at < $1`, olderThan.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
