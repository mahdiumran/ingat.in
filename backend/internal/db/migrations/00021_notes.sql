-- +goose Up

-- ============================================================================
-- F26 — Catatan (Sticky Notes) dengan tim pemilik + berbagi antar tim.
--
-- 1. Tabel `notes`: catatan mandiri (tidak menempel ke work item) dengan
--    flag visibilitas internal/eksternal, warna, pin, dan tim pemilik.
-- 2. Tabel `note_shares`: daftar tim yang boleh melihat sebuah catatan
--    (many-to-many notes <-> teams).
-- 3. Izin RBAC baru: notes.view, notes.write.
-- ============================================================================

CREATE TABLE IF NOT EXISTS notes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title               TEXT NOT NULL DEFAULT '',
    body                TEXT NOT NULL DEFAULT '',
    visibility          TEXT NOT NULL DEFAULT 'internal'
                        CHECK (visibility IN ('internal','eksternal')),
    owner_team_id       UUID REFERENCES teams(id) ON DELETE SET NULL,
    color               TEXT NOT NULL DEFAULT '',
    pinned              BOOLEAN NOT NULL DEFAULT FALSE,
    created_by          TEXT NOT NULL DEFAULT '',
    updated_by_username TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_notes_owner_updated
    ON notes (owner_team_id, pinned DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS note_shares (
    note_id    UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (note_id, team_id)
);

CREATE INDEX IF NOT EXISTS ix_note_shares_team ON note_shares (team_id);

-- Izin RBAC awal (admin selalu boleh via is_super).
INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('manager', 'notes.view',  TRUE,  'seed'),
    ('manager', 'notes.write', TRUE,  'seed'),
    ('spv',     'notes.view',  TRUE,  'seed'),
    ('spv',     'notes.write', TRUE,  'seed'),
    ('owner',   'notes.view',  TRUE,  'seed'),
    ('owner',   'notes.write', FALSE, 'seed'),
    ('noc',     'notes.view',  TRUE,  'seed'),
    ('noc',     'notes.write', TRUE,  'seed'),
    ('agent',   'notes.view',  TRUE,  'seed'),
    ('agent',   'notes.write', TRUE,  'seed'),
    ('sales',   'notes.view',  TRUE,  'seed'),
    ('sales',   'notes.write', TRUE,  'seed'),
    ('viewer',  'notes.view',  FALSE, 'seed'),
    ('viewer',  'notes.write', FALSE, 'seed'),
    ('customer','notes.view',  FALSE, 'seed'),
    ('customer','notes.write', FALSE, 'seed')
ON CONFLICT (role, action) DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE action IN ('notes.view', 'notes.write');
DROP TABLE IF EXISTS note_shares;
DROP TABLE IF EXISTS notes;
