-- +goose Up

-- ============================================================================
-- Status awal Aktivasi/EWO (RFS): `planned` → `new`.
--
-- Alur yang diinginkan produk:
--   new          → dibuat admin/sales (belum dikerjakan)
--   in_progress  → penangan menekan "Mulai Kerjakan" setelah jadwal tiba
--   activated    → penangan menekan "Close Ticket" (Closed / Completed)
--
-- Sebelumnya status awal adalah `planned`, sehingga RFS baru tampak "sudah
-- direncanakan" padahal belum ada yang mengerjakan. Status `planned` kini
-- hanya dikenal sebagai TAHAP INSTALASI (rfs_details.install_stage), bukan
-- status workflow.
--
-- Perubahan:
--   1. Sinkronkan baris workflow_definitions rfs dengan state machine aktual
--      di kode (workitems.DefaultWorkflows) agar tabel tetap informatif.
--   2. Pindahkan RFS yang masih berstatus `planned` ke `new` (hanya yang
--      belum selesai; activated/cancelled tidak disentuh).
-- ============================================================================

UPDATE workflow_definitions SET
    states_json      = '["new","in_progress","in_progress_field","activated","postponed","cancelled"]'::jsonb,
    transitions_json = '{"new":["in_progress","postponed","cancelled"],
                         "in_progress":["in_progress_field","activated","postponed","cancelled"],
                         "in_progress_field":["activated","postponed","cancelled"],
                         "activated":[],
                         "postponed":["new","in_progress","cancelled"],
                         "cancelled":[]}'::jsonb,
    initial_state    = 'new',
    terminal_states  = '["activated","cancelled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'rfs';

UPDATE work_items
   SET status     = 'new',
       updated_at = now()
 WHERE item_type = 'rfs'
   AND status = 'planned'
   AND NOT is_deleted;

-- +goose Down

UPDATE work_items
   SET status     = 'planned',
       updated_at = now()
 WHERE item_type = 'rfs'
   AND status = 'new'
   AND NOT is_deleted;

UPDATE workflow_definitions SET
    states_json      = '["planned","in_progress","in_progress_field","activated","postponed","cancelled"]'::jsonb,
    transitions_json = '{"planned":["in_progress","postponed","cancelled"],
                         "in_progress":["in_progress_field","activated","postponed","cancelled"],
                         "in_progress_field":["activated","postponed","cancelled"],
                         "activated":[],
                         "postponed":["planned","in_progress","cancelled"],
                         "cancelled":[]}'::jsonb,
    initial_state    = 'planned',
    terminal_states  = '["activated","cancelled"]'::jsonb,
    updated_at       = now()
WHERE item_type = 'rfs';
