-- +goose Up
INSERT INTO organizations (name, code, contacts)
VALUES ('Internal', 'INTERNAL', '{}'::jsonb)
ON CONFLICT (code) DO NOTHING;

INSERT INTO teams (name, description) VALUES
    ('NOC', 'Network Operations Center'),
    ('Sales', 'Tim Sales'),
    ('Field Operations', 'Tim operasional lapangan')
ON CONFLICT (name) DO NOTHING;

INSERT INTO workflow_definitions
    (item_type, name, states_json, transitions_json, initial_state, terminal_states_json, is_default)
VALUES
    ('task', 'Alur Task Standar',
     '["open","in_progress","blocked","done","cancelled"]'::jsonb,
     '{"open":["in_progress","blocked","cancelled"],"in_progress":["blocked","done","cancelled"],"blocked":["in_progress","cancelled"],"done":["open"],"cancelled":["open"]}'::jsonb,
     'open', '["done","cancelled"]'::jsonb, TRUE),
    ('reminder', 'Alur Reminder Standar',
     '["scheduled","active","expiring","expired","cancelled"]'::jsonb,
     '{"scheduled":["active","cancelled"],"active":["expiring","expired","cancelled"],"expiring":["expired","active","cancelled"],"expired":["active"],"cancelled":[]}'::jsonb,
     'scheduled', '["expired","cancelled"]'::jsonb, TRUE),
    ('rfs', 'Alur RFS Standar',
     '["planned","in_progress","in_progress_field","activated","postponed","cancelled"]'::jsonb,
     '{"planned":["in_progress","postponed","cancelled"],"in_progress":["in_progress_field","activated","postponed","cancelled"],"in_progress_field":["activated","postponed","cancelled"],"activated":[],"postponed":["planned","in_progress","cancelled"],"cancelled":[]}'::jsonb,
     'planned', '["activated","cancelled"]'::jsonb, TRUE),
    ('incident', 'Alur Insiden NOC',
     '["new","assigned","in_progress","pending_customer","resolved","closed"]'::jsonb,
     '{"new":["assigned","in_progress","closed"],"assigned":["in_progress","pending_customer","resolved","closed"],"in_progress":["pending_customer","resolved","closed"],"pending_customer":["in_progress","resolved","closed"],"resolved":["closed","in_progress"],"closed":["in_progress"]}'::jsonb,
     'new', '["closed"]'::jsonb, TRUE),
    ('request', 'Alur Permintaan Layanan',
     '["new","assigned","in_progress","pending_customer","fulfilled","closed"]'::jsonb,
     '{"new":["assigned","in_progress","closed"],"assigned":["in_progress","pending_customer","fulfilled","closed"],"in_progress":["pending_customer","fulfilled","closed"],"pending_customer":["in_progress","fulfilled","closed"],"fulfilled":["closed"],"closed":["in_progress"]}'::jsonb,
     'new', '["closed"]'::jsonb, TRUE),
    ('change', 'Alur Change Request',
     '["draft","review","approved","scheduled","implementing","completed","rolled_back","cancelled"]'::jsonb,
     '{"draft":["review","cancelled"],"review":["approved","draft","cancelled"],"approved":["scheduled","cancelled"],"scheduled":["implementing","cancelled"],"implementing":["completed","rolled_back"],"completed":[],"rolled_back":[],"cancelled":[]}'::jsonb,
     'draft', '["completed","rolled_back","cancelled"]'::jsonb, TRUE)
ON CONFLICT (item_type, name) DO NOTHING;

INSERT INTO escalation_policies
    (name, description, offsets_json, applies_to_item_types, max_attempts, is_default)
VALUES
    ('TRIAL-3D', 'Eskalasi reminder trial tiga hari',
     '[{"label":"H-2","hours_before":48,"severity":"info"},{"label":"H-1","hours_before":24,"severity":"warning"},{"label":"H-0","hours_before":0,"severity":"critical"},{"label":"LATE-1H","hours_after":1,"severity":"critical"}]'::jsonb,
     '["reminder"]'::jsonb, 5, TRUE),
    ('RFS-DEFAULT', 'Eskalasi jadwal RFS standar',
     '[{"label":"H-7","hours_before":168,"severity":"info"},{"label":"H-3","hours_before":72,"severity":"info"},{"label":"H-1","hours_before":24,"severity":"warning"},{"label":"H-0","hours_before":0,"severity":"critical"},{"label":"LATE-4H","hours_after":4,"severity":"critical"}]'::jsonb,
     '["rfs"]'::jsonb, 5, FALSE)
ON CONFLICT (name) DO NOTHING;

INSERT INTO sla_policies (name, description, warning_pct, is_default)
VALUES ('DEFAULT', 'Kebijakan SLA awal', 80, TRUE)
ON CONFLICT (name) DO NOTHING;

INSERT INTO notification_targets (name, kind, notes, organization_id)
SELECT 'NOC-Team', 'group', 'Target notifikasi NOC bawaan', id
FROM organizations WHERE code = 'INTERNAL'
ON CONFLICT (name) DO NOTHING;

INSERT INTO notification_templates
    (key, item_type, channel, subject_tpl, body_tpl, severity, description)
