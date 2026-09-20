-- +goose Up
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order)
VALUES ('customer', 'Customer', 'Data pelanggan', 'business', TRUE, 70)
ON CONFLICT (kind) DO UPDATE SET
    label=EXCLUDED.label,
    description=EXCLUDED.description,
    icon=EXCLUDED.icon,
    is_system=TRUE;

INSERT INTO master_data (kind, code, label, description, meta_json) VALUES
    ('customer', 'CUST-SEED-001', 'Pelanggan Contoh Personal', 'Data awal yang dapat diubah operator',
     '{"jenis":"personal","pic":"","telepon":"","email":"","alamat":"","kapasitas":""}'::jsonb),
    ('customer', 'CUST-SEED-002', 'Pelanggan Contoh Corporate', 'Data awal yang dapat diubah operator',
     '{"jenis":"corporate","pic":"PIC Operasional","telepon":"","email":"","alamat":"","kapasitas":""}'::jsonb)
ON CONFLICT (kind, code) DO NOTHING;

-- +goose Down
DELETE FROM master_data WHERE kind='customer' AND code IN ('CUST-SEED-001','CUST-SEED-002');
-- Kind customer dibuat sejak 00005 agar schema panel lengkap; jangan hapus di sini.
