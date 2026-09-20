-- +goose Up
ALTER TABLE work_items DROP CONSTRAINT ck_work_items_item_type;
ALTER TABLE work_items ADD CONSTRAINT ck_work_items_item_type
    CHECK (item_type IN ('task','reminder','rfs','incident','request','change','daily_task'));

INSERT INTO workflow_definitions
    (item_type, name, states_json, transitions_json, initial_state, terminal_states_json, is_default, is_active)
VALUES
    ('daily_task', 'Alur Daily Task',
     '["pending","in_progress","done","canceled"]'::jsonb,
     '{"pending":["in_progress","done","canceled"],"in_progress":["done","pending","canceled"],"done":["pending"],"canceled":["pending"]}'::jsonb,
     'pending', '["done","canceled"]'::jsonb, TRUE, TRUE)
ON CONFLICT (item_type, name) DO UPDATE SET
    states_json=EXCLUDED.states_json,
    transitions_json=EXCLUDED.transitions_json,
    initial_state=EXCLUDED.initial_state,
    terminal_states_json=EXCLUDED.terminal_states_json,
    is_default=EXCLUDED.is_default,
    is_active=EXCLUDED.is_active,
    updated_at=now();

INSERT INTO notification_templates
    (key, item_type, channel, subject_tpl, body_tpl, severity, description)
VALUES
    ('DAILY_TASK_CREATED', 'daily_task', 'telegram', '', E'📋 DAILY TASK BARU {{.RefNo}}\n{{.Title}}\n\nTanggal: {{dash .StartAt}}\nOwner: {{dash .Owner}}\n\nDeskripsi : {{.Description}}', 'info', 'Notifikasi Daily Task baru'),
    ('DAILY_TASK_CREATED', 'daily_task', 'whatsapp', '', E'*DAILY TASK BARU* {{.RefNo}}\n{{.Title}}\n\nTanggal: {{dash .StartAt}}\nOwner: {{dash .Owner}}\n\nDeskripsi : {{.Description}}', 'info', 'Notifikasi Daily Task baru')
ON CONFLICT DO NOTHING;

CREATE INDEX ix_work_items_daily ON work_items(item_type, start_at);

-- +goose Down
DROP INDEX IF EXISTS ix_work_items_daily;
DELETE FROM notification_templates WHERE key='DAILY_TASK_CREATED';
DELETE FROM workflow_definitions WHERE item_type='daily_task' AND name='Alur Daily Task';
ALTER TABLE work_items DROP CONSTRAINT ck_work_items_item_type;
ALTER TABLE work_items ADD CONSTRAINT ck_work_items_item_type
    CHECK (item_type IN ('task','reminder','rfs','incident','request','change'));
