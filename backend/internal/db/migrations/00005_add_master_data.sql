-- +goose Up
CREATE TABLE master_data_kinds (
    kind TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon TEXT NOT NULL DEFAULT 'category',
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INTEGER NOT NULL DEFAULT 999
);

CREATE TABLE master_data (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL REFERENCES master_data_kinds(kind) ON DELETE RESTRICT,
    code TEXT NOT NULL,
    label TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id UUID REFERENCES master_data(id) ON DELETE SET NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    meta_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, code),
    CHECK (jsonb_typeof(meta_json) = 'object')
);

CREATE TABLE role_permissions (
    role TEXT NOT NULL CHECK (role IN ('admin','agent','noc','sales','viewer','customer')),
    action TEXT NOT NULL,
    allowed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL DEFAULT 'system',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role, action)
);

CREATE INDEX ix_master_data_kind_active ON master_data(kind, is_active, sort_order);
CREATE INDEX ix_master_data_parent ON master_data(parent_id, sort_order);
CREATE INDEX ix_master_data_kind_parent ON master_data(kind, parent_id);

INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('service_package', 'Paket Layanan', 'Jenis paket layanan', 'inventory_2', TRUE, 10),
    ('bandwidth', 'Bandwidth', 'Pilihan bandwidth', 'speed', TRUE, 20),
    ('install_stage', 'Tahap Instalasi', 'Tahap pengerjaan RFS', 'construction', TRUE, 30),
    ('incident_type', 'Jenis Gangguan', 'Klasifikasi gangguan tiket', 'report_problem', TRUE, 40),
    ('tag_color', 'Warna Tag', 'Pemetaan tag ke warna UI', 'label', TRUE, 50),
    ('ticket_category', 'Kategori Tiket', 'Kategori dan subkategori tiket', 'category', TRUE, 60),
    ('customer', 'Customer', 'Data pelanggan', 'business', TRUE, 70)
ON CONFLICT (kind) DO UPDATE SET label=EXCLUDED.label, description=EXCLUDED.description, icon=EXCLUDED.icon;

INSERT INTO master_data (kind, code, label, description, meta_json) VALUES
    ('service_package', 'DEDICATED', 'Dedicated Internet', '', '{}'::jsonb),
    ('service_package', 'BROADBAND', 'Broadband Internet', '', '{}'::jsonb),
    ('bandwidth', '100M', '100 Mbps', '', '{}'::jsonb),
    ('bandwidth', '1G', '1 Gbps', '', '{}'::jsonb),
    ('install_stage', 'planned', 'Planned', '', '{}'::jsonb),
    ('install_stage', 'provisioning', 'Provisioning', '', '{}'::jsonb),
    ('tag_color', 'urgent', 'Urgent', '', '{"color":"red"}'::jsonb),
    ('tag_color', 'vip', 'VIP', '', '{"color":"purple"}'::jsonb)
ON CONFLICT (kind, code) DO NOTHING;

INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('admin', 'items.write', TRUE, 'system'),
    ('admin', 'providers.write', TRUE, 'system'),
    ('admin', 'masterdata.write', TRUE, 'system'),
    ('admin', 'users.write', TRUE, 'system'),
    ('agent', 'items.write', TRUE, 'system'),
    ('noc', 'items.write', TRUE, 'system'),
    ('sales', 'items.write', TRUE, 'system'),
    ('viewer', 'items.write', FALSE, 'system'),
    ('customer', 'items.write', FALSE, 'system'),
    ('agent', 'providers.write', FALSE, 'system'),
    ('noc', 'providers.write', FALSE, 'system'),
    ('sales', 'providers.write', FALSE, 'system'),
    ('viewer', 'providers.write', FALSE, 'system'),
    ('customer', 'providers.write', FALSE, 'system'),
    ('agent', 'masterdata.write', FALSE, 'system'),
    ('noc', 'masterdata.write', FALSE, 'system'),
    ('sales', 'masterdata.write', FALSE, 'system'),
    ('viewer', 'masterdata.write', FALSE, 'system'),
    ('customer', 'masterdata.write', FALSE, 'system'),
    ('agent', 'users.write', FALSE, 'system'),
    ('noc', 'users.write', FALSE, 'system'),
    ('sales', 'users.write', FALSE, 'system'),
    ('viewer', 'users.write', FALSE, 'system'),
    ('customer', 'users.write', FALSE, 'system')
ON CONFLICT (role, action) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS master_data;
DROP TABLE IF EXISTS master_data_kinds;