VALUES
    ('TODO_CREATED', 'task', 'telegram', '', E'✅ TODO BARU {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{dash .Owner}}\nDue: {{dash .DueAt}}', 'info', 'Notifikasi Todo baru'),
    ('TODO_CREATED', 'task', 'whatsapp', '', E'*TODO BARU* {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{dash .Owner}}\nDue: {{dash .DueAt}}', 'info', 'Notifikasi Todo baru'),
    ('REMINDER_OFFSET', 'reminder', 'telegram', '', E'⏰ REMINDER {{.OffsetLabel}}\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nSisa: {{dash .Remaining}}', 'warning', 'Reminder sebelum jatuh tempo'),
    ('REMINDER_OFFSET', 'reminder', 'whatsapp', '', E'*REMINDER {{.OffsetLabel}}*\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nSisa: {{dash .Remaining}}', 'warning', 'Reminder sebelum jatuh tempo'),
    ('REMINDER_DUE_TODAY', 'reminder', 'telegram', '', E'🔴 HARI INI EXPIRED\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}', 'critical', 'Reminder jatuh tempo hari ini'),
    ('REMINDER_DUE_TODAY', 'reminder', 'whatsapp', '', E'*HARI INI EXPIRED*\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}', 'critical', 'Reminder jatuh tempo hari ini'),
    ('REMINDER_LATE', 'reminder', 'telegram', '', E'🚨 TERLAMBAT — BELUM ADA AKTIVASI\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNOC}}', 'critical', 'Reminder melewati jatuh tempo'),
    ('REMINDER_LATE', 'reminder', 'whatsapp', '', E'*TERLAMBAT — BELUM ADA AKTIVASI*\n{{.Title}}\n\nSubjek: {{dash .SubjectName}}\nExpire: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNOC}}', 'critical', 'Reminder melewati jatuh tempo'),
    ('RFS_UPCOMING', 'rfs', 'telegram', '', E'📅 RFS {{.OffsetLabel}}\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNoc}}', 'warning', 'RFS mendatang'),
    ('RFS_UPCOMING', 'rfs', 'whatsapp', '', E'*RFS {{.OffsetLabel}}*\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNoc}}', 'warning', 'RFS mendatang'),
    ('RFS_TODAY', 'rfs', 'telegram', '', E'🚨 RFS HARI INI\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNoc}}', 'critical', 'RFS hari ini'),
    ('RFS_TODAY', 'rfs', 'whatsapp', '', E'*RFS HARI INI*\nCustomer: {{dash .CustomerName}}\nPaket: {{.ServicePackage}} / {{.Bandwidth}}\nRFS: {{dash .ExpireAt}}\nPIC NOC: {{dash .PicNoc}}', 'critical', 'RFS hari ini'),
    ('RFS_LATE', 'rfs', 'telegram', '', E'🚨 RFS TERLEWAT\nCustomer: {{dash .CustomerName}}\nRFS: {{dash .ExpireAt}}\nTerlewat: {{dash .Remaining}}\nPIC NOC: {{dash .PicNoc}}', 'critical', 'RFS terlambat'),
    ('RFS_LATE', 'rfs', 'whatsapp', '', E'*RFS TERLEWAT*\nCustomer: {{dash .CustomerName}}\nRFS: {{dash .ExpireAt}}\nTerlewat: {{dash .Remaining}}\nPIC NOC: {{dash .PicNoc}}', 'critical', 'RFS terlambat'),
    ('TEST_MESSAGE', NULL, 'telegram', '', E'✅ Ingat.in — pesan uji\n\nTarget: {{dash .TargetName}}\nChannel: {{dash .Channel}}\nWaktu: {{.CreatedAt}}', 'info', 'Pesan uji provider'),
    ('TEST_MESSAGE', NULL, 'whatsapp', '', E'*Ingat.in — pesan uji*\n\nTarget: {{dash .TargetName}}\nChannel: {{dash .Channel}}\nWaktu: {{.CreatedAt}}', 'info', 'Pesan uji provider'),
    ('SLA_WARNING', 'incident', 'telegram', '', E'⚠️ SLA MENDEKATI BATAS\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{dash .Owner}}', 'warning', 'Peringatan SLA'),
    ('SLA_WARNING', 'incident', 'whatsapp', '', E'*SLA MENDEKATI BATAS*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{dash .Owner}}', 'warning', 'Peringatan SLA'),
    ('SLA_BREACH', 'incident', 'telegram', '', E'🚨 SLA TERLEWATI\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{dash .Owner}}', 'critical', 'Pelanggaran SLA'),
    ('SLA_BREACH', 'incident', 'whatsapp', '', E'*SLA TERLEWATI*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{dash .Owner}}', 'critical', 'Pelanggaran SLA')
ON CONFLICT DO NOTHING;

INSERT INTO settings (key, value) VALUES
    ('timezone', '{"value":"Asia/Jakarta"}'::jsonb),
    ('working_hours', '{"from":"08:00","to":"17:00","days":[1,2,3,4,5]}'::jsonb),
    ('quiet_hours', '{"from":"22:00","to":"06:00"}'::jsonb),
    ('outbox_retention_days', '{"value":30}'::jsonb),
    ('audit_retention_days', '{"value":365}'::jsonb),
    ('notification_defaults', '{"max_attempts":5}'::jsonb)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM settings WHERE key IN (
    'timezone','working_hours','quiet_hours','outbox_retention_days',
    'audit_retention_days','notification_defaults'
);
DELETE FROM notification_templates WHERE key IN (
    'TODO_CREATED','REMINDER_OFFSET','REMINDER_DUE_TODAY','REMINDER_LATE',
    'RFS_UPCOMING','RFS_TODAY','RFS_LATE','TEST_MESSAGE','SLA_WARNING','SLA_BREACH'
);
DELETE FROM notification_targets WHERE name = 'NOC-Team';
DELETE FROM sla_policies WHERE name = 'DEFAULT';
DELETE FROM escalation_policies WHERE name IN ('TRIAL-3D','RFS-DEFAULT');
DELETE FROM workflow_definitions WHERE item_type IN ('task','reminder','rfs','incident','request','change');
DELETE FROM teams WHERE name IN ('NOC','Sales','Field Operations');
DELETE FROM organizations WHERE code = 'INTERNAL';
