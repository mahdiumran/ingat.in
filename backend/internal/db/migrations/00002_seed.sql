-- +goose Up

-- ============================================================================
-- Ingat.in — seed data awal (F1)
--
-- Seed ini mengisi data yang dibutuhkan agar aplikasi bisa berjalan:
-- organisasi, tim, definisi workflow, policy eskalasi (trial 3 hari & RFS),
-- policy SLA placeholder, target notifikasi NOC, template notifikasi, settings.
--
-- Admin user TIDAK di-seed di sini (dibuat saat bootstrap aplikasi dari
-- INGATIN_ADMIN_USERNAME / INGATIN_ADMIN_PASSWORD) agar password tidak
-- pernah muncul di file migrasi.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Organisasi default
-- ---------------------------------------------------------------------------
INSERT INTO organizations (name, code) VALUES ('Internal', 'INTERNAL')
ON CONFLICT (code) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Tim
-- ---------------------------------------------------------------------------
INSERT INTO teams (name, description) VALUES
  ('NOC',   'Network Operations Center — monitoring, reminder trial/RFS, tiket insiden'),
  ('Sales', 'Tim sales — input data RFS dan data pelanggan'),
  ('Field', 'Tim lapangan — instalasi dan aktivasi perangkat')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Business calendar default (jam kerja WIB)
