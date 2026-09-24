-- +goose Up

-- ============================================================================
-- F19 — Jejak pengubah terakhir + keterangan penyelesaian.
--
-- 1) work_items.updated_by_username
--    Menyimpan username operator yang paling terakhir mengubah item (dibuat,
--    diedit, atau diubah statusnya). Ditampilkan di UI ("Diperbarui oleh …")
--    dan disertakan pada sinkronisasi spreadsheet.
--
-- 2) task_details.completion_note
--    Keterangan yang diisi operator saat menandai Daily Task "Selesai".
--    Hanya relevan untuk item_type='daily_task'; kolom ini juga ikut disinkronkan
--    ke spreadsheet.
-- ============================================================================

ALTER TABLE work_items
    ADD COLUMN IF NOT EXISTS updated_by_username TEXT NOT NULL DEFAULT '';

ALTER TABLE task_details
    ADD COLUMN IF NOT EXISTS completion_note TEXT NOT NULL DEFAULT '';

-- Isi awal: item lama dianggap terakhir diubah oleh pembuatnya.
UPDATE work_items
   SET updated_by_username = created_by
 WHERE updated_by_username = '' AND created_by <> '';

-- +goose Down

ALTER TABLE task_details DROP COLUMN IF EXISTS completion_note;
ALTER TABLE work_items DROP COLUMN IF EXISTS updated_by_username;
