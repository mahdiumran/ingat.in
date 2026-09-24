-- +goose Up

-- ============================================================================
-- Master Data & Role Permissions (F12).
--
-- Tujuan: menyediakan SATU wadah generik untuk data referensi yang dapat
-- ditambah/diubah/dihapus operator dari panel, tanpa perlu migrasi skema baru
-- setiap kali muncul kebutuhan (mis. kategori EWO, produk, lokasi POP, tag
-- tiket). Ini juga menjadi fondasi F10 (ticketing) dan F11 (SLA).
--
-- Desain:
--   master_data.kind  — kelompok data ("ticket_category", "product", "pop",
--                       "ticket_tag", "rfs_category", "site", dsb.). Nilai
--                       kind bersifat terbuka: operator dapat membuat kelompok
--                       baru langsung dari panel.
--   master_data.code  — kode unik dalam satu kind (dipakai program/notifikasi).
--   master_data.label — nama tampil untuk manusia.
--   parent_id         — hierarki opsional (mis. Kategori → Subkategori EWO).
--   meta_json         — atribut tambahan bebas (mis. SLA menit, warna, PIC).
--
--   role_permissions  — matriks izin yang dapat diedit (Edit Role Permission),
--                       menggantikan hardcode di middleware secara bertahap.
-- ============================================================================

CREATE TABLE master_data (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        TEXT NOT NULL,
    code        TEXT NOT NULL,
    label       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id   UUID REFERENCES master_data(id) ON DELETE CASCADE,
    sort_order  INT NOT NULL DEFAULT 0,
    meta_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Kode unik per kelompok, tetapi boleh sama antar kelompok (mis. kode
    -- "internet" pada product dan pada ticket_tag tidak saling bentrok).
    UNIQUE (kind, code)
);

CREATE INDEX ix_master_data_kind ON master_data(kind, sort_order, label);
CREATE INDEX ix_master_data_parent ON master_data(parent_id);
CREATE INDEX ix_master_data_active ON master_data(kind, is_active);

-- Registri kelompok master data yang dikenali panel (agar UI dapat menampilkan
-- tab/kelompok walau belum ada satu pun entri).
CREATE TABLE master_data_kinds (
    kind        TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon        TEXT NOT NULL DEFAULT 'category',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Role permissions — matriks izin yang dapat diedit dari panel.
--
-- Format baris: (role, action) unik. `allowed` menentukan apakah role tersebut
-- boleh melakukan `action`. `action` memakai penamaan "<entities>.<verb>",
-- mis. "items.write", "providers.write", "masterdata.write".
-- ---------------------------------------------------------------------------
CREATE TABLE role_permissions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role       TEXT NOT NULL
               CHECK (role IN ('admin','agent','noc','sales','viewer','customer')),
    action     TEXT NOT NULL,
    allowed    BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role, action)
);

CREATE INDEX ix_role_permissions_role ON role_permissions(role);

-- ---------------------------------------------------------------------------
-- Seed kelompok master data awal.
-- ---------------------------------------------------------------------------
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('ticket_category', 'Kategori Tiket (EWO)',  'Kategori/klasifikasi tiket NOC, mis. EWO, Gangguan, Permintaan.', 'confirmation_number', TRUE, 10),
    ('ticket_tag',      'Tag Tiket',             'Label bebas untuk pengelompokan tiket.',                         'sell',                TRUE, 20),
    ('product',         'Produk',                'Produk/layanan yang dijual, mis. Dedicated, Broadband.',         'inventory_2',         TRUE, 30),
    ('pop',             'Lokasi POP',            'Titik kehadiran (POP) untuk penugasan & pelaporan.',             'location_on',         TRUE, 40),
    ('site',            'Site',                  'Lokasi pelanggan/site operasional.',                             'place',               TRUE, 50),
    ('rfs_category',    'Kategori RFS',          'Jenis permintaan fasilitas (RFS).',                             'event_available',     TRUE, 60),
    ('reminder_category','Kategori Reminder',    'Kategori pengingat operasional.',                               'alarm',               TRUE, 70)
ON CONFLICT (kind) DO NOTHING;

-- Kategori tiket awal (dapat diedit operator).
INSERT INTO master_data (kind, code, label, description, sort_order, created_by) VALUES
    ('ticket_category', 'EWO',        'EWO',        'Emergency Work Order — gangguan yang butuh tindakan cepat.', 10, 'seed'),
    ('ticket_category', 'GANGGUAN',   'Gangguan',   'Gangguan layanan umum.',                                      20, 'seed'),
    ('ticket_category', 'PERMINTAAN', 'Permintaan', 'Permintaan layanan/perubahan dari pelanggan.',                30, 'seed'),
    ('ticket_category', 'MAINTENANCE','Maintenance','Pemeliharaan terjadwal.',                                     40, 'seed'),
    ('product',         'DEDICATED',  'Dedicated',  'Layanan dedicated internet.',                                 10, 'seed'),
    ('product',         'BROADBAND',  'Broadband',  'Layanan broadband.',                                          20, 'seed'),
    ('rfs_category',    'TRIAL',      'Trial',      'Uji coba layanan.',                                           10, 'seed'),
    ('rfs_category',    'NEW_INSTALL','Instalasi Baru','Pemasangan baru.',                                         20, 'seed'),
    ('reminder_category','TRIAL',     'Trial',      'Pengingat masa trial.',                                       10, 'seed'),
    ('reminder_category','MAINTENANCE','Maintenance','Pengingat jadwal pemeliharaan.',                              20, 'seed')
ON CONFLICT (kind, code) DO NOTHING;

-- Matriks izin awal (mencerminkan perilaku middleware saat ini).
-- admin selalu boleh (dijaga di kode); baris di bawah untuk peran lain.
INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('admin',   'items.write',     TRUE,  'seed'),
    ('admin',   'providers.write', TRUE,  'seed'),
    ('admin',   'masterdata.write',TRUE,  'seed'),
    ('admin',   'users.write',     TRUE,  'seed'),
    ('noc',     'items.write',     TRUE,  'seed'),
    ('noc',     'providers.write', TRUE,  'seed'),
    ('noc',     'masterdata.write',TRUE,  'seed'),
    ('noc',     'users.write',     FALSE, 'seed'),
    ('agent',   'items.write',     TRUE,  'seed'),
    ('agent',   'providers.write', FALSE, 'seed'),
    ('agent',   'masterdata.write',FALSE, 'seed'),
    ('agent',   'users.write',     FALSE, 'seed'),
    ('sales',   'items.write',     TRUE,  'seed'),
    ('sales',   'providers.write', FALSE, 'seed'),
    ('sales',   'masterdata.write',FALSE, 'seed'),
    ('sales',   'users.write',     FALSE, 'seed'),
    ('viewer',  'items.write',     FALSE, 'seed'),
    ('viewer',  'providers.write', FALSE, 'seed'),
    ('viewer',  'masterdata.write',FALSE, 'seed'),
    ('viewer',  'users.write',     FALSE, 'seed')
ON CONFLICT (role, action) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS master_data;
DROP TABLE IF EXISTS master_data_kinds;
