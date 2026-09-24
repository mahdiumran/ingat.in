-- +goose Up

-- ============================================================================
-- F22 — Izin teams.write untuk manager.
--
-- Manager (admin di bawah super user) mengelola tim beserta kategorinya.
-- ============================================================================

INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('manager', 'teams.write', TRUE, 'seed')
ON CONFLICT (role, action) DO UPDATE
    SET allowed = EXCLUDED.allowed, updated_by = EXCLUDED.updated_by, updated_at = now();

-- +goose Down

DELETE FROM role_permissions WHERE role = 'manager' AND action = 'teams.write';
