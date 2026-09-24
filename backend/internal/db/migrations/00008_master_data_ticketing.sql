-- +goose Up

-- ============================================================================
-- F15 — Master Data untuk Ticketing + warna tag.
--
-- Menambah kelompok master data baru dan memperkaya ticket_details:
--   1. 'incident_type'  — jenis gangguan (fiber cut, power outage, BGP flap…).
--   2. 'tag_color'      — pemetaan warna untuk tag bebas (kode = nama tag).
--   3. 'ticket_category' diperluas: subkategori disimpan sebagai anak
--      (parent_id) dari kategori. Entri anak memakai kode unik per kind.
--   4. ticket_details.incident_type — jenis gangguan terpilih.
-- ============================================================================

-- 1) Kelompok master data baru.
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('incident_type', 'Jenis Gangguan', 'Jenis gangguan teknis, mis. fiber cut, power outage, BGP flap.', 'report_problem', TRUE, 15),
    ('tag_color',     'Warna Tag',      'Pemetaan warna untuk tag (merah, kuning, hijau, biru, ungu, oren).', 'palette',        TRUE, 25)
ON CONFLICT (kind) DO NOTHING;

-- 2) Warna tag awal (kode = nama tag yang dipakai di work_items.tags).
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('tag_color', 'red',    'Merah',  'Tag kritis/tinggi.',   '{"color":"red"}'::jsonb,    10, 'seed'),
    ('tag_color', 'orange', 'Oren',   'Tag tindak lanjut.',   '{"color":"orange"}'::jsonb, 20, 'seed'),
    ('tag_color', 'yellow', 'Kuning', 'Tag peringatan.',      '{"color":"yellow"}'::jsonb, 30, 'seed'),
    ('tag_color', 'green',  'Hijau',  'Tag terbuka/normal.',  '{"color":"green"}'::jsonb,  40, 'seed'),
    ('tag_color', 'blue',   'Biru',   'Tag jadwal/maintenance.','{"color":"blue"}'::jsonb, 50, 'seed'),
    ('tag_color', 'purple', 'Ungu',   'Tag VIP/pelanggan.',   '{"color":"purple"}'::jsonb, 60, 'seed'),
    ('tag_color', 'gray',   'Abu',    'Tag netral.',          '{"color":"gray"}'::jsonb,   70, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- Pemetaan warna tag umum (rekomendasi; operator dapat mengubahnya).
INSERT INTO master_data (kind, code, label, description, meta_json, sort_order, created_by) VALUES
    ('tag_color', 'critical', 'Tag: critical', '', '{"color":"red"}'::jsonb,    100, 'seed'),
    ('tag_color', 'high',     'Tag: high',     '', '{"color":"red"}'::jsonb,    101, 'seed'),
    ('tag_color', 'urgent',   'Tag: urgent',   '', '{"color":"red"}'::jsonb,    102, 'seed'),
    ('tag_color', 'warning',  'Tag: warning',  '', '{"color":"yellow"}'::jsonb, 103, 'seed'),
    ('tag_color', 'open',     'Tag: open',     '', '{"color":"green"}'::jsonb,  104, 'seed'),
    ('tag_color', 'maintenance','Tag: maintenance','','{"color":"blue"}'::jsonb, 105, 'seed'),
    ('tag_color', 'follow-up','Tag: follow-up','', '{"color":"orange"}'::jsonb, 106, 'seed'),
    ('tag_color', 'pending',  'Tag: pending',  '', '{"color":"orange"}'::jsonb, 107, 'seed'),
    ('tag_color', 'vip',      'Tag: vip',      '', '{"color":"purple"}'::jsonb, 108, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 3) Jenis gangguan awal.
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('incident_type', 'FIBER_CUT',   'Fiber Cut',    'Kabel fiber terputus.',              10, 'seed'),
    ('incident_type', 'POWER_OUTAGE','Power Outage', 'Gangguan catu daya di site/POP.',    20, 'seed'),
    ('incident_type', 'BGP_FLAP',    'BGP Flap',     'Sesi BGP naik-turun berulang.',      30, 'seed'),
    ('incident_type', 'HIGH_LATENCY','High Latency', 'Latensi tinggi pada link.',          40, 'seed'),
    ('incident_type', 'PACKET_LOSS', 'Packet Loss',  'Kehilangan paket pada jalur.',       50, 'seed'),
    ('incident_type', 'DEVICE_DOWN', 'Device Down',  'Perangkat tidak responsif.',         60, 'seed'),
    ('incident_type', 'OTHER',       'Lainnya',      'Jenis gangguan lain.',               90, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- 4) Subkategori tiket sebagai anak dari kategori (parent_id).
--    Contoh: Gangguan → { Fiber Cut, Device Down }, EWO → { Jaringan, Perangkat }.
INSERT INTO master_data (kind, code, label, description, parent_id, sort_order, created_by)
SELECT 'ticket_category', v.code, v.label, v.description, p.id, v.sort_order, 'seed'
FROM (VALUES
    ('GANGGUAN_FIBER',  'Fiber',        'Gangguan pada jalur fiber.',       'GANGGUAN',   110),
    ('GANGGUAN_DEVICE', 'Perangkat',    'Gangguan pada perangkat.',         'GANGGUAN',   120),
    ('GANGGUAN_POWER',  'Power',        'Gangguan catu daya.',              'GANGGUAN',   130),
    ('EWO_JARINGAN',    'Jaringan',     'EWO untuk gangguan jaringan.',     'EWO',        210),
    ('EWO_PERANGKAT',   'Perangkat',   'EWO untuk gangguan perangkat.',    'EWO',        220),
    ('MAINT_PREVENTIF', 'Preventif',    'Pemeliharaan terjadwal preventif.', 'MAINTENANCE', 310),
    ('MAINT_KOREKTIF',  'Korektif',     'Pemeliharaan korektif.',           'MAINTENANCE', 320)
) AS v(code, label, description, parent_code, sort_order)
JOIN master_data p ON p.kind = 'ticket_category' AND p.code = v.parent_code
ON CONFLICT (kind, code) DO NOTHING;

-- 5) Jenis gangguan pada ticket_details.
ALTER TABLE ticket_details ADD COLUMN IF NOT EXISTS incident_type TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE ticket_details DROP COLUMN IF EXISTS incident_type;
DELETE FROM master_data WHERE kind = 'ticket_category'
    AND code IN ('GANGGUAN_FIBER','GANGGUAN_DEVICE','GANGGUAN_POWER','EWO_JARINGAN','EWO_PERANGKAT','MAINT_PREVENTIF','MAINT_KOREKTIF');
DELETE FROM master_data WHERE kind = 'incident_type';
DELETE FROM master_data WHERE kind = 'tag_color';
DELETE FROM master_data_kinds WHERE kind IN ('incident_type','tag_color');
