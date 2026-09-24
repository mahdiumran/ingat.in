-- +goose Up

-- ============================================================================
-- F29 — Snapshot penuh Master Data default.
--
-- Tujuan: menjadikan SELURUH master data yang dipakai aplikasi sebagai
-- konfigurasi default yang IKUT terpasang saat instalasi di server baru
-- (lewat `docker compose run --rm migrate`), tanpa perlu restore dump.
--
-- Catatan: sebagian besar baris sudah di-seed oleh migrasi 00005–00019.
-- Migrasi ini menyatukan semuanya (termasuk `ticket_tag` yang sebelumnya
-- hanya dibuat lewat panel) menjadi satu sumber kebenaran default. Seluruh
-- INSERT bersifat IDEMPOTEN (`ON CONFLICT DO NOTHING`) sehingga di database
-- yang sudah berjalan tidak ada perubahan apa pun — HANYA menambah baris
-- yang belum ada (mis. `ticket_tag`).
--
-- Bukan bagian dari master data (tidak disertakan, sesuai permintaan):
--   - konfigurasi Google Sheets
--   - provider Telegram/WhatsApp
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1) Kelompok master data (master_data_kinds) — label/icon/sort_order persis
--    seperti produksi. ON CONFLICT DO NOTHING: label yang sudah disesuaikan
--    operator TIDAK ditimpa.
-- ---------------------------------------------------------------------------
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('customer',          'Customer',             'Data pelanggan (personal/corporate) beserta kontak dan kapasitas.', 'person',              TRUE,  5),
    ('team_category',     'Kategori Tim',         'Pengelompokan tim sesuai kebutuhan (NOC, Sales, Field, dll).',      'diversity_3',         TRUE,  5),
    ('ticket_category',   'Kategori Tiket (EWO)', 'Kategori/klasifikasi tiket NOC, mis. EWO, Gangguan, Permintaan.',  'confirmation_number', TRUE, 10),
    ('incident_type',     'Jenis Gangguan',       'Jenis gangguan teknis, mis. fiber cut, power outage, BGP flap.',   'report_problem',      TRUE, 15),
    ('ticket_tag',        'Tag Tiket',            'Label bebas untuk pengelompokan tiket.',                            'sell',                TRUE, 20),
    ('tag_color',         'Warna Tag',            'Pemetaan warna untuk tag (merah, kuning, hijau, biru, ungu, oren).','palette',             TRUE, 25),
    ('product',           'Produk',               'Produk/layanan yang dijual, mis. Dedicated, Broadband.',           'inventory_2',         TRUE, 30),
    ('service_package',   'Jenis Paket',          'Jenis paket layanan untuk Aktivasi/EWO, mis. Internet + Metro, Metro, CDN.', 'lan',        TRUE, 35),
    ('daily_task_type',   'Jenis Daily Task',     'Jenis pekerjaan harian NOC; sebagian dapat memicu pembuatan tiket gangguan.', 'playlist_add_check', TRUE, 40),
    ('pop',               'Lokasi POP',           'Titik kehadiran (POP) untuk penugasan & pelaporan.',               'location_on',         TRUE, 40),
    ('site',              'Site',                 'Lokasi pelanggan/site operasional.',                               'place',               TRUE, 50),
    ('rfs_category',      'Kategori RFS',         'Jenis permintaan fasilitas (RFS).',                                'event_available',     TRUE, 60),
    ('reminder_category', 'Kategori Reminder',    'Kategori pengingat operasional.',                                  'alarm',               TRUE, 70)
ON CONFLICT (kind) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2) Entri master data TANPA parent.
-- ---------------------------------------------------------------------------