-- ---------------------------------------------------------------------------
INSERT INTO business_calendars (name, timezone, workdays, hours_from, hours_to, holidays, is_default)
VALUES ('Kalender Kerja NOC', 'Asia/Jakarta', '[1,2,3,4,5]'::jsonb, '08:00', '17:00', '[]'::jsonb, TRUE)
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Workflow definitions — task, reminder, rfs (aktif di F5–F7)
--                        incident, request, change (siap, dipakai di F10)
-- ---------------------------------------------------------------------------
INSERT INTO workflow_definitions (item_type, name, states_json, transitions_json, initial_state, terminal_states, is_default) VALUES
(
  'task', 'Alur Task Standar',
  '["open","in_progress","blocked","done","cancelled"]'::jsonb,
  '{"open":["in_progress","blocked","done","cancelled"],
    "in_progress":["blocked","done","cancelled"],
    "blocked":["in_progress","done","cancelled"],
    "done":["open"],
    "cancelled":["open"]}'::jsonb,
  'open', '["done","cancelled"]'::jsonb, TRUE
),
(
  'reminder', 'Alur Reminder Standar',
  '["scheduled","active","expiring","expired","cancelled"]'::jsonb,
  '{"scheduled":["active","cancelled"],
    "active":["expiring","expired","cancelled"],
    "expiring":["expired","active","cancelled"],
    "expired":["active"],
    "cancelled":[]}'::jsonb,
  'scheduled', '["expired","cancelled"]'::jsonb, TRUE
),
(
  'rfs', 'Alur RFS Standar',
  '["planned","in_progress","in_progress_field","activated","postponed","cancelled"]'::jsonb,
  '{"planned":["in_progress","postponed","cancelled"],
    "in_progress":["in_progress_field","activated","postponed","cancelled"],
    "in_progress_field":["activated","postponed","cancelled"],
    "activated":[],
    "postponed":["planned","in_progress","cancelled"],
    "cancelled":[]}'::jsonb,
  'planned', '["activated","cancelled"]'::jsonb, TRUE
),
(
  'incident', 'Alur Insiden NOC',
  '["new","assigned","in_progress","pending_customer","resolved","closed"]'::jsonb,
  '{"new":["assigned","in_progress","closed"],
    "assigned":["in_progress","pending_customer","resolved","closed"],
    "in_progress":["pending_customer","resolved","closed"],
    "pending_customer":["in_progress","resolved","closed"],
    "resolved":["closed","in_progress"],
    "closed":["in_progress"]}'::jsonb,
  'new', '["closed"]'::jsonb, TRUE
),
(
  'request', 'Alur Permintaan Layanan',
  '["new","assigned","in_progress","pending_customer","fulfilled","closed"]'::jsonb,
  '{"new":["assigned","in_progress","closed"],
    "assigned":["in_progress","pending_customer","fulfilled","closed"],
    "in_progress":["pending_customer","fulfilled","closed"],
    "pending_customer":["in_progress","fulfilled","closed"],
    "fulfilled":["closed"],
    "closed":["in_progress"]}'::jsonb,
  'new', '["closed"]'::jsonb, TRUE
),
(
  'change', 'Alur Change Request',
  '["draft","review","approved","scheduled","implementing","completed","rolled_back","cancelled"]'::jsonb,
  '{"draft":["review","cancelled"],
    "review":["approved","draft","cancelled"],
    "approved":["scheduled","cancelled"],
    "scheduled":["implementing","cancelled"],
    "implementing":["completed","rolled_back"],
    "completed":[],
    "rolled_back":[],
    "cancelled":[]}'::jsonb,
  'draft', '["completed","rolled_back","cancelled"]'::jsonb, TRUE
)
ON CONFLICT (item_type, name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Escalation policies
--
-- TRIAL-3D    : trial dedicated 3 hari  -> H-2, H-1, H-0, dan LATE-1H
-- RFS-DEFAULT : RFS mendekat           -> H-7, H-3, H-1, H-0, dan LATE-4H
-- ---------------------------------------------------------------------------
INSERT INTO escalation_policies (name, description, offsets_json, applies_to_item_types, quiet_hours_from, quiet_hours_to, max_attempts, is_default) VALUES
(
  'TRIAL-3D',
  'Reminder trial dedicated 3 hari: H-2, H-1, H-0, lalu eskalasi 1 jam setelah expired bila belum ada aktivasi.',
  '[{"label":"H-2","hours_before":48,"severity":"info"},
    {"label":"H-1","hours_before":24,"severity":"warning"},
    {"label":"H-0","hours_before":0,"severity":"critical"},
    {"label":"LATE-1H","hours_after":1,"severity":"critical"}]'::jsonb,
  '["reminder"]'::jsonb, '21:00', '08:00', 5, TRUE
),
(
  'RFS-DEFAULT',
  'Pengingat RFS untuk NOC: H-7, H-3, H-1, H-0, dan eskalasi 4 jam setelah RFS terlewat.',
  '[{"label":"H-7","hours_before":168,"severity":"info"},
    {"label":"H-3","hours_before":72,"severity":"warning"},
    {"label":"H-1","hours_before":24,"severity":"warning"},
    {"label":"H-0","hours_before":0,"severity":"critical"},
    {"label":"LATE-4H","hours_after":4,"severity":"critical"}]'::jsonb,
  '["rfs"]'::jsonb, '21:00', '08:00', 5, TRUE
)
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- SLA policy placeholder (mesin SLA baru aktif di F11)
-- ---------------------------------------------------------------------------
INSERT INTO sla_policies (name, item_type, priority, is_default, description)
SELECT 'SLA-NOC-DEFAULT', 'incident', NULL, TRUE,
       'Placeholder SLA NOC: respons pertama 15 menit, penyelesaian 4 jam. Mesin SLA diaktifkan pada fase F11.'
WHERE NOT EXISTS (SELECT 1 FROM sla_policies WHERE name = 'SLA-NOC-DEFAULT');

INSERT INTO sla_targets (sla_policy_id, metric, target_minutes, warning_pct, business_hours_only)
SELECT p.id, t.metric, t.target_minutes, t.warning_pct, t.business_hours_only
FROM sla_policies p
CROSS JOIN (VALUES
  ('first_response', 15,  80, TRUE),
  ('resolution',     240, 80, TRUE)
) AS t(metric, target_minutes, warning_pct, business_hours_only)
WHERE p.name = 'SLA-NOC-DEFAULT'
  AND NOT EXISTS (SELECT 1 FROM sla_targets st WHERE st.sla_policy_id = p.id);

