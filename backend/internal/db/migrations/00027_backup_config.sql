-- +goose Up

-- ============================================================================
-- F34 — Konfigurasi cadangan (backup & restore) + unggah otomatis ke FTP.
--
-- Seluruh pengaturan disimpan pada tabel `settings` (key = 'backup.config')
-- sehingga dapat diubah operator dari panel tanpa deploy ulang. Password FTP
-- disimpan TERENKRIPSI (AES-256-GCM) pada kolom separuh; lihat internal/backup.
--
-- Bentuk nilai (JSONB):
--   {
--     "enabled": false,               -- job cadangan otomatis aktif
--     "schedule": "0 2 * * *",        -- ekspresi cron 5-field (waktu lokal)
--     "keep_days": 14,                -- retensi berkas lokal
--     "ftp": {
--       "enabled": false,
--       "host": "", "port": 21,
--       "username": "", "password_enc": "",
--       "dir": "", "passive": true
--     },
--     "last_run_at": null,
--     "last_status": "",              -- "", "ok", "error"
--     "last_error": "",
--     "last_file": ""
--   }
-- ============================================================================

INSERT INTO settings (key, value, updated_by) VALUES
    ('backup.config', '{
        "enabled": false,
        "schedule": "0 2 * * *",
        "keep_days": 14,
        "ftp": {
            "enabled": false,
            "host": "",
            "port": 21,
            "username": "",
            "password_enc": "",
            "dir": "",
            "passive": true
        },
        "last_run_at": null,
        "last_status": "",
        "last_error": "",
        "last_file": ""
    }'::jsonb, 'seed')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

DELETE FROM settings WHERE key = 'backup.config';
