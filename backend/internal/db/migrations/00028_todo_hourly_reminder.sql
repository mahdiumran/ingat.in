-- +goose Up

-- ============================================================================
-- F35 — Reminder Todo Task per jam + carry-over harian.
--
-- Tujuan:
--   1. Daftar Todo Task yang BELUM selesai dikirim berkala (default tiap jam)
--      ke kanal notifikasi sampai tugas tersebut ditandai selesai/closed.
--   2. Menyediakan status `resolved` pada workflow Reminder agar pengingat yang
--      sudah ditangani dapat ditandai tanpa menunggu expire.
--
-- Perubahan:
--   1. Template TODO_HOURLY_REMINDER (telegram & whatsapp).
--   2. Pengaturan interval reminder todo (menit; default 60 = tiap jam).
--   3. Baris workflow_definitions reminder disinkronkan dengan state machine
--      aktual di kode (workitems.DefaultWorkflows) agar tabel tetap informatif.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1) Template reminder todo per jam.
--    {{.TodoTotal}} = jumlah tugas belum selesai, {{.Description}} = daftar.
-- ---------------------------------------------------------------------------
INSERT INTO notification_templates (key, item_type, channel, subject_tpl, body_tpl, severity, description) VALUES
(
  'TODO_HOURLY_REMINDER', 'task', 'telegram',
  '',
  E'📋 *TODO BELUM SELESAI* ({{.CreatedAt}})\n\nAda *{{.TodoTotal}}* tugas yang belum selesai:\n\n{{.Description}}\n\nTandai "Selesai" pada task terkait agar tidak diingatkan lagi.',
  'warning', 'Reminder berkala Todo Task yang belum selesai (default tiap jam).'
),
(
  'TODO_HOURLY_REMINDER', 'task', 'whatsapp',
  '',
  E'*TODO BELUM SELESAI* ({{.CreatedAt}})\n\nAda *{{.TodoTotal}}* tugas yang belum selesai:\n\n{{.Description}}\n\nTandai "Selesai" pada task terkait agar tidak diingatkan lagi.',
  'warning', 'Reminder berkala Todo Task yang belum selesai (default tiap jam).'
)
ON CONFLICT (key, channel) DO UPDATE SET
    item_type = EXCLUDED.item_type,
    subject_tpl = EXCLUDED.subject_tpl,
    body_tpl = EXCLUDED.body_tpl,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description,
    updated_by = 'seed',
    updated_at = now();

-- ---------------------------------------------------------------------------
-- 2) Pengaturan interval reminder todo.
-- ---------------------------------------------------------------------------
INSERT INTO settings (key, value, updated_by) VALUES
    ('todo_task.reminder_interval_min', '60'::jsonb, 'system')
ON CONFLICT (key) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3) Sinkronkan definisi workflow reminder (informatif) dengan state machine
--    aktual, termasuk status `resolved`.
-- ---------------------------------------------------------------------------
UPDATE workflow_definitions SET
    states_json      = '["scheduled","active","expiring","expired","resolved","cancelled"]'::jsonb,
    transitions_json = '{"scheduled":["active","resolved","cancelled"],
                         "active":["expiring","expired","resolved","cancelled"],
                         "expiring":["expired","active","resolved","cancelled"],
                         "expired":["active","resolved"],
                         "resolved":["active"],
                         "cancelled":[]}'::jsonb,
    initial_state    = 'scheduled',
    terminal_states  = '["resolved","expired","cancelled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'reminder';

-- +goose Down

DELETE FROM notification_templates WHERE key = 'TODO_HOURLY_REMINDER';
DELETE FROM settings WHERE key = 'todo_task.reminder_interval_min';

UPDATE workflow_definitions SET
    states_json      = '["scheduled","active","expiring","expired","cancelled"]'::jsonb,
    transitions_json = '{"scheduled":["active","cancelled"],
                         "active":["expiring","expired","cancelled"],
                         "expiring":["expired","active","cancelled"],
                         "expired":["active"],
                         "cancelled":[]}'::jsonb,
    initial_state    = 'scheduled',
    terminal_states  = '["expired","cancelled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'reminder';