-- ---------------------------------------------------------------------------
-- Notification target default: NOC-Team
-- (binding Telegram/WhatsApp ditambahkan nanti dari panel setelah kredensial ada)
-- ---------------------------------------------------------------------------
INSERT INTO notification_targets (name, kind, notes)
SELECT 'NOC-Team', 'group', 'Target default untuk seluruh notifikasi operasional NOC.'
WHERE NOT EXISTS (SELECT 1 FROM notification_targets WHERE name = 'NOC-Team');

-- ---------------------------------------------------------------------------
-- Notification templates
--
-- Placeholder yang tersedia (lihat DEVELOPMENT.md §7):
--   {{.RefNo}} {{.Title}} {{.Priority}} {{.Status}} {{.Owner}} {{.Requester}}
--   {{.Team}} {{.DueAt}} {{.ExpireAt}} {{.Remaining}} {{.OffsetLabel}} {{.Severity}}
--   {{.CustomerName}} {{.ServicePackage}} {{.Bandwidth}} {{.PicNOC}} {{.PicSales}}
--   {{.DeviceRef}} {{.Site}} {{.Notes}} {{.CreatedBy}}
--
-- CATATAN (temuan A11): baris dengan channel NULL tidak ter-deduplikasi oleh
-- ON CONFLICT (key, channel) karena PostgreSQL memperlakukan NULL sebagai
-- nilai berbeda. Karena itu baris ber-channel eksplisit dan baris tanpa
-- channel ditangani dengan DUPLIKASI yang berbeda, dan baris tanpa channel
-- dilindungi indeks unik parsial (lihat migrasi 00003).
-- ---------------------------------------------------------------------------