-- 2a. Team category
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('team_category', 'NOC',        'NOC',        'Network Operations Center.',        10, 'seed'),
    ('team_category', 'SALES',      'Sales',      'Tim penjualan.',                    20, 'seed'),
    ('team_category', 'FIELD',      'Field',      'Tim lapangan / instalasi.',         30, 'seed'),
    ('team_category', 'SUPPORT',    'Support',    'Tim dukungan teknis.',              40, 'seed'),
    ('team_category', 'MANAGEMENT', 'Management', 'Tim manajemen / pimpinan.',         50, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2b. Kategori tiket (induk)
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('ticket_category', 'EWO',         'EWO',         'Emergency Work Order — gangguan yang butuh tindakan cepat.', 10, 'seed'),
    ('ticket_category', 'GANGGUAN',    'Gangguan',    'Gangguan layanan umum.',                                     20, 'seed'),
    ('ticket_category', 'PERMINTAAN',  'Permintaan',  'Permintaan layanan/perubahan dari pelanggan.',               30, 'seed'),
    ('ticket_category', 'MAINTENANCE', 'Maintenance', 'Pemeliharaan terjadwal.',                                    40, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2c. Jenis gangguan
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('incident_type', 'FIBER_CUT',    'Fiber Cut',    'Kabel fiber terputus.',              10, 'seed'),
    ('incident_type', 'POWER_OUTAGE', 'Power Outage', 'Gangguan catu daya di site/POP.',    20, 'seed'),
    ('incident_type', 'BGP_FLAP',     'BGP Flap',     'Sesi BGP naik-turun berulang.',      30, 'seed'),
    ('incident_type', 'HIGH_LATENCY', 'High Latency', 'Latensi tinggi pada link.',          40, 'seed'),
    ('incident_type', 'PACKET_LOSS',  'Packet Loss',  'Kehilangan paket pada jalur.',       50, 'seed'),
    ('incident_type', 'DEVICE_DOWN',  'Device Down',  'Perangkat tidak responsif.',         60, 'seed'),
    ('incident_type', 'OTHER',        'Lainnya',      'Jenis gangguan lain.',               90, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2d. Tag tiket (sebelumnya dibuat lewat panel; kini jadi default)
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('ticket_tag', 'critical',  'Critical',  '', 0, 'seed'),
    ('ticket_tag', 'gangguan',  'Gangguan',  '', 0, 'seed'),
    ('ticket_tag', 'high',      'High',      '', 0, 'seed'),
    ('ticket_tag', 'incident',  'Incident',  '', 0, 'seed'),
    ('ticket_tag', 'perubahan', 'Perubahan', '', 0, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2e. Warna tag (meta_json menyimpan kode warna)
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('tag_color', 'red',            'Merah',            'Tag kritis/tinggi.',        '{"color":"red"}'::jsonb,    10, 'seed'),
    ('tag_color', 'orange',         'Oren',             'Tag tindak lanjut.',        '{"color":"orange"}'::jsonb, 20, 'seed'),
    ('tag_color', 'yellow',         'Kuning',           'Tag peringatan.',           '{"color":"yellow"}'::jsonb, 30, 'seed'),
    ('tag_color', 'green',          'Hijau',            'Tag terbuka/normal.',       '{"color":"green"}'::jsonb,  40, 'seed'),
    ('tag_color', 'blue',           'Biru',             'Tag jadwal/maintenance.',   '{"color":"blue"}'::jsonb,   50, 'seed'),
    ('tag_color', 'purple',         'Ungu',             'Tag VIP/pelanggan.',        '{"color":"purple"}'::jsonb, 60, 'seed'),
    ('tag_color', 'gray',           'Abu',              'Tag netral.',               '{"color":"gray"}'::jsonb,   70, 'seed'),
    ('tag_color', 'critical',       'Tag: critical',    '',                          '{"color":"red"}'::jsonb,   100, 'seed'),
    ('tag_color', 'high',           'Tag: high',        '',                          '{"color":"red"}'::jsonb,   101, 'seed'),
    ('tag_color', 'urgent',         'Urgent',           'Butuh penanganan segera.',  '{"color":"red"}'::jsonb,   102, 'seed'),
    ('tag_color', 'warning',        'Tag: warning',     '',                          '{"color":"yellow"}'::jsonb,103, 'seed'),
    ('tag_color', 'open',           'Tag: open',        '',                          '{"color":"green"}'::jsonb, 104, 'seed'),
    ('tag_color', 'maintenance',    'Tag: maintenance', '',                          '{"color":"blue"}'::jsonb,  105, 'seed'),
    ('tag_color', 'follow-up',      'Tag: follow-up',   '',                          '{"color":"orange"}'::jsonb,106, 'seed'),
    ('tag_color', 'pending',        'Tag: pending',     '',                          '{"color":"orange"}'::jsonb,107, 'seed'),
    ('tag_color', 'vip',            'Tag: vip',         '',                          '{"color":"purple"}'::jsonb,108, 'seed'),
    ('tag_color', 'change_service', 'Change Service',   'Perubahan layanan.',        '{"color":"blue"}'::jsonb,  200, 'seed'),
    ('tag_color', 'upgrade',        'Upgrade',          'Peningkatan kapasitas.',    '{"color":"purple"}'::jsonb,201, 'seed'),
    ('tag_color', 'new_install',    'New',              'Pemasangan baru.',          '{"color":"green"}'::jsonb, 202, 'seed'),
    ('tag_color', 'request',        'Request',          'Permintaan layanan.',       '{"color":"yellow"}'::jsonb,203, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2f. Produk
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('product', 'DEDICATED', 'Dedicated', 'Layanan dedicated internet.', 10, 'seed'),
    ('product', 'BROADBAND', 'Broadband', 'Layanan broadband.',          20, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2g. Jenis paket (Aktivasi/EWO)
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('service_package', 'INTERNET_METRO', 'Internet + Metro', 'Internet dengan jalur Metro Ethernet.', 10, 'seed'),
    ('service_package', 'METRO',          'Metro',            'Layanan Metro Ethernet saja.',          20, 'seed'),
    ('service_package', 'INTERNET_ONLY',  'Internet Only',    'Layanan internet saja.',                30, 'seed'),
    ('service_package', 'CDN',            'CDN',              'Content Delivery Network.',             40, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2h. Jenis daily task (meta_json.dapat_membuat_tiket)
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('daily_task_type', 'PENGECEKAN_GANGGUAN', 'Pengecekan Gangguan', 'Pengecekan rutin indikasi gangguan jaringan/layanan.', '{"dapat_membuat_tiket":true}'::jsonb,  10, 'seed'),
    ('daily_task_type', 'BACKUP',              'Backup',              'Backup konfigurasi/data perangkat.',                  '{"dapat_membuat_tiket":true}'::jsonb,  20, 'seed'),
    ('daily_task_type', 'MONITORING',          'Monitoring',          'Pemantauan perangkat/link.',                          '{"dapat_membuat_tiket":true}'::jsonb,  30, 'seed'),
    ('daily_task_type', 'ADMINISTRASI',        'Administrasi',        'Pekerjaan administratif (laporan, dokumentasi).',      '{"dapat_membuat_tiket":false}'::jsonb, 40, 'seed'),
    ('daily_task_type', 'LAINNYA',             'Lainnya',             'Pekerjaan lain di luar kategori di atas.',            '{"dapat_membuat_tiket":false}'::jsonb, 50, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 2i. Kategori RFS & Reminder
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('rfs_category', 'TRIAL',       'Trial',       'Uji coba layanan.',                10, 'seed'),
    ('rfs_category', 'NEW_INSTALL', 'Instalasi Baru','Pemasangan baru.',               20, 'seed'),
    ('reminder_category', 'TRIAL',       'Trial',       'Pengingat masa trial.',                   10, 'seed'),
    ('reminder_category', 'MAINTENANCE', 'Maintenance', 'Pengingat jadwal pemeliharaan.',          20, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3) Sub-kategori tiket (parent_id) — join ke induk berdasarkan kode.
--    Pola sama seperti migrasi 00008: baris dilewati bila induk belum ada.
-- ---------------------------------------------------------------------------
INSERT INTO master_data (kind, code, label, description, parent_id, sort_order, created_by)
SELECT 'ticket_category', v.code, v.label, v.description, p.id, v.sort_order, 'seed'
FROM (VALUES
    ('GANGGUAN_FIBER',  'Fiber',     'Gangguan pada jalur fiber.',        'GANGGUAN',    110),
    ('GANGGUAN_DEVICE', 'Perangkat', 'Gangguan pada perangkat.',          'GANGGUAN',    120),
    ('GANGGUAN_POWER',  'Power',     'Gangguan catu daya.',               'GANGGUAN',    130),
    ('EWO_JARINGAN',    'Jaringan',  'EWO untuk gangguan jaringan.',      'EWO',         210),
    ('EWO_PERANGKAT',   'Perangkat', 'EWO untuk gangguan perangkat.',     'EWO',         220),
    ('MAINT_PREVENTIF', 'Preventif', 'Pemeliharaan terjadwal preventif.', 'MAINTENANCE', 310),
    ('MAINT_KOREKTIF',  'Korektif',  'Pemeliharaan korektif.',            'MAINTENANCE', 320)
) AS v(code, label, description, parent_code, sort_order)
JOIN master_data p ON p.kind = 'ticket_category' AND p.code = v.parent_code
ON CONFLICT (kind, code) DO NOTHING;

-- +goose Down

-- Hanya membersihkan baris yang murni ditambahkan oleh migrasi ini
-- (`ticket_tag`), TIDAK menyentuh kind lain yang di-seed migrasi lama.
DELETE FROM master_data WHERE kind = 'ticket_tag';
