package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F21 — Lampiran
   --------------------------------------------------------------------------- */

// CreateAttachmentParams adalah parameter penyimpanan lampiran.
type CreateAttachmentParams struct {
	WorkItemID uuid.UUID
	Filename   string
	StoredPath string
	SizeBytes  int64
	Mime       string
	UploadedBy string
}

// CreateAttachment menyimpan metadata lampiran.
func (s *Store) CreateAttachment(ctx context.Context, p CreateAttachmentParams) (*models.Attachment, bool, error) {
	a := &models.Attachment{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO attachments (work_item_id, filename, stored_path, size_bytes, mime, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, work_item_id, filename, stored_path, size_bytes, mime, uploaded_by, created_at`,
		p.WorkItemID, p.Filename, p.StoredPath, p.SizeBytes, p.Mime, p.UploadedBy,
	).Scan(&a.ID, &a.WorkItemID, &a.Filename, &a.StoredPath, &a.SizeBytes, &a.Mime, &a.UploadedBy, &a.CreatedAt)
	if err != nil {
		return nil, false, mapErr(err)
	}
	return a, true, nil
}

// GetAttachment mengambil metadata lampiran berdasarkan id.
func (s *Store) GetAttachment(ctx context.Context, id uuid.UUID) (*models.Attachment, error) {
	a := &models.Attachment{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, work_item_id, filename, stored_path, size_bytes, mime, uploaded_by, created_at
		FROM attachments WHERE id=$1`, id,
	).Scan(&a.ID, &a.WorkItemID, &a.Filename, &a.StoredPath, &a.SizeBytes, &a.Mime, &a.UploadedBy, &a.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return a, nil
}

// ListAttachments mengambil lampiran sebuah work item.
func (s *Store) ListAttachments(ctx context.Context, workItemID uuid.UUID) ([]models.Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, work_item_id, filename, stored_path, size_bytes, mime, uploaded_by, created_at
		FROM attachments WHERE work_item_id=$1 ORDER BY created_at`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Attachment{}
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.WorkItemID, &a.Filename, &a.StoredPath, &a.SizeBytes,
			&a.Mime, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAttachment menghapus baris lampiran.
func (s *Store) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM attachments WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAttachments menghitung lampiran per work item (untuk badge di daftar).
func (s *Store) CountAttachments(ctx context.Context, workItemID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE work_item_id=$1`, workItemID).Scan(&n)
	return n, err
}

var _ = pgx.ErrNoRows
