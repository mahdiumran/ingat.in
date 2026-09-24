-- +goose Up

-- ============================================================================
-- F32 — Izin "providers.view" untuk Notification Center.
--
-- Sebelumnya halaman Notification Center (Providers, Notification Targets,
-- Notification Templates) dapat DIBACA oleh semua role. Mulai sekarang akses
-- baca dibatasi ke admin (super user) + role yang diberi izin `providers.view`
-- dari dialog Role Permissions.
--
-- Escalation Policies TIDAK dibatasi (tetap dapat dilihat semua role).
--
-- Seed awal: admin, noc, dan adminnoc = TRUE (pengelola notifikasi);
-- role lain FALSE (dapat diaktifkan operator lewat Role Permissions).
-- ============================================================================

INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('admin',    'providers.view', TRUE,  'seed'),
    ('adminnoc', 'providers.view', TRUE,  'seed'),
    ('noc',      'providers.view', TRUE,  'seed'),
    ('manager',  'providers.view', FALSE, 'seed'),
    ('spv',      'providers.view', FALSE, 'seed'),
    ('owner',    'providers.view', FALSE, 'seed'),
    ('agent',    'providers.view', FALSE, 'seed'),
    ('sales',    'providers.view', FALSE, 'seed'),
    ('viewer',   'providers.view', FALSE, 'seed'),
    ('customer', 'providers.view', FALSE, 'seed')
ON CONFLICT (role, action) DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE action = 'providers.view';
