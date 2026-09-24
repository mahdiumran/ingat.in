-- +goose Up

-- ============================================================================
-- Lampirkan deskripsi item ke notifikasi Telegram/WA.
--
-- Payload notifikasi sudah membawa {{.Description}} (diisi dari work_items
-- description). Migrasi ini menambahkan blok "Deskripsi" pada template
-- pembuatan todo/tiket, reminder (offset/hari-H/terlambat), dan RFS
-- (mendekat/hari-H/terlewat) untuk kedua kanal.
--
-- Blok deskripsi hanya muncul bila deskripsi tidak kosong (memakai helper
-- {{or}} + teks bersyarat), sehingga tidak meninggalkan label menggantung.
-- ============================================================================

-- TODO_CREATED (dipakai juga untuk notifikasi pembuatan tiket).
UPDATE notification_templates
SET body_tpl = body_tpl || E'\n\nDeskripsi : {{or .Description "—"}}',
    updated_at = now()
WHERE key = 'TODO_CREATED'
  AND body_tpl NOT LIKE '%{{.Description}}%';

-- REMINDER_OFFSET / REMINDER_DUE_TODAY / REMINDER_LATE.
UPDATE notification_templates
SET body_tpl = body_tpl || E'\n\nDeskripsi : {{or .Description "—"}}',
    updated_at = now()
WHERE key IN ('REMINDER_OFFSET', 'REMINDER_DUE_TODAY', 'REMINDER_LATE')
  AND body_tpl NOT LIKE '%{{.Description}}%';

-- RFS_UPCOMING / RFS_TODAY / RFS_LATE.
UPDATE notification_templates
SET body_tpl = body_tpl || E'\n\nDeskripsi : {{or .Description "—"}}',
    updated_at = now()
WHERE key IN ('RFS_UPCOMING', 'RFS_TODAY', 'RFS_LATE')
  AND body_tpl NOT LIKE '%{{.Description}}%';

-- +goose Down

UPDATE notification_templates
SET body_tpl = replace(body_tpl, E'\n\nDeskripsi : {{or .Description "—"}}', ''),
    updated_at = now()
WHERE key IN ('TODO_CREATED', 'REMINDER_OFFSET', 'REMINDER_DUE_TODAY', 'REMINDER_LATE',
              'RFS_UPCOMING', 'RFS_TODAY', 'RFS_LATE');