-- Baris DENGAN channel spesifik (aman memakai ON CONFLICT (key, channel)).
INSERT INTO notification_templates (key, item_type, channel, subject_tpl, body_tpl, severity, description) VALUES
(
  'TODO_CREATED', 'task', 'telegram',
  '',
  E'✅ TODO BARU {{.RefNo}}\n{{.Title}}\n\nPrioritas : {{.Priority}}\nOwner     : {{.Owner}}\nDue       : {{.DueAt}}\nDibuat    : {{.CreatedBy}}',
  'info', 'Notifikasi saat todo task dibuat.'
),
(
  'TODO_CREATED', 'task', 'whatsapp',
  '',
  E'*TODO BARU* {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{.Owner}}\nDue: {{.DueAt}}',
  'info', 'Notifikasi saat todo task dibuat (WhatsApp).'
),
(
  'REMINDER_OFFSET', 'reminder', 'telegram',
  '',
  E'⏰ REMINDER {{.OffsetLabel}}\n{{.Title}}\n\nSubjek : {{.SubjectName}}\nExpire : {{.ExpireAt}}\nSisa   : {{.Remaining}}\nDevice : {{.DeviceRef}}',
  'warning', 'Reminder pada setiap offset H-n (H-7/H-3/H-2/H-1).'
),
(
  'REMINDER_OFFSET', 'reminder', 'whatsapp',
  '',
  E'*REMINDER {{.OffsetLabel}}*\n{{.Title}}\nSubjek: {{.SubjectName}}\nExpire: {{.ExpireAt}}\nSisa: {{.Remaining}}',
  'warning', 'Reminder pada setiap offset H-n (WhatsApp).'
),
(
  'REMINDER_DUE_TODAY', 'reminder', 'telegram',
  '',
  E'🔴 HARI INI EXPIRED\n{{.Title}}\n\nSubjek : {{.SubjectName}}\nExpire : {{.ExpireAt}}\n\nAksi NOC: konfirmasi aktivasi atau eksekusi sesuai SOP.',
  'critical', 'Reminder saat hari-H (H-0).'
),
(
  'REMINDER_DUE_TODAY', 'reminder', 'whatsapp',
  '',
  E'*HARI INI EXPIRED*\n{{.Title}}\nSubjek: {{.SubjectName}}\nExpire: {{.ExpireAt}}\n\nAksi NOC: konfirmasi aktivasi.',
  'critical', 'Reminder saat hari-H (H-0) via WhatsApp.'
),
(
  'REMINDER_LATE', 'reminder', 'telegram',
  '',
  E'🚨 TERLAMBAT — BELUM ADA AKTIVASI\n{{.Title}}\n\nSubjek   : {{.SubjectName}}\nExpire   : {{.ExpireAt}}\nTerlewat : {{.Remaining}}\nPIC NOC  : {{.PicNOC}}\n\nMohon segera ditindaklanjuti.',
  'critical', 'Eskalasi setelah expired tanpa aktivasi.'
),
(
  'REMINDER_LATE', 'reminder', 'whatsapp',
  '',
  E'*TERLAMBAT - BELUM ADA AKTIVASI*\n{{.Title}}\nSubjek: {{.SubjectName}}\nExpire: {{.ExpireAt}}\nPIC NOC: {{.PicNOC}}',
  'critical', 'Eskalasi setelah expired tanpa aktivasi (WhatsApp).'
),
(
  'RFS_UPCOMING', 'rfs', 'telegram',
  '',
  E'📅 RFS {{.OffsetLabel}}\n{{.Title}}\n\nCustomer : {{.CustomerName}}\nPaket    : {{.ServicePackage}} / {{.Bandwidth}}\nRFS      : {{.ExpireAt}}\nSite     : {{.Site}}\nDevice   : {{.DeviceRef}}\nPIC NOC  : {{.PicNOC}}\nSales    : {{.PicSales}}',
  'warning', 'Pengingat RFS mendekat (H-7/H-3/H-1).'
),
(
  'RFS_UPCOMING', 'rfs', 'whatsapp',
  '',
  E'*RFS {{.OffsetLabel}}*\nCustomer: {{.CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{.ExpireAt}}\nPIC NOC: {{.PicNOC}}',
  'warning', 'Pengingat RFS mendekat (WhatsApp).'
),
(
  'RFS_TODAY', 'rfs', 'telegram',
  '',
  E'🚨 RFS HARI INI\n{{.Title}}\n\nCustomer : {{.CustomerName}}\nPaket    : {{.ServicePackage}} / {{.Bandwidth}}\nRFS      : {{.ExpireAt}}\nSite     : {{.Site}}\nDevice   : {{.DeviceRef}}\nPIC NOC  : {{.PicNOC}}\nSales    : {{.PicSales}}\n\nMohon pastikan layanan siap aktif.',
  'critical', 'RFS hari-H.'
),
(
  'RFS_TODAY', 'rfs', 'whatsapp',
  '',
  E'*RFS HARI INI*\nCustomer: {{.CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{.ExpireAt}}\nPIC NOC: {{.PicNOC}}',
  'critical', 'RFS hari-H (WhatsApp).'
),
(
  'RFS_LATE', 'rfs', 'telegram',
  '',
  E'🚨 RFS TERLEWAT\n{{.Title}}\n\nCustomer : {{.CustomerName}}\nRFS      : {{.ExpireAt}}\nTerlewat : {{.Remaining}}\nPIC NOC  : {{.PicNOC}}\n\nPerlu konfirmasi status instalasi.',
  'critical', 'RFS terlewat dari jadwal.'
),
(
  'TEST_MESSAGE', NULL, 'telegram',
  '',
  E'✅ Ingat.in — pesan uji\n\nKanal notifikasi berfungsi.\nTarget : {{.TargetName}}\nChannel: {{.Channel}}\nWaktu  : {{.CreatedAt}}',
  'info', 'Pesan uji dari panel Providers/Targets.'
),
(
  'TEST_MESSAGE', NULL, 'whatsapp',
  '',
  E'*Ingat.in — pesan uji*\nKanal notifikasi berfungsi.\nTarget: {{.TargetName}}\nWaktu: {{.CreatedAt}}',
  'info', 'Pesan uji dari panel Providers/Targets (WhatsApp).'
),
(
  'SLA_WARNING', NULL, 'telegram',
  '',
  E'⚠️ SLA MENDEKATI BATAS\n{{.RefNo}} — {{.Title}}\n\nStatus : {{.Status}}\nOwner  : {{.Owner}}\nTeam   : {{.Team}}',
  'warning', 'Peringatan SLA pada warning_pct (aktif F11).'
),
(
  'SLA_WARNING', NULL, 'whatsapp',
  '',
  E'*SLA MENDEKATI BATAS*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{.Owner}}',
  'warning', 'Peringatan SLA pada warning_pct (WhatsApp, aktif F11).'
),
(
  'SLA_BREACH', NULL, 'telegram',
  '',
  E'🔴 SLA TERLAMPAUI\n{{.RefNo}} — {{.Title}}\n\nStatus : {{.Status}}\nOwner  : {{.Owner}}\nTeam   : {{.Team}}',
  'critical', 'Pelanggaran SLA (aktif F11).'
),
(
  'SLA_BREACH', NULL, 'whatsapp',
  '',
  E'*SLA TERLAMPAUI*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{.Owner}}',
  'critical', 'Pelanggaran SLA (WhatsApp, aktif F11).'
)
ON CONFLICT (key, channel) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Settings awal
-- ---------------------------------------------------------------------------
INSERT INTO settings (key, value, updated_by) VALUES
  ('general', '{"app_name":"Ingat.in","timezone":"Asia/Jakarta","locale":"id-ID"}'::jsonb, 'seed'),
  ('working_hours', '{"from":"08:00","to":"17:00","workdays":[1,2,3,4,5],"timezone":"Asia/Jakarta"}'::jsonb, 'seed'),
  ('quiet_hours', '{"enabled":true,"from":"21:00","to":"08:00","allow_critical":true}'::jsonb, 'seed'),
  ('notifications', '{"outbox_batch_size":50,"max_attempts":5,"retention_days":90,"digest_enabled":true,"digest_at":"08:00"}'::jsonb, 'seed'),
  ('reminders', '{"default_trial_hours":72,"default_escalation_policy":"TRIAL-3D","default_rfs_policy":"RFS-DEFAULT"}'::jsonb, 'seed'),
  ('retention', '{"audit_days":90,"outbox_days":90,"backup_days":14}'::jsonb, 'seed'),
  ('security', '{"session_idle_timeout_minutes":0,"access_token_minutes":4320,"refresh_token_days":30}'::jsonb, 'seed')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM settings WHERE updated_by = 'seed';
DELETE FROM notification_templates WHERE key IN
  ('TODO_CREATED','REMINDER_OFFSET','REMINDER_DUE_TODAY','REMINDER_LATE',
   'RFS_UPCOMING','RFS_TODAY','RFS_LATE','TEST_MESSAGE','SLA_WARNING','SLA_BREACH');
DELETE FROM notification_targets WHERE name = 'NOC-Team';
DELETE FROM sla_targets WHERE sla_policy_id IN (SELECT id FROM sla_policies WHERE name = 'SLA-NOC-DEFAULT');
DELETE FROM sla_policies WHERE name = 'SLA-NOC-DEFAULT';
DELETE FROM escalation_policies WHERE name IN ('TRIAL-3D','RFS-DEFAULT');
DELETE FROM workflow_definitions WHERE name IN
  ('Alur Task Standar','Alur Reminder Standar','Alur RFS Standar',
   'Alur Insiden NOC','Alur Permintaan Layanan','Alur Change Request');
DELETE FROM business_calendars WHERE name = 'Kalender Kerja NOC';
DELETE FROM teams WHERE name IN ('NOC','Sales','Field');
DELETE FROM organizations WHERE code = 'INTERNAL';
