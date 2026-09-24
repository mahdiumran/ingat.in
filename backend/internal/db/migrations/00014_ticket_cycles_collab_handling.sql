-- +goose Up

-- ============================================================================
-- F20/F21 — SLA per siklus, collaborator, catatan penanganan, force unlock.
--
-- 1) ticket_sla_cycles
--    Rincian SLA PER SIKLUS. Siklus 0 = pekerjaan awal sejak tiket dibuat;
--    siklus 1..n = setiap kali tiket dibuka kembali (reopen). Riwayat siklus
--    TIDAK pernah dihapus; saat reopen, siklus baru dibuka dan
--    work_items.closed_at dikosongkan sebagai penanda "sedang dikerjakan".
--
-- 2) ticket_collaborators
--    1 owner utama (work_items.owner_username) + banyak NOC yang "ikut
--    menangani". Semua yang terlibat mendapat kredit KPI setara.
--
-- 3) ticket_details: issue_found / troubleshooting / action_solution
--
-- 4) event_type baru: 'unlocked' (dipakai saat admin memaksa buka tiket).
--
-- 5) Seed SLA policy PER PRIORITAS untuk incident/request/change (target sama,
--    kalender 24 jam). work_items.sla_policy_id di-backfill.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1) Siklus SLA
-- ---------------------------------------------------------------------------
CREATE TABLE ticket_sla_cycles (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id       UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    -- 0 = siklus awal; 1.. = reopen ke-1, ke-2, dst.
    cycle_no           INT NOT NULL DEFAULT 0,
    opened_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Waktu siklus ini dibuka kembali (NULL untuk siklus awal).
    reopened_at        TIMESTAMPTZ,
    -- Waktu siklus ini ditutup; NULL berarti masih berjalan.
    closed_at          TIMESTAMPTZ,
    first_response_at  TIMESTAMPTZ,
    -- Owner saat siklus ini ditutup (untuk KPI per person).
    handler_username   TEXT NOT NULL DEFAULT '',
    closed_by          TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (work_item_id, cycle_no)
);

CREATE INDEX ix_ticket_sla_cycles_item ON ticket_sla_cycles(work_item_id);
CREATE INDEX ix_ticket_sla_cycles_closed ON ticket_sla_cycles(closed_at);

-- Backfill: satu siklus awal untuk setiap tiket yang sudah ada.
INSERT INTO ticket_sla_cycles
    (work_item_id, cycle_no, opened_at, closed_at, first_response_at,
     handler_username, closed_by)
SELECT w.id, 0, w.created_at, w.closed_at, w.first_response_at,
       w.owner_username, ''
FROM work_items w
WHERE w.item_type IN ('incident','request','change')
  AND NOT EXISTS (SELECT 1 FROM ticket_sla_cycles c WHERE c.work_item_id = w.id);

-- ---------------------------------------------------------------------------
-- 2) Collaborator ("ikut menangani")
-- ---------------------------------------------------------------------------
CREATE TABLE ticket_collaborators (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    username     TEXT NOT NULL,
    added_by     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (work_item_id, username)
);

CREATE INDEX ix_ticket_collaborators_user ON ticket_collaborators(username);

-- ---------------------------------------------------------------------------
-- 3) Catatan penanganan tiket
-- ---------------------------------------------------------------------------
ALTER TABLE ticket_details ADD COLUMN IF NOT EXISTS issue_found     TEXT NOT NULL DEFAULT '';
ALTER TABLE ticket_details ADD COLUMN IF NOT EXISTS troubleshooting TEXT NOT NULL DEFAULT '';
ALTER TABLE ticket_details ADD COLUMN IF NOT EXISTS action_solution TEXT NOT NULL DEFAULT '';

-- ---------------------------------------------------------------------------
-- 4) Event 'unlocked'
-- ---------------------------------------------------------------------------
ALTER TABLE work_item_events DROP CONSTRAINT IF EXISTS work_item_events_event_type_check;
ALTER TABLE work_item_events ADD CONSTRAINT work_item_events_event_type_check
    CHECK (event_type IN ('created','updated','status_changed','stage_changed',
                          'assigned','unassigned','escalated','reopened','commented',
                          'attachment_added','due_changed','expire_changed','sla_warning',
                          'sla_breached','sla_paused','sla_resumed','resolved','closed',
                          'cancelled','notified','notify_failed','unlocked'));

