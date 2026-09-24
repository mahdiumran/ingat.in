-- +goose Up

-- ============================================================================
-- F17 — Daily Task (item_type='daily_task').
--
-- Menu baru "Daily Task": daftar task yang dikerjakan HARI INI, bergaya todo
-- list (kolom Kanban: Belum Selesai / Sedang Dikerjakan / Selesai) dengan
-- penambahan task langsung per kolom.
--
-- Menumpang tabel work_items yang sudah ada agar otomatis memperoleh:
--   * notifikasi (template TODO_CREATED),
--   * komentar + timeline event,
--   * audit log, tags, dan halaman detail.
--
-- Konvensi tanggal:
--   * start_at = tanggal harian (pukul 00:00 WIB, disimpan UTC).
--   * due_at   = akhir tanggal harian (23:59:59 WIB) -> label "Due" konsisten.
--     Carry-over: task belum selesai dari tanggal sebelumnya tetap tampil.
--
-- Status workflow:
--   pending (Belum Selesai) -> in_progress (Sedang Dikerjakan) -> done (Selesai)
--   canceled = dibatalkan. pending juga boleh langsung -> done.
-- ============================================================================

-- 1) Perluas CHECK constraint item_type agar menerima 'daily_task'.
ALTER TABLE work_items DROP CONSTRAINT IF EXISTS work_items_item_type_check;
ALTER TABLE work_items ADD CONSTRAINT work_items_item_type_check
    CHECK (item_type IN ('task','reminder','rfs','incident','request','change','daily_task'));

-- Mempercepat filter harian (kolom start_at).
CREATE INDEX IF NOT EXISTS ix_work_items_daily
    ON work_items(item_type, start_at) WHERE NOT is_deleted;

-- 2) Baris workflow_definitions (informatif; state machine aktual di kode).
INSERT INTO workflow_definitions
    (item_type, name, states_json, transitions_json, initial_state, terminal_states, is_default)
VALUES (
    'daily_task', 'Alur Daily Task',
    '["pending","in_progress","done","canceled"]'::jsonb,
    '{"pending":["in_progress","done","canceled"],
      "in_progress":["done","pending","canceled"],
      "done":["pending"],
      "canceled":["pending"]}'::jsonb,
    'pending',
    '["done","canceled"]'::jsonb,
    TRUE
)
ON CONFLICT (item_type, name) DO NOTHING;

-- 3) Template notifikasi pembuatan daily task (channel telegram & whatsapp).
INSERT INTO notification_templates (key, item_type, channel, subject_tpl, body_tpl, severity, description) VALUES
(
  'DAILY_TASK_CREATED', 'daily_task', 'telegram',
  '',
  E'📋 DAILY TASK BARU {{.RefNo}}\n{{.Title}}\n\nTanggal : {{.DueAt}}\nOwner   : {{.Owner}}\nDibuat  : {{.CreatedBy}}\nDeskripsi : {{or .Description "—"}}',
  'info', 'Notifikasi saat daily task dibuat.'
),
(
  'DAILY_TASK_CREATED', 'daily_task', 'whatsapp',
  '',
  E'*DAILY TASK BARU* {{.RefNo}}\n{{.Title}}\n\nTanggal: {{.DueAt}}\nOwner: {{.Owner}}\nDeskripsi : {{or .Description "—"}}',
  'info', 'Notifikasi saat daily task dibuat (WhatsApp).'
)
ON CONFLICT (key, channel) DO NOTHING;

-- 4) Kebijakan notifikasi (policies) untuk daily_task: kanal telegram/WA aktif
--    mengikuti target default; tidak ada quiet hours wajib.
--    (Baris policy bersifat opsional — item tetap memakai target default bila
--     tidak ada policy khusus.)

-- +goose Down

DELETE FROM notification_templates
 WHERE key = 'DAILY_TASK_CREATED' AND item_type = 'daily_task';

DELETE FROM workflow_definitions WHERE item_type = 'daily_task';

DROP INDEX IF EXISTS ix_work_items_daily;

ALTER TABLE work_items DROP CONSTRAINT IF EXISTS work_items_item_type_check;
ALTER TABLE work_items ADD CONSTRAINT work_items_item_type_check
    CHECK (item_type IN ('task','reminder','rfs','incident','request','change'));
