-- +goose Up

-- ============================================================================
-- Memperbaiki temuan A11 dari review F1:
--
--   notification_templates memiliki UNIQUE (key, channel). Namun PostgreSQL
--   memperlakukan NULL sebagai nilai yang BERBEDA pada indeks unik, sehingga
--   baris dengan channel NULL (TEST_MESSAGE, SLA_WARNING, SLA_BREACH) TIDAK
--   ter-deduplikasi oleh `ON CONFLICT (key, channel) DO NOTHING`.
--   Akibatnya seed yang dijalankan ulang menambah baris duplikat.
--
-- Solusi: tambahkan indeks unik ekspresi yang memperlakukan NULL sebagai
-- nilai tunggal. Baris tetap menyimpan channel = NULL (semantiknya "berlaku
-- untuk semua kanal"), tetapi keunikannya kini ditegakkan.
--
-- Catatan: indeks unik (key, channel) yang lama tetap dipertahankan karena
--   masih melayani dedup baris ber-channel eksplisit, dan menghapusnya akan
--   mengubah perilaku ON CONFLICT yang sudah dipakai seed. Indeks baru ini
--   melengkapi, bukan menggantikan.
-- ============================================================================

-- Bersihkan duplikat yang mungkin sudah terlanjur ada sebelum indeks dibuat.
DELETE FROM notification_templates a
USING notification_templates b
WHERE a.channel IS NULL
  AND b.channel IS NULL
  AND a.key = b.key
  AND a.id <> b.id
  AND a.ctid > b.ctid;

-- Indeks unik untuk baris tanpa kanal spesifik (channel IS NULL).
CREATE UNIQUE INDEX IF NOT EXISTS ux_notification_templates_key_no_channel
    ON notification_templates (key)
    WHERE channel IS NULL;

-- +goose Down
DROP INDEX IF EXISTS ux_notification_templates_key_no_channel;
