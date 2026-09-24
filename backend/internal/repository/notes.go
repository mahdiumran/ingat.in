package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F26 — Catatan (sticky notes)
   --------------------------------------------------------------------------- */

const noteColumns = `n.id, n.title, n.body, n.visibility, n.owner_team_id, n.color,
	n.pinned, n.created_by, n.updated_by_username, n.created_at, n.updated_at`

// ListNotesParams adalah filter daftar catatan.
type ListNotesParams struct {
	Visibility string // "" | internal | eksternal
	Search     string
	TeamScope  *uuid.UUID // batasi ke tim ini (pemilik atau di-share); nil = tanpa batas tim
	Limit      int
}

// ListNotes mengambil catatan dengan filter visibilitas + scope tim.
func (s *Store) ListNotes(ctx context.Context, p ListNotesParams) ([]models.Note, error) {
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 200
	}

	where := "WHERE TRUE"
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where += " AND " + cond
	}
	if p.Visibility != "" {
		add("n.visibility = $"+itoa(len(args)+1), p.Visibility)
	}
	if p.Search != "" {
		add("(n.title ILIKE '%'||$"+itoa(len(args)+1)+"||'%' OR n.body ILIKE '%'||$"+itoa(len(args)+1)+"||'%')", p.Search)
	}
	if p.TeamScope != nil {
		add("(n.owner_team_id = $"+itoa(len(args)+1)+
			" OR EXISTS (SELECT 1 FROM note_shares ns WHERE ns.note_id = n.id AND ns.team_id = $"+itoa(len(args)+1)+"))",
			*p.TeamScope)
	}
	args = append(args, p.Limit)

	q := `SELECT ` + noteColumns + ` FROM notes n ` + where +
		` ORDER BY n.pinned DESC, n.updated_at DESC LIMIT $` + itoa(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Lampirkan daftar tim yang di-share (hindari N+1).
	shares, err := s.listAllNoteShares(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].SharedTeams = shares[out[i].ID]
		ids := make([]uuid.UUID, 0, len(out[i].SharedTeams))
		for _, st := range out[i].SharedTeams {
			ids = append(ids, st.TeamID)
		}
		out[i].SharedTeamIDs = ids
	}
	return out, nil
}

// GetNote mengambil satu catatan (beserta tim yang di-share).
func (s *Store) GetNote(ctx context.Context, id uuid.UUID) (*models.Note, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+noteColumns+
		` FROM notes n WHERE n.id=$1`, id)
	n, err := scanNote(row)
	if err != nil {
		return nil, err
	}
	shares, err := s.ListNoteShares(ctx, id)
	if err != nil {
		return nil, err
	}
	n.SharedTeams = shares
	n.SharedTeamIDs = make([]uuid.UUID, 0, len(shares))
	for _, st := range shares {
		n.SharedTeamIDs = append(n.SharedTeamIDs, st.TeamID)
	}
	return n, nil
}

// CreateNoteParams adalah parameter pembuatan catatan.
type CreateNoteParams struct {
	Title       string
	Body        string
	Visibility  string
	OwnerTeamID *uuid.UUID
	Color       string
	Pinned      bool
	CreatedBy   string
}

// CreateNote membuat catatan baru.
func (s *Store) CreateNote(ctx context.Context, p CreateNoteParams) (*models.Note, error) {
	if p.Visibility == "" {
		p.Visibility = models.NoteVisibilityInternal
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO notes (title, body, visibility, owner_team_id, color, pinned, created_by, updated_by_username)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
		RETURNING id, title, body, visibility, owner_team_id, color,
		          pinned, created_by, updated_by_username, created_at, updated_at`,
		p.Title, p.Body, p.Visibility, p.OwnerTeamID, p.Color, p.Pinned, p.CreatedBy)

	n := &models.Note{}
	if err := row.Scan(&n.ID, &n.Title, &n.Body, &n.Visibility, &n.OwnerTeamID, &n.Color,
		&n.Pinned, &n.CreatedBy, &n.UpdatedByUsername, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return n, nil
}

// UpdateNoteParams adalah parameter update catatan (parsial).
type UpdateNoteParams struct {
	Title       *string
	Body        *string
	Visibility  *string
	OwnerTeamID *uuid.UUID
	SetOwner    bool
	Color       *string
	Pinned      *bool
	UpdatedBy   string
}

// UpdateNote memperbarui catatan.
func (s *Store) UpdateNote(ctx context.Context, id uuid.UUID, p UpdateNoteParams) (*models.Note, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE notes SET
			title               = COALESCE($2, title),
			body                = COALESCE($3, body),
			visibility          = COALESCE($4, visibility),
			owner_team_id       = CASE WHEN $5::boolean THEN $6 ELSE owner_team_id END,
			color               = COALESCE($7, color),
			pinned              = COALESCE($8, pinned),
			updated_by_username = $9,
			updated_at          = now()
		WHERE id = $1
		RETURNING id, title, body, visibility, owner_team_id, color,
		          pinned, created_by, updated_by_username, created_at, updated_at`,
		id, p.Title, p.Body, p.Visibility, p.SetOwner, p.OwnerTeamID, p.Color, p.Pinned, p.UpdatedBy)

	n := &models.Note{}
	if err := row.Scan(&n.ID, &n.Title, &n.Body, &n.Visibility, &n.OwnerTeamID, &n.Color,
		&n.Pinned, &n.CreatedBy, &n.UpdatedByUsername, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return n, nil
}

// DeleteNote menghapus catatan (share ikut terhapus via CASCADE).
func (s *Store) DeleteNote(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notes WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoteShares mengganti daftar tim yang di-share ke sebuah catatan.
func (s *Store) SetNoteShares(ctx context.Context, noteID uuid.UUID, teamIDs []uuid.UUID, actor string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM note_shares WHERE note_id=$1`, noteID); err != nil {
		return err
	}
	for _, tid := range teamIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO note_shares (note_id, team_id, created_by)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, noteID, tid, actor); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListNoteShares mengambil daftar tim yang di-share ke sebuah catatan.
func (s *Store) ListNoteShares(ctx context.Context, noteID uuid.UUID) ([]models.NoteSharedTeam, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ns.team_id, t.name
		FROM note_shares ns JOIN teams t ON t.id = ns.team_id
		WHERE ns.note_id = $1 ORDER BY t.name`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NoteSharedTeam{}
	for rows.Next() {
		var st models.NoteSharedTeam
		if err := rows.Scan(&st.TeamID, &st.Name); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// listAllNoteShares mengambil seluruh share lalu dikelompokkan per catatan.
func (s *Store) listAllNoteShares(ctx context.Context) (map[uuid.UUID][]models.NoteSharedTeam, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ns.note_id, ns.team_id, t.name
		FROM note_shares ns JOIN teams t ON t.id = ns.team_id
		ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[uuid.UUID][]models.NoteSharedTeam{}
	for rows.Next() {
		var noteID uuid.UUID
		var st models.NoteSharedTeam
		if err := rows.Scan(&noteID, &st.TeamID, &st.Name); err != nil {
			return nil, err
		}
		out[noteID] = append(out[noteID], st)
	}
	return out, rows.Err()
}

// scanNote memindai satu baris catatan dengan prefix alias `n.`.
func scanNote(row pgx.Row) (*models.Note, error) {
	n := &models.Note{}
	if err := row.Scan(&n.ID, &n.Title, &n.Body, &n.Visibility, &n.OwnerTeamID, &n.Color,
		&n.Pinned, &n.CreatedBy, &n.UpdatedByUsername, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return n, nil
}
