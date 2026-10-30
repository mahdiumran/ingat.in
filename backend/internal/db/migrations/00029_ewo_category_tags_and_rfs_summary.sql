-- +goose Up

-- ============================================================================
-- F45 — Peningkatan Aktivasi/EWO (RFS).
--
-- 1. Master data `ewo_category` — kategori permintaan Aktivasi/EWO
--    (Aktivasi, Downgrade, Perubahan Layanan). Dipakai sebagai pilihan pada
--    form RFS; label disalin (snapshot) ke rfs_details.
-- 2. Master data `ewo_tag` — daftar tag sah untuk RFS. work_items.tags tetap
--    menyimpan nilai tag; hanya divalidasi terhadap daftar aktif ini.
-- 3. rfs_details.category_code + category_label — snapshot kategori RFS agar
--    riwayat tetap terbaca walau entri master data diubah/dinonaktifkan.
--
-- Tidak ada perubahan destruktif: seluruh INSERT idempoten dan ALTER memakai
-- IF NOT EXISTS dengan DEFAULT sehingga data lama tetap valid.
-- ============================================================================

-- 1) Kelompok master data.
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('ewo_category', 'Kategori EWO', 'Kategori permintaan Aktivasi/EWO, mis. Aktivasi, Downgrade, Perubahan Layanan.', 'event_available', TRUE, 62),
    ('ewo_tag',      'Tag EWO',      'Daftar tag yang sah untuk Aktivasi/EWO (RFS).',                                   'sell',            TRUE, 63)
ON CONFLICT (kind) DO NOTHING;

-- 2) Kategori EWO awal. Code kanonik huruf besar; label teks tampilan.
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('ewo_category', 'ACTIVATION',     'Aktivasi',           'Aktivasi / Activation layanan baru.',              10, 'seed'),
    ('ewo_category', 'DOWNGRADE',      'Downgrade',          'Penurunan kapasitas / layanan (Downgrade).',       20, 'seed'),
    ('ewo_category', 'SERVICE_CHANGE', 'Perubahan Layanan',  'Perubahan paket/layanan (Service Change).',        30, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 3) Tag EWO awal (work_items.tags divalidasi terhadap daftar aktif ini).
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('ewo_tag', 'new_install',    'New Install',   'Pemasangan baru.',              10, 'seed'),
    ('ewo_tag', 'upgrade',        'Upgrade',       'Peningkatan kapasitas.',        20, 'seed'),
    ('ewo_tag', 'downgrade',      'Downgrade',     'Penurunan kapasitas.',          30, 'seed'),
    ('ewo_tag', 'change_service', 'Change Service','Perubahan layanan.',            40, 'seed'),
    ('ewo_tag', 'relocation',     'Relocation',    'Perpindahan lokasi/site.',      50, 'seed'),
    ('ewo_tag', 'survey',         'Survey',        'Survei lokasi sebelum aktivasi.',60, 'seed'),
    ('ewo_tag', 'maintenance',    'Maintenance',   'Terkait pemeliharaan.',         70, 'seed'),
    ('ewo_tag', 'urgent',         'Urgent',        'Butuh penanganan segera.',      80, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 4) Snapshot kategori pada rfs_details.
ALTER TABLE rfs_details ADD COLUMN IF NOT EXISTS category_code  TEXT NOT NULL DEFAULT '';
ALTER TABLE rfs_details ADD COLUMN IF NOT EXISTS category_label TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE rfs_details DROP COLUMN IF EXISTS category_label;
ALTER TABLE rfs_details DROP COLUMN IF EXISTS category_code;
DELETE FROM master_data WHERE kind = 'ewo_tag';
DELETE FROM master_data WHERE kind = 'ewo_category';
DELETE FROM master_data_kinds WHERE kind IN ('ewo_tag', 'ewo_category');
