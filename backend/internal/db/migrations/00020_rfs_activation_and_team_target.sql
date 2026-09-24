-- +goose Up

-- ============================================================================
-- F25 — Peningkatan RFS/Aktivasi-EWO.
--
-- 1. teams.target_id — target notifikasi default per tim. Dipakai sebagai
--    resolusi target untuk item yang tidak punya target sendiri (mis. RFS
--    dengan PIC team), setelah target item dan sebelum target default.
-- 2. rfs_details.pic_team_id — PIC berupa tim (menggantikan input bebas
--    pic_noc di UI). Kolom pic_noc lama tetap disimpan untuk data historis.
-- 3. rfs_activation_data — data teknis aktivasi (1:1 work_items RFS).
-- ============================================================================

-- 1) Target notifikasi per tim.
ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS target_id UUID REFERENCES notification_targets(id) ON DELETE SET NULL;

-- 2) PIC team pada rfs_details.
ALTER TABLE rfs_details
    ADD COLUMN IF NOT EXISTS pic_team_id UUID REFERENCES teams(id) ON DELETE SET NULL;

-- 3) Data aktivasi RFS.
CREATE TABLE IF NOT EXISTS rfs_activation_data (
    work_item_id      UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    ip_address        TEXT NOT NULL DEFAULT '',
    vlan_detail       TEXT NOT NULL DEFAULT '',
    interface_port    TEXT NOT NULL DEFAULT '',
    bandwidth_test    TEXT NOT NULL DEFAULT '',
    ping_test         TEXT NOT NULL DEFAULT '',
    packet_loss       TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS rfs_activation_data;
ALTER TABLE rfs_details DROP COLUMN IF EXISTS pic_team_id;
ALTER TABLE teams DROP COLUMN IF EXISTS target_id;
