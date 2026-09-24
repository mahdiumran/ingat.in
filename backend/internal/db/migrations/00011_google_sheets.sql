-- +goose Up

-- ============================================================================
-- F18 — Auto-sync Todo Task ke Google Spreadsheet.
--
-- Tujuan: setiap Todo Task (item_type='task') yang dibuat NOC otomatis tercatat
-- sebagai satu baris di Google Spreadsheet; status diperbarui in-place.
--
-- Arsitektur (meniru pola notification_outbox):
--   * sheet_sync_config : konfigurasi tunggal (enabled, spreadsheet_id,
--     sheet_name, service account terenkripsi). Dikelola dari panel admin —
--     TIDAK ada nilai yang di-hardcode di kode.
--   * sheet_sync_queue  : antrean idempoten. Satu entri per work item
--     (event_key unik), payload di-upsert saat item berubah; op=append sampai
--     pernah sukses, lalu op=update (memperbarui baris yang sama via Ref).
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Konfigurasi sinkronisasi (satu baris; id tetap agar mudah di-UPSERT).
-- ---------------------------------------------------------------------------
CREATE TABLE sheet_sync_config (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enabled             BOOLEAN NOT NULL DEFAULT FALSE,
    spreadsheet_id      TEXT NOT NULL DEFAULT '',
    -- sheet_name hanya SARAN AWAL; admin dapat mengubahnya dari panel web.
    sheet_name          TEXT NOT NULL DEFAULT 'Todo',
    -- Service account JSON terenkripsi AES-256-GCM (pola notification_providers).
    service_account_enc TEXT NOT NULL DEFAULT '',
    -- Apakah baris header sudah ditulis (agar tidak ditulis berulang).
    header_written      BOOLEAN NOT NULL DEFAULT FALSE,
    last_sync_at        TIMESTAMPTZ,
    last_error          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hanya boleh ada satu baris. Membuat baris awal (config default nonaktif).
INSERT INTO sheet_sync_config (enabled, spreadsheet_id, sheet_name)
VALUES (FALSE, '', 'Todo');

-- ---------------------------------------------------------------------------
-- Antrean sinkronisasi (idempoten; aman restart & internet mati).
-- ---------------------------------------------------------------------------
CREATE TABLE sheet_sync_queue (
    id              BIGSERIAL PRIMARY KEY,
    work_item_id    UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    -- Satu entri logis per work item; membuat enqueue berulang tidak menggandakan.
    event_key       TEXT NOT NULL UNIQUE,
    -- Ref dipakai sebagai kunci pencocokan baris di spreadsheet (kolom A).
    ref_no          TEXT NOT NULL,
    op              TEXT NOT NULL DEFAULT 'append'
                    CHECK (op IN ('append','update')),
    action          TEXT NOT NULL DEFAULT 'create',
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','sending','sent','failed')),
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT NOT NULL DEFAULT '',
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Klaim batch: baris yang jatuh tempo untuk diproses.
CREATE INDEX ix_sheet_sync_queue_ready
    ON sheet_sync_queue(status, next_attempt_at) WHERE status IN ('pending','failed');

-- Pencarian baris berdasarkan Ref.
CREATE INDEX ix_sheet_sync_queue_ref ON sheet_sync_queue(ref_no);

-- +goose Down

DROP TABLE IF EXISTS sheet_sync_queue;
DROP TABLE IF EXISTS sheet_sync_config;
