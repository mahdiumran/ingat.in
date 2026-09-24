-- +goose Up

-- ============================================================================
-- F24 — Jenis Daily Task + pembuatan tiket otomatis dari tugas harian.
--
-- 1. Kelompok master data `daily_task_type` (Jenis Daily Task). Tiap jenis
--    membawa meta `dapat_membuat_tiket` (bool) yang menentukan apakah task
--    berjenis tersebut boleh dieskalasi menjadi tiket gangguan.
-- 2. task_details.daily_task_type — kode jenis yang dipilih operator.
-- 3. task_details.result_status — hasil penyelesaian: '' (belum), 'normal',
--    atau 'bermasalah' (memicu pembuatan tiket).
-- 4. daily_ticket_links — tautan idempotent task (daily_task) -> tiket
--    (incident/request) yang dibuat dari tugas tersebut.
-- ============================================================================

-- 1) Kelompok master data "Jenis Daily Task".
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('daily_task_type', 'Jenis Daily Task', 'Jenis pekerjaan harian NOC; sebagian dapat memicu pembuatan tiket gangguan.', 'playlist_add_check', TRUE, 40)
ON CONFLICT (kind) DO NOTHING;

-- Entri awal jenis (meta can_create_ticket disimpan pada meta_json).
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('daily_task_type', 'PENGECEKAN_GANGGUAN', 'Pengecekan Gangguan', 'Pengecekan rutin indikasi gangguan jaringan/layanan.', '{"dapat_membuat_tiket":true}'::jsonb,  10, 'seed'),
    ('daily_task_type', 'BACKUP',              'Backup',              'Backup konfigurasi/data perangkat.',                  '{"dapat_membuat_tiket":true}'::jsonb,  20, 'seed'),
    ('daily_task_type', 'MONITORING',          'Monitoring',          'Pemantauan perangkat/link.',                          '{"dapat_membuat_tiket":true}'::jsonb,  30, 'seed'),
    ('daily_task_type', 'ADMINISTRASI',        'Administrasi',        'Pekerjaan administratif (laporan, dokumentasi).',      '{"dapat_membuat_tiket":false}'::jsonb, 40, 'seed'),
    ('daily_task_type', 'LAINNYA',             'Lainnya',             'Pekerjaan lain di luar kategori di atas.',            '{"dapat_membuat_tiket":false}'::jsonb, 50, 'seed')
ON CONFLICT (kind, code) DO UPDATE
    SET label = EXCLUDED.label, description = EXCLUDED.description, meta_json = EXCLUDED.meta_json;

-- 2 & 3) Kolom jenis + hasil penyelesaian pada task_details.
ALTER TABLE task_details ADD COLUMN IF NOT EXISTS daily_task_type TEXT NOT NULL DEFAULT '';
ALTER TABLE task_details ADD COLUMN IF NOT EXISTS result_status   TEXT NOT NULL DEFAULT '';

-- 4) Tautan task -> tiket (mencegah duplikasi pembuatan tiket dari 1 task).
CREATE TABLE IF NOT EXISTS daily_ticket_links (
    daily_task_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    ticket_id     UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    created_by    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (daily_task_id)
);
CREATE INDEX IF NOT EXISTS idx_daily_ticket_links_ticket ON daily_ticket_links(ticket_id);

-- +goose Down

DROP TABLE IF EXISTS daily_ticket_links;
ALTER TABLE task_details DROP COLUMN IF EXISTS result_status;
ALTER TABLE task_details DROP COLUMN IF EXISTS daily_task_type;
DELETE FROM master_data WHERE kind = 'daily_task_type';
DELETE FROM master_data_kinds WHERE kind = 'daily_task_type';
