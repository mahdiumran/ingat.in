-- +goose Up
INSERT INTO workflow_definitions
    (item_type, name, states_json, transitions_json, initial_state, terminal_states_json, is_default, is_active)
VALUES
    ('task', 'Alur Task Standar',
     '["accepted","on_progress","expired","canceled","closed"]'::jsonb,
     '{"accepted":["on_progress","expired","canceled","closed"],"on_progress":["expired","canceled","closed"],"expired":["accepted","canceled","closed"],"canceled":["accepted"],"closed":["accepted"]}'::jsonb,
     'accepted', '["expired","canceled","closed"]'::jsonb, TRUE, TRUE)
ON CONFLICT (item_type, name) DO UPDATE SET
    states_json=EXCLUDED.states_json,
    transitions_json=EXCLUDED.transitions_json,
    initial_state=EXCLUDED.initial_state,
    terminal_states_json=EXCLUDED.terminal_states_json,
    is_default=EXCLUDED.is_default,
    is_active=EXCLUDED.is_active,
    updated_at=now();

-- +goose Down
DELETE FROM workflow_definitions WHERE item_type='task' AND name='Alur Task Standar';
