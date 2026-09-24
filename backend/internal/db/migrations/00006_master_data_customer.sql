-- +goose Up

-- ============================================================================
-- Master Data: Customer.
--
-- Customer memakai tabel master_data yang sama (kind='customer'), dengan
-- atribut terstruktur disimpan pada meta_json:
--   jenis      : "personal" | "corporate"
--   pic        : nama PIC (hanya wajib untuk corporate)
--   phone      : nomor telepon
--   email      : surel
--   address    : alamat
--   capacity   : kapasitas (teks bebas, mis. "100 Mbps")
--
-- Kolom utama:
--   code  -> nomor/kode pelanggan (mis. CUST-2026-0001)
--   label -> nama pelanggan
-- ============================================================================

INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('customer', 'Customer', 'Data pelanggan (personal/corporate) beserta kontak dan kapasitas.', 'person', TRUE, 5)
ON CONFLICT (kind) DO NOTHING;

-- Urutan tampil di panel: customer di atas.
UPDATE master_data_kinds SET sort_order = 5 WHERE kind = 'customer';

-- +goose Down
DELETE FROM master_data_kinds WHERE kind = 'customer';
