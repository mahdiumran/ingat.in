-- +goose Up

-- ============================================================================
-- Perubahan definisi status Todo (item_type='task').
--
-- Status lama : open / in_progress / blocked / done / cancelled
-- Status baru : accepted / on_progress / expired / canceled / closed
--
-- Tabel workflow_definitions bersifat informatif (state machine aktual
-- ditentukan workitems.DefaultWorkflows di kode). Migrasi ini menjaga baris
-- tabel tetap sinkron dan memetakan data lama agar tidak ada baris work_items
-- yang menyandang status tak dikenal.
-- ============================================================================

-- 1) Petakan status lama ke status baru pada data yang sudah ada.
UPDATE work_items SET status = 'accepted'    WHERE item_type = 'task' AND status = 'open';
UPDATE work_items SET status = 'on_progress' WHERE item_type = 'task' AND status = 'in_progress';
UPDATE work_items SET status = 'expired'     WHERE item_type = 'task' AND status = 'blocked';
UPDATE work_items SET status = 'closed'      WHERE item_type = 'task' AND status = 'done';
UPDATE work_items SET status = 'canceled'    WHERE item_type = 'task' AND status = 'cancelled';

-- 2) Perbarui definisi workflow task.
UPDATE workflow_definitions SET
    states_json      = '["accepted","on_progress","expired","canceled","closed"]'::jsonb,
    transitions_json = '{"accepted":["on_progress","expired","canceled","closed"],
                        "on_progress":["expired","canceled","closed"],
                        "expired":["accepted","canceled","closed"],
                        "canceled":["accepted"],
                        "closed":["accepted"]}'::jsonb,
    initial_state    = 'accepted',
    terminal_states  = '["closed","canceled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'task';

-- +goose Down

UPDATE work_items SET status = 'open'        WHERE item_type = 'task' AND status = 'accepted';
UPDATE work_items SET status = 'in_progress' WHERE item_type = 'task' AND status = 'on_progress';
UPDATE work_items SET status = 'blocked'     WHERE item_type = 'task' AND status = 'expired';
UPDATE work_items SET status = 'done'        WHERE item_type = 'task' AND status = 'closed';
UPDATE work_items SET status = 'cancelled'   WHERE item_type = 'task' AND status = 'canceled';

UPDATE workflow_definitions SET
    states_json      = '["open","in_progress","blocked","done","cancelled"]'::jsonb,
    transitions_json = '{"open":["in_progress","blocked","done","cancelled"],
                        "in_progress":["blocked","done","cancelled"],
                        "blocked":["in_progress","done","cancelled"],
                        "done":["open"],
                        "cancelled":["open"]}'::jsonb,
    initial_state    = 'open',
    terminal_states  = '["done","cancelled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'task';
