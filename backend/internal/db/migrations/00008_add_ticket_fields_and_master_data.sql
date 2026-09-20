-- +goose Up
ALTER TABLE ticket_details ADD COLUMN IF NOT EXISTS incident_type TEXT NOT NULL DEFAULT '';

INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('incident_type', 'Jenis Gangguan', 'Klasifikasi gangguan tiket', 'report_problem', TRUE, 40),
    ('tag_color', 'Warna Tag', 'Pemetaan tag ke warna UI', 'label', TRUE, 50),
    ('ticket_category', 'Kategori Tiket', 'Kategori dan subkategori tiket', 'category', TRUE, 60)
ON CONFLICT (kind) DO UPDATE SET label=EXCLUDED.label, description=EXCLUDED.description, icon=EXCLUDED.icon;

INSERT INTO master_data (kind, code, label, sort_order) VALUES
    ('incident_type', 'network_down', 'Network Down', 10),
    ('incident_type', 'degradation', 'Degradasi Layanan', 20),
    ('incident_type', 'packet_loss', 'Packet Loss', 30),
    ('incident_type', 'high_latency', 'High Latency', 40),
    ('incident_type', 'configuration', 'Kesalahan Konfigurasi', 50),
    ('tag_color', 'red', 'Merah', 10),
    ('tag_color', 'orange', 'Oranye', 20),
    ('tag_color', 'yellow', 'Kuning', 30),
    ('tag_color', 'green', 'Hijau', 40),
    ('tag_color', 'blue', 'Biru', 50),
    ('tag_color', 'purple', 'Ungu', 60),
    ('tag_color', 'gray', 'Abu-abu', 70)
ON CONFLICT (kind, code) DO NOTHING;

UPDATE master_data SET meta_json = jsonb_build_object('color', code)
WHERE kind='tag_color' AND meta_json = '{}'::jsonb;

INSERT INTO master_data (kind, code, label, sort_order) VALUES
    ('ticket_category', 'network', 'Network', 10),
    ('ticket_category', 'service', 'Service', 20),
    ('ticket_category', 'customer', 'Customer', 30)
ON CONFLICT (kind, code) DO NOTHING;

INSERT INTO master_data (kind, code, label, parent_id, sort_order)
SELECT 'ticket_category', child.code, child.label, parent.id, child.sort_order
FROM (VALUES
    ('network', 'routing', 'Routing', 11),
    ('network', 'access', 'Access', 12),
    ('service', 'internet', 'Internet', 21),
    ('customer', 'complaint', 'Keluhan Pelanggan', 31)
) AS child(parent_code, code, label, sort_order)
JOIN master_data parent ON parent.kind='ticket_category' AND parent.code=child.parent_code
ON CONFLICT (kind, code) DO NOTHING;

-- +goose Down
DELETE FROM master_data WHERE kind IN ('incident_type','tag_color','ticket_category');