-- ---------------------------------------------------------------------------
-- 5) SLA policy per prioritas untuk incident/request/change
--    (target sama untuk ketiga tipe; kalender 24 jam => business_hours_only=FALSE)
-- ---------------------------------------------------------------------------

-- 5a) Policy (satu per item_type × priority).
INSERT INTO sla_policies (name, item_type, priority, is_default, is_active, description)
SELECT 'SLA-' || upper(t.item_type) || '-' || upper(tg.priority),
       t.item_type, tg.priority, FALSE, TRUE,
       'SLA ' || t.item_type || ' prioritas ' || tg.priority || ' (24 jam).'
FROM (VALUES ('incident'), ('request'), ('change')) AS t(item_type)
CROSS JOIN (VALUES ('critical'), ('high'), ('normal'), ('low')) AS tg(priority)
ON CONFLICT (name) DO NOTHING;

-- 5b) Target first_response per policy.
INSERT INTO sla_targets (sla_policy_id, metric, target_minutes, warning_pct, business_hours_only)
SELECT p.id, 'first_response',
       CASE p.priority WHEN 'critical' THEN 10 WHEN 'high' THEN 15 WHEN 'normal' THEN 30 ELSE 60 END,
       80, FALSE
FROM sla_policies p
WHERE p.name LIKE 'SLA-INCIDENT-%' OR p.name LIKE 'SLA-REQUEST-%' OR p.name LIKE 'SLA-CHANGE-%'
  AND NOT EXISTS (SELECT 1 FROM sla_targets st WHERE st.sla_policy_id = p.id AND st.metric = 'first_response');

-- 5c) Target resolution per policy.
INSERT INTO sla_targets (sla_policy_id, metric, target_minutes, warning_pct, business_hours_only)
SELECT p.id, 'resolution',
       CASE p.priority WHEN 'critical' THEN 120 WHEN 'high' THEN 240 WHEN 'normal' THEN 480 ELSE 960 END,
       80, FALSE
FROM sla_policies p
WHERE (p.name LIKE 'SLA-INCIDENT-%' OR p.name LIKE 'SLA-REQUEST-%' OR p.name LIKE 'SLA-CHANGE-%')
  AND NOT EXISTS (SELECT 1 FROM sla_targets st WHERE st.sla_policy_id = p.id AND st.metric = 'resolution');

-- 5d) Kebijakan yang sudah ada sebelumnya: kalender 24 jam.
UPDATE sla_targets SET business_hours_only = FALSE;

-- 5e) Backfill sla_policy_id untuk tiket lama sesuai (item_type, priority).
UPDATE work_items w
SET sla_policy_id = p.id
FROM sla_policies p
WHERE w.item_type IN ('incident','request','change')
  AND w.sla_policy_id IS NULL
  AND p.item_type = w.item_type
  AND p.priority = w.priority
  AND p.is_active;

-- +goose Down

ALTER TABLE work_item_events DROP CONSTRAINT IF EXISTS work_item_events_event_type_check;
ALTER TABLE work_item_events ADD CONSTRAINT work_item_events_event_type_check
    CHECK (event_type IN ('created','updated','status_changed','stage_changed',
                          'assigned','unassigned','escalated','reopened','commented',
                          'attachment_added','due_changed','expire_changed','sla_warning',
                          'sla_breached','sla_paused','sla_resumed','resolved','closed',
                          'cancelled','notified','notify_failed'));

DELETE FROM sla_targets st
 USING sla_policies p
 WHERE st.sla_policy_id = p.id
   AND (p.name LIKE 'SLA-INCIDENT-%' OR p.name LIKE 'SLA-REQUEST-%' OR p.name LIKE 'SLA-CHANGE-%');
DELETE FROM sla_policies
 WHERE name LIKE 'SLA-INCIDENT-%' OR name LIKE 'SLA-REQUEST-%' OR name LIKE 'SLA-CHANGE-%';

ALTER TABLE ticket_details DROP COLUMN IF EXISTS action_solution;
ALTER TABLE ticket_details DROP COLUMN IF EXISTS troubleshooting;
ALTER TABLE ticket_details DROP COLUMN IF EXISTS issue_found;

DROP TABLE IF EXISTS ticket_collaborators;
DROP TABLE IF EXISTS ticket_sla_cycles;
