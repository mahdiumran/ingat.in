-- +goose Up

-- ============================================================================
-- Memperbaiki nama placeholder pada template notifikasi.
--
-- Temuan saat pengujian end-to-end F7:
--   Template seed memakai `{{.PicNoc}}` sementara field pada struct
--   notify.Payload bernama `PicNOC` (huruf kapital semua). text/template
--   bersifat case-sensitive, sehingga render GAGAL dengan:
--     "executing body at <.PicNoc>: can't evaluate field PicNoc"
--   Akibatnya notifikasi RFS langsung berstatus failed di outbox.
--
-- Perbaikan: samakan seluruh placeholder menjadi `{{.PicNOC}}`.
--
-- Catatan: ini juga menegaskan pentingnya fallback + penyimpanan rendered_body,
-- karena tanpa keduanya kegagalan template akan sulit didiagnosis.
-- ============================================================================

UPDATE notification_templates
SET body_tpl = replace(body_tpl, '{{.PicNoc}}', '{{.PicNOC}}'),
    updated_at = now()
WHERE body_tpl LIKE '%{{.PicNoc}}%';

-- Perbaiki juga komentar dokumentasi placeholder pada deskripsi (bila ada).
UPDATE notification_templates
SET description = replace(description, '{{.PicNoc}}', '{{.PicNOC}}')
WHERE description LIKE '%{{.PicNoc}}%';

-- +goose Down
-- Mengembalikan ke bentuk lama (sengaja menghasilkan template yang salah render
-- agar pengembalian skema benar-benar simetris dengan Up; lingkungan produksi
-- tidak seharusnya menjalankan Down ini).
UPDATE notification_templates
SET body_tpl = replace(body_tpl, '{{.PicNOC}}', '{{.PicNoc}}'),
    updated_at = now()
WHERE key IN ('REMINDER_LATE', 'RFS_UPCOMING', 'RFS_TODAY', 'RFS_LATE');
