-- +goose Up

-- ============================================================================
-- F23 — Menu "Aktivasi / EWO".
--
-- RFS (item_type='rfs') ditampilkan sebagai menu tersendiri "Aktivasi / EWO"
-- dengan field khusus: Nama Customer, Product (jenis paket), Priority,
-- Bandwidth, dan Tag. Migrasi ini:
--   1. Menambah kelompok master data `service_package` (Jenis Paket).
--   2. Menambah preset tag Aktivasi/EWO (change_service, upgrade, new_install,
--      request, urgent) pada kind `tag_color`.
--   3. Menambah kolom penanganan pada rfs_details (issue_found,
--      troubleshooting, action_solution) — dipakai modal Troubleshoot.
-- ============================================================================

-- 1) Kelompok master data "Jenis Paket".
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('service_package', 'Jenis Paket', 'Jenis paket layanan untuk Aktivasi/EWO, mis. Internet + Metro, Metro, CDN.', 'lan', TRUE, 35)
ON CONFLICT (kind) DO NOTHING;

-- Entri awal jenis paket (dapat diubah/ditambah dari panel Master Data).
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('service_package', 'INTERNET_METRO', 'Internet + Metro', 'Internet dengan jalur Metro Ethernet.',        10, 'seed'),
    ('service_package', 'METRO',          'Metro',            'Layanan Metro Ethernet saja.',                 20, 'seed'),
    ('service_package', 'INTERNET_ONLY',  'Internet Only',    'Layanan internet saja.',                       30, 'seed'),
    ('service_package', 'CDN',            'CDN',              'Content Delivery Network.',                    40, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2) Preset tag Aktivasi/EWO (warna mengikuti skema yang sudah ada).
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('tag_color', 'change_service', 'Change Service', 'Perubahan layanan.',        '{"color":"blue"}'::jsonb,   200, 'seed'),
    ('tag_color', 'upgrade',        'Upgrade',        'Peningkatan kapasitas.',    '{"color":"purple"}'::jsonb, 201, 'seed'),
    ('tag_color', 'new_install',    'New',            'Pemasangan baru.',          '{"color":"green"}'::jsonb,  202, 'seed'),
    ('tag_color', 'request',        'Request',        'Permintaan layanan.',       '{"color":"yellow"}'::jsonb, 203, 'seed'),
    ('tag_color', 'urgent',         'Urgent',         'Butuh penanganan segera.',  '{"color":"red"}'::jsonb,    204, 'seed')
ON CONFLICT (kind, code) DO UPDATE
    SET label = EXCLUDED.label, description = EXCLUDED.description, meta_json = EXCLUDED.meta_json;

-- 3) Kolom penanganan pada rfs_details (modal Troubleshoot).
ALTER TABLE rfs_details ADD COLUMN IF NOT EXISTS issue_found     TEXT NOT NULL DEFAULT '';
ALTER TABLE rfs_details ADD COLUMN IF NOT EXISTS troubleshooting TEXT NOT NULL DEFAULT '';
ALTER TABLE rfs_details ADD COLUMN IF NOT EXISTS action_solution TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE rfs_details DROP COLUMN IF EXISTS issue_found;
ALTER TABLE rfs_details DROP COLUMN IF EXISTS troubleshooting;
ALTER TABLE rfs_details DROP COLUMN IF EXISTS action_solution;
DELETE FROM master_data WHERE kind = 'service_package';
DELETE FROM master_data_kinds WHERE kind = 'service_package';
DELETE FROM master_data WHERE kind = 'tag_color' AND code IN ('change_service','upgrade','new_install','request','urgent');
