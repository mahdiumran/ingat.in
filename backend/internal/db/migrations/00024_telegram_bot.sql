-- +goose Up

-- ============================================================================
-- F12 — Telegram command bot (inbound).
--
-- Bot menerima command dari grup Telegram NOC lewat webhook
--   POST /api/hooks/telegram/{secret}
-- lalu membuat/mengubah work item memakai layanan yang sama dengan API panel
-- (workflow, audit, outbox) — bukan menulis langsung ke tabel.
--
-- Dua tabel kecil di sini murni untuk ketahanan & jejak:
--   bot_update_log   — idempotensi. Telegram mengirim ulang update bila webhook
--                      tidak membalas 2xx; update_id UNIQUE mencegah command
--                      (mis. /open) diproses dua kali.
--   bot_command_log  — audit ringan command bot (siapa, grup mana, hasilnya).
--
-- Daftar GRUP YANG DIIZINKAN tidak disimpan di sini, melainkan di master_data
-- (kind 'telegram_chat'): admin mengelolanya dari panel, sama seperti data
-- referensi lain. Satu entri = satu grup; kolom code = chat_id Telegram.
-- ============================================================================

-- Kelompok master data untuk allowlist grup bot Telegram.
INSERT INTO master_data_kinds (kind, label, description, icon, is_system, sort_order) VALUES
    ('telegram_chat', 'Grup Telegram Bot',
     'Grup Telegram yang boleh mengirim command ke bot. Isi Kode = chat_id (lihat /id di grup).',
     'smartphone', TRUE, 80)
ON CONFLICT (kind) DO NOTHING;

-- Idempotensi update webhook Telegram.
CREATE TABLE bot_update_log (
    update_id    BIGINT PRIMARY KEY,
    chat_id      BIGINT NOT NULL DEFAULT 0,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Jejak command bot (audit ringan, terpisah dari audit_log agar mudah ditelusuri).
CREATE TABLE bot_command_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id       BIGINT NOT NULL,
    group_label   TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    command       TEXT NOT NULL DEFAULT '',
    args          TEXT NOT NULL DEFAULT '',
    work_item_ref TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_bot_command_log_chat ON bot_command_log(chat_id, created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS bot_command_log;
DROP TABLE IF EXISTS bot_update_log;
DELETE FROM master_data_kinds WHERE kind = 'telegram_chat';
