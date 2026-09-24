package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F20 — Collaborator ("ikut menangani")
   --------------------------------------------------------------------------- */

// AddCollaborator menambahkan seorang user ke daftar penangan tiket.
//
// Idempoten: menambahkan ulang tidak menghasilkan error (ON CONFLICT DO NOTHING).
func (s *Store) AddCollaborator(ctx context.Context, workItemID uuid.UUID, username, addedBy string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO ticket_collaborators (work_item_id, username, added_by)
		VALUES ($1,$2,$3)
		ON CONFLICT (work_item_id, username) DO NOTHING`,
		workItemID, strings.TrimSpace(username), addedBy)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RemoveCollaborator menghapus seorang user dari daftar penangan tiket.
func (s *Store) RemoveCollaborator(ctx context.Context, workItemID uuid.UUID, username string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM ticket_collaborators
		WHERE work_item_id=$1 AND lower(username)=lower($2)`,
		workItemID, strings.TrimSpace(username))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListCollaborators mengambil daftar penangan sebuah tiket.
func (s *Store) ListCollaborators(ctx context.Context, workItemID uuid.UUID) ([]models.Collaborator, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, work_item_id, username, added_by, created_at
		FROM ticket_collaborators WHERE work_item_id=$1 ORDER BY created_at`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Collaborator{}
	for rows.Next() {
		var c models.Collaborator
		if err := rows.Scan(&c.ID, &c.WorkItemID, &c.Username, &c.AddedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// IsCollaborator melaporkan apakah username termasuk penangan tiket.
func (s *Store) IsCollaborator(ctx context.Context, workItemID uuid.UUID, username string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM ticket_collaborators
		WHERE work_item_id=$1 AND lower(username)=lower($2)`,
		workItemID, strings.TrimSpace(username)).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetWorkItemOwner menetapkan owner (penanggung jawab utama) sebuah item.
func (s *Store) SetWorkItemOwner(ctx context.Context, workItemID uuid.UUID, owner, by string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE work_items
		SET owner_username = $2, updated_by_username = NULLIF($3,''), updated_at = now()
		WHERE id = $1 AND NOT is_deleted`, workItemID, owner, by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
