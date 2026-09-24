-- +goose Up

-- ============================================================================
-- F22 — RBAC dinamis, kategori tim, dan ringkasan task harian.
--
-- 1. Tabel `roles`: daftar peran TIDAK lagi hardcoded. Operator dapat
--    menambah/mengubah/menghapus peran dari panel sesuai kebutuhan.
--    `admin` diperlakukan sebagai super user (selalu dapat seluruh izin).
-- 2. Menghapus CHECK constraint role pada `users` dan `role_permissions`
--    agar dapat menampung peran baru (mis. manager, spv, owner).
-- 3. `teams.category` + master data kind `team_category` untuk pengelompokan
--    tim sesuai kebutuhan.
-- 4. Settings untuk ringkasan tugas harian (trigger mode on_change/interval).
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1. Registri peran dinamis
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    role        TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- is_super: super user (selalu dapat seluruh izin; tidak dapat dicabut).
    is_super    BOOLEAN NOT NULL DEFAULT FALSE,
    -- is_system: peran bawaan; tidak dapat dihapus dari panel.
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    rank        INT NOT NULL DEFAULT 100,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO roles (role, label, description, is_super, is_system, rank) VALUES
    ('admin',    'Administrator (Super User)', 'Akses penuh; selalu memiliki seluruh izin.',                     TRUE,  TRUE, 10),
    ('manager',  'Manager',                    'Manajerial — KPI/SLA, pengguna, persetujuan.',                   FALSE, TRUE, 20),
    ('spv',      'Supervisor',                 'Pengawas operasional — KPI/SLA dan penugasan.',                  FALSE, TRUE, 30),
    ('owner',    'Owner',                      'Pemilik/pimpinan — visibilitas KPI/SLA penuh.',                  FALSE, TRUE, 35),
    ('noc',      'NOC',                        'Kelola reminder, RFS, target, provider, dan tiket.',             FALSE, TRUE, 40),
    ('agent',    'Agent',                      'Menangani work item & tiket.',                                   FALSE, TRUE, 50),
    ('sales',    'Sales',                      'Input data RFS dan pelanggan.',                                  FALSE, TRUE, 60),
    ('viewer',   'Viewer',                     'Hanya membaca.',                                                 FALSE, TRUE, 70),
    ('customer', 'Customer',                   'Akses terbatas untuk pelanggan eksternal.',                      FALSE, TRUE, 80)
ON CONFLICT (role) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. Longgarkan CHECK constraint agar peran dinamis diterima
-- ---------------------------------------------------------------------------
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE role_permissions DROP CONSTRAINT IF EXISTS role_permissions_role_check;

-- ---------------------------------------------------------------------------
-- 3. Kategori tim
-- ---------------------------------------------------------------------------
ALTER TABLE teams ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '';

INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('team_category', 'Kategori Tim', 'Pengelompokan tim sesuai kebutuhan (NOC, Sales, Field, dll).', 'diversity_3', TRUE, 5)
ON CONFLICT (kind) DO NOTHING;

INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('team_category', 'NOC',      'NOC',      'Network Operations Center.',        10, 'seed'),
    ('team_category', 'SALES',    'Sales',    'Tim penjualan.',                    20, 'seed'),
    ('team_category', 'FIELD',    'Field',    'Tim lapangan / instalasi.',         30, 'seed'),
    ('team_category', 'SUPPORT',  'Support',  'Tim dukungan teknis.',              40, 'seed'),
    ('team_category', 'MANAGEMENT','Management','Tim manajemen / pimpinan.',       50, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- Terapkan kategori awal pada tim bawaan (idempotent).
UPDATE teams SET category = 'NOC'   WHERE category = '' AND name = 'NOC';
UPDATE teams SET category = 'SALES' WHERE category = '' AND name = 'Sales';
UPDATE teams SET category = 'FIELD' WHERE category = '' AND name = 'Field';

-- ---------------------------------------------------------------------------
-- 4. Izin awal peran manajerial baru (mencerminkan peran pengelola).
--    admin tetap selalu boleh.
-- ---------------------------------------------------------------------------
INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('manager', 'items.write',      TRUE,  'seed'),
    ('manager', 'providers.write',  FALSE, 'seed'),
    ('manager', 'masterdata.write', FALSE, 'seed'),
    ('manager', 'users.write',      TRUE,  'seed'),
    ('manager', 'teams.write',      TRUE,  'seed'),
    ('manager', 'kpi.view',         TRUE,  'seed'),
    ('spv',     'items.write',      TRUE,  'seed'),
    ('spv',     'providers.write',  FALSE, 'seed'),
    ('spv',     'masterdata.write', FALSE, 'seed'),
    ('spv',     'users.write',      FALSE, 'seed'),
    ('spv',     'kpi.view',         TRUE,  'seed'),
    ('owner',   'items.write',      FALSE, 'seed'),
    ('owner',   'providers.write',  FALSE, 'seed'),
    ('owner',   'masterdata.write', FALSE, 'seed'),
    ('owner',   'users.write',      FALSE, 'seed'),
    ('owner',   'kpi.view',         TRUE,  'seed'),
    ('admin',   'kpi.view',         TRUE,  'seed'),
    -- Peran lama: hanya yang berhak pengelolaan yang melihat KPI.
    ('noc',     'kpi.view',         FALSE, 'seed'),
    ('agent',   'kpi.view',         FALSE, 'seed'),
    ('sales',   'kpi.view',         FALSE, 'seed'),
    ('viewer',  'kpi.view',         FALSE, 'seed')
ON CONFLICT (role, action) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 5. Pengaturan ringkasan tugas harian
--    summary_mode : 'on_change' | 'interval' | 'off'
-- ---------------------------------------------------------------------------
INSERT INTO settings (key, value, updated_by) VALUES
    ('daily_task.summary_mode',         '"interval"'::jsonb, 'system'),
    ('daily_task.summary_interval_min', '60'::jsonb,          'system')
ON CONFLICT (key) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 6. Template notifikasi ringkasan tugas harian (dapat diedit operator).
-- ---------------------------------------------------------------------------
INSERT INTO notification_templates (key, item_type, channel, subject_tpl, body_tpl, severity, description) VALUES
    ('DAILY_SUMMARY', 'daily_task', 'telegram',
     'Ringkasan Tugas Harian',
     E'📊 *RINGKASAN TUGAS HARI INI*\n{{.CreatedAt}}\n\n{{.Description}}\n\n{{.Notes}}',
     'info', 'Ringkasan harian task pending/sedang dikerjakan/selesai (otomatis).'),
    ('DAILY_SUMMARY', 'daily_task', 'whatsapp',
     '',
     E'📊 *RINGKASAN TUGAS HARI INI*\n{{.CreatedAt}}\n\n{{.Description}}\n\n{{.Notes}}',
     'info', 'Ringkasan harian task pending/sedang dikerjakan/selesai (otomatis).')
ON CONFLICT (key, channel) DO NOTHING;

-- +goose Down

DELETE FROM notification_templates WHERE key = 'DAILY_SUMMARY';
DELETE FROM settings WHERE key IN ('daily_task.summary_mode', 'daily_task.summary_interval_min');
DELETE FROM master_data WHERE kind = 'team_category';
DELETE FROM master_data_kinds WHERE kind = 'team_category';
ALTER TABLE teams DROP COLUMN IF EXISTS category;
DELETE FROM role_permissions WHERE action = 'kpi.view';
DROP TABLE IF EXISTS roles;
