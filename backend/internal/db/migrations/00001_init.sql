-- +goose Up

-- ============================================================================
-- Ingat.in — initial schema
--
-- F1 membuat SELURUH skema (termasuk yang belum dipakai UI) agar fase lanjutan
-- (ticketing F10, SLA F11) tidak memerlukan ALTER TABLE berat di produksi.
-- Kolom "masa depan" dibuat nullable sejak awal.
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- Organisasi (multi-tenant ringan; default satu baris "Internal")
-- ---------------------------------------------------------------------------
CREATE TABLE organizations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    code         TEXT NOT NULL UNIQUE,
    contacts     JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Team (NOC, Sales, Field, ...)
-- ---------------------------------------------------------------------------
CREATE TABLE teams (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    description  TEXT NOT NULL DEFAULT '',
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- User
--   role       : admin | agent | noc | sales | viewer
--   token_version dinaikkan saat revoke semua sesi (ganti password/logout-all)
--   team_id / organization_id nullable -> siap multi-tenant
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username           TEXT NOT NULL UNIQUE,
    email              TEXT NOT NULL DEFAULT '',
    full_name          TEXT NOT NULL DEFAULT '',
    password_hash      TEXT NOT NULL,
    role               TEXT NOT NULL DEFAULT 'viewer'
                       CHECK (role IN ('admin','agent','noc','sales','viewer','customer')),
    team_id            UUID REFERENCES teams(id) ON DELETE SET NULL,
    organization_id    UUID REFERENCES organizations(id) ON DELETE SET NULL,
    telegram_chat_id   TEXT NOT NULL DEFAULT '',
    wa_number          TEXT NOT NULL DEFAULT '',
    token_version      INT NOT NULL DEFAULT 0,
    is_active          BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at      TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_users_role ON users(role);
CREATE INDEX ix_users_team ON users(team_id);

-- ---------------------------------------------------------------------------
-- Refresh token (rotating; disimpan sebagai SHA-256 hash)
-- ---------------------------------------------------------------------------
CREATE TABLE refresh_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    rotated_from UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX ix_refresh_tokens_expires ON refresh_tokens(expires_at);

-- ---------------------------------------------------------------------------
-- Workflow definitions (state machine per item_type, tanpa deploy ulang)
-- ---------------------------------------------------------------------------
CREATE TABLE workflow_definitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_type       TEXT NOT NULL,
    name            TEXT NOT NULL,
    states_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    transitions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    initial_state   TEXT NOT NULL DEFAULT 'new',
    terminal_states JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (item_type, name)
);

CREATE INDEX ix_workflow_definitions_type ON workflow_definitions(item_type) WHERE is_active;

-- ---------------------------------------------------------------------------
-- Notification providers (kanal WhatsApp & lainnya; API key terenkripsi)
-- ---------------------------------------------------------------------------
CREATE TABLE notification_providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel         TEXT NOT NULL DEFAULT 'whatsapp'
                    CHECK (channel IN ('whatsapp','telegram','email','sms','slack','discord','webhook')),
    kind            TEXT NOT NULL
                    CHECK (kind IN ('waha','fonnte','wablas','starsender','custom_http',
                                    'telegram_bot','smtp','slack_webhook','discord_webhook','generic')),
    label           TEXT NOT NULL,
    base_url        TEXT NOT NULL DEFAULT '',
    api_key_enc     TEXT NOT NULL DEFAULT '',
    extra_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    last_test_at    TIMESTAMPTZ,
    last_test_ok    BOOLEAN,
    last_test_error TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_notification_providers_channel ON notification_providers(channel);

-- ---------------------------------------------------------------------------
-- SLA policies & targets (skema siap; mesin aktif di F11)
-- ---------------------------------------------------------------------------
CREATE TABLE business_calendars (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    timezone     TEXT NOT NULL DEFAULT 'Asia/Jakarta',
    workdays     JSONB NOT NULL DEFAULT '[1,2,3,4,5]'::jsonb,  -- 0=Min .. 6=Sab
    hours_from   TIME NOT NULL DEFAULT '08:00',
    hours_to     TIME NOT NULL DEFAULT '17:00',
    holidays     JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_default   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sla_policies (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    item_type    TEXT NOT NULL DEFAULT 'incident',
    priority     TEXT,
    calendar_id  UUID REFERENCES business_calendars(id) ON DELETE SET NULL,
    is_default   BOOLEAN NOT NULL DEFAULT FALSE,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sla_targets (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sla_policy_id      UUID NOT NULL REFERENCES sla_policies(id) ON DELETE CASCADE,
    metric             TEXT NOT NULL
                       CHECK (metric IN ('first_response','resolution','update')),
    target_minutes     INT NOT NULL CHECK (target_minutes > 0),
    warning_pct        INT NOT NULL DEFAULT 80 CHECK (warning_pct BETWEEN 1 AND 100),
    business_hours_only BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_sla_targets_policy ON sla_targets(sla_policy_id);

-- ---------------------------------------------------------------------------
-- Escalation policies (offset notifikasi berjenjang)
-- offsets_json: [{"label":"H-1","hours_before":24,"severity":"warning"}, ...]
-- ---------------------------------------------------------------------------
CREATE TABLE escalation_policies (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL UNIQUE,
    description           TEXT NOT NULL DEFAULT '',
    offsets_json          JSONB NOT NULL DEFAULT '[]'::jsonb,
    applies_to_item_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    quiet_hours_from      TIME,
    quiet_hours_to        TIME,
    max_attempts          INT NOT NULL DEFAULT 5,
    is_default            BOOLEAN NOT NULL DEFAULT FALSE,
    is_active             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Notification targets (grup/tim atau personal) + binding per kanal
-- ---------------------------------------------------------------------------
CREATE TABLE notification_targets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,
    kind          TEXT NOT NULL DEFAULT 'group' CHECK (kind IN ('group','personal')),
    notes         TEXT NOT NULL DEFAULT '',
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE target_bindings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id     UUID NOT NULL REFERENCES notification_targets(id) ON DELETE CASCADE,
    channel       TEXT NOT NULL
                  CHECK (channel IN ('whatsapp','telegram','email','sms','slack','discord','webhook')),
    destination   TEXT NOT NULL,
    provider_id   UUID REFERENCES notification_providers(id) ON DELETE SET NULL,
    label         TEXT NOT NULL DEFAULT '',
    is_primary    BOOLEAN NOT NULL DEFAULT TRUE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_target_bindings_target ON target_bindings(target_id);
CREATE INDEX ix_target_bindings_channel ON target_bindings(channel);

-- ---------------------------------------------------------------------------
-- Notification templates (text/template)
-- ---------------------------------------------------------------------------
CREATE TABLE notification_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key         TEXT NOT NULL,
    item_type   TEXT,
    channel     TEXT,
    subject_tpl TEXT NOT NULL DEFAULT '',
    body_tpl    TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT 'info'
                CHECK (severity IN ('info','warning','critical')),
    description TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (key, channel)
);

-- ---------------------------------------------------------------------------
-- WORK_ITEMS — inti generik untuk task | reminder | rfs | incident | request | change
--
-- Kolom masa depan (SLA, ticketing, multi-tenant) sudah ada dan nullable
-- sehingga tidak perlu ALTER TABLE saat F10/F11 dikerjakan.
-- ---------------------------------------------------------------------------
CREATE TABLE work_items (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_no               TEXT NOT NULL UNIQUE,
    item_type            TEXT NOT NULL
                         CHECK (item_type IN ('task','reminder','rfs','incident','request','change')),
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    priority             TEXT NOT NULL DEFAULT 'normal'
                         CHECK (priority IN ('low','normal','high','critical')),
    status               TEXT NOT NULL DEFAULT 'open',
    stage                TEXT NOT NULL DEFAULT '',
    owner_username       TEXT NOT NULL DEFAULT '',
    requester_username   TEXT NOT NULL DEFAULT '',
    team_id              UUID REFERENCES teams(id) ON DELETE SET NULL,
    organization_id      UUID REFERENCES organizations(id) ON DELETE SET NULL,
    target_id            UUID REFERENCES notification_targets(id) ON DELETE SET NULL,
    source               TEXT NOT NULL DEFAULT 'web',
    parent_id            UUID REFERENCES work_items(id) ON DELETE SET NULL,

    start_at             TIMESTAMPTZ,
    due_at               TIMESTAMPTZ,
    expire_at            TIMESTAMPTZ,

    -- SLA (diisi mesin SLA di F11)
    sla_policy_id        UUID REFERENCES sla_policies(id) ON DELETE SET NULL,
    sla_state            TEXT NOT NULL DEFAULT 'none'
                         CHECK (sla_state IN ('none','on_track','at_risk','breached','paused')),
    sla_breached_at      TIMESTAMPTZ,
    first_response_at    TIMESTAMPTZ,
    resolved_at          TIMESTAMPTZ,
    closed_at            TIMESTAMPTZ,
    paused_total_seconds BIGINT NOT NULL DEFAULT 0,

    -- referensi eksternal (biarkan sebagai teks: integrasi nanti)
    device_ref           TEXT NOT NULL DEFAULT '',
    service_ref          TEXT NOT NULL DEFAULT '',
    customer_ref         TEXT NOT NULL DEFAULT '',
    tags                 JSONB NOT NULL DEFAULT '[]'::jsonb,

    is_deleted           BOOLEAN NOT NULL DEFAULT FALSE,
    created_by           TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_work_items_type_status ON work_items(item_type, status) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_owner ON work_items(owner_username) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_expire ON work_items(expire_at) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_due ON work_items(due_at) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_team ON work_items(team_id) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_org ON work_items(organization_id) WHERE NOT is_deleted;
CREATE INDEX ix_work_items_parent ON work_items(parent_id);
CREATE INDEX ix_work_items_created ON work_items(created_at DESC);
CREATE INDEX ix_work_items_ref ON work_items(ref_no);

-- ---------------------------------------------------------------------------
-- Extension 1:1 per tipe (nullable, tidak mengubah inti)
-- ---------------------------------------------------------------------------
CREATE TABLE task_details (
    work_item_id     UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    checklist_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    progress_pct     INT NOT NULL DEFAULT 0 CHECK (progress_pct BETWEEN 0 AND 100),
    estimate_minutes INT
);

CREATE TABLE reminder_details (
    work_item_id         UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    category             TEXT NOT NULL DEFAULT 'generic'
                         CHECK (category IN ('trial','rfs','maintenance','generic','custom')),
    subject_name         TEXT NOT NULL DEFAULT '',
    subject_type         TEXT NOT NULL DEFAULT 'none'
                         CHECK (subject_type IN ('customer','service','device','none')),
    recurrence_rule      TEXT,
    escalation_policy_id UUID REFERENCES escalation_policies(id) ON DELETE SET NULL,
    activated_at         TIMESTAMPTZ,
    last_offset_fired    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX ix_reminder_details_category ON reminder_details(category);

CREATE TABLE rfs_details (
    work_item_id    UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    customer_name   TEXT NOT NULL DEFAULT '',
    service_id      TEXT NOT NULL DEFAULT '',
    service_package TEXT NOT NULL DEFAULT '',
    bandwidth       TEXT NOT NULL DEFAULT '',
    pic_noc         TEXT NOT NULL DEFAULT '',
    pic_sales       TEXT NOT NULL DEFAULT '',
    sales_username  TEXT NOT NULL DEFAULT '',
    site            TEXT NOT NULL DEFAULT '',
    install_stage   TEXT NOT NULL DEFAULT 'planned'
                    CHECK (install_stage IN ('planned','provisioning','delivered','activated','cancelled','postponed'))
);

-- Ticketing (F10). Dibuat sekarang supaya tidak ada migrasi skema saat F10.
CREATE TABLE ticket_details (
    work_item_id     UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    category         TEXT NOT NULL DEFAULT '',
    subcategory      TEXT NOT NULL DEFAULT '',
    impact           TEXT NOT NULL DEFAULT 'medium'
                     CHECK (impact IN ('low','medium','high')),
    urgency          TEXT NOT NULL DEFAULT 'medium'
                     CHECK (urgency IN ('low','medium','high')),
    assignment_group TEXT NOT NULL DEFAULT '',
    escalation_level INT NOT NULL DEFAULT 1,
    reopen_count     INT NOT NULL DEFAULT 0
);

CREATE TABLE ticket_queues (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    team_id      UUID REFERENCES teams(id) ON DELETE SET NULL,
    email_alias  TEXT NOT NULL DEFAULT '',
    target_id    UUID REFERENCES notification_targets(id) ON DELETE SET NULL,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ticket_queue_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id   UUID NOT NULL REFERENCES ticket_queues(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (queue_id, user_id)
);

CREATE TABLE ticket_categories (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    parent_id        UUID REFERENCES ticket_categories(id) ON DELETE CASCADE,
    default_queue_id UUID REFERENCES ticket_queues(id) ON DELETE SET NULL,
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, parent_id)
);

CREATE TABLE incident_links (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_work_item_id  UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    child_work_item_id   UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    link_type            TEXT NOT NULL DEFAULT 'related'
                         CHECK (link_type IN ('related','problem','change','duplicate','blocks')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (parent_work_item_id, child_work_item_id, link_type)
);

-- ---------------------------------------------------------------------------
-- Activity: events (append-only → sumber timeline + SLA + audit), komentar, lampiran, watcher
-- ---------------------------------------------------------------------------
CREATE TABLE work_item_events (
    id            BIGSERIAL PRIMARY KEY,
    work_item_id  UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL
                  CHECK (event_type IN ('created','updated','status_changed','stage_changed',
                                        'assigned','unassigned','escalated','reopened','commented',
                                        'attachment_added','due_changed','expire_changed','sla_warning',
                                        'sla_breached','sla_paused','sla_resumed','resolved','closed',
                                        'cancelled','notified','notify_failed')),
    actor_username TEXT NOT NULL DEFAULT '',
    from_value    TEXT NOT NULL DEFAULT '',
    to_value      TEXT NOT NULL DEFAULT '',
    detail_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_work_item_events_item ON work_item_events(work_item_id, created_at DESC);
CREATE INDEX ix_work_item_events_type ON work_item_events(event_type);
CREATE INDEX ix_work_item_events_created ON work_item_events(created_at DESC);

CREATE TABLE comments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id  UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    author_username TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL,
    is_internal   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_comments_item ON comments(work_item_id, created_at DESC);

CREATE TABLE attachments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    stored_path  TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    mime         TEXT NOT NULL DEFAULT '',
    uploaded_by  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_attachments_item ON attachments(work_item_id);

CREATE TABLE watchers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    user_id      UUID REFERENCES users(id) ON DELETE CASCADE,
    target_id    UUID REFERENCES notification_targets(id) ON DELETE CASCADE,
    added_by     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (user_id IS NOT NULL OR target_id IS NOT NULL)
);

CREATE INDEX ix_watchers_item ON watchers(work_item_id);

-- ---------------------------------------------------------------------------
-- Notification outbox — SATU pipa untuk semua tipe; idempotent via event_key
-- ---------------------------------------------------------------------------
CREATE TABLE notification_outbox (
    id                  BIGSERIAL PRIMARY KEY,
    work_item_id        UUID REFERENCES work_items(id) ON DELETE CASCADE,
    event_key           TEXT NOT NULL UNIQUE,
    source_type         TEXT NOT NULL DEFAULT 'work_item'
                        CHECK (source_type IN ('work_item','todo','reminder','rfs','ticket',
                                              'manual','test','system')),
    template_key        TEXT NOT NULL DEFAULT '',
    offset_label        TEXT NOT NULL DEFAULT '',
    severity            TEXT NOT NULL DEFAULT 'info'
                        CHECK (severity IN ('info','warning','critical')),
    channel             TEXT NOT NULL
                        CHECK (channel IN ('whatsapp','telegram','email','sms','slack','discord','webhook')),
    destination         TEXT NOT NULL,
    provider_id         UUID REFERENCES notification_providers(id) ON DELETE SET NULL,
    payload_json        JSONB NOT NULL DEFAULT '{}'::jsonb,
    rendered_subject    TEXT NOT NULL DEFAULT '',
    rendered_body       TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','sending','sent','failed','suppressed')),
    attempts            INT NOT NULL DEFAULT 0,
    max_attempts        INT NOT NULL DEFAULT 5,
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error          TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_outbox_dispatch ON notification_outbox(status, next_attempt_at);
CREATE INDEX ix_outbox_item ON notification_outbox(work_item_id);
CREATE INDEX ix_outbox_created ON notification_outbox(created_at DESC);

-- ---------------------------------------------------------------------------
-- Audit & settings
-- ---------------------------------------------------------------------------
CREATE TABLE audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    entity_type   TEXT NOT NULL DEFAULT '',
    entity_id     TEXT NOT NULL DEFAULT '',
    detail        JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip            TEXT NOT NULL DEFAULT '',
    success       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_audit_logs_created ON audit_logs(created_at DESC);
CREATE INDEX ix_audit_logs_username ON audit_logs(username);
CREATE INDEX ix_audit_logs_action ON audit_logs(action);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Counter referensi (transaksional, aman untuk konkurensi)
-- ---------------------------------------------------------------------------
CREATE TABLE ref_counters (
    prefix     TEXT NOT NULL,
    year       INT NOT NULL,
    last_value BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (prefix, year)
);

-- +goose Down
DROP TABLE IF EXISTS ref_counters;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS notification_outbox;
DROP TABLE IF EXISTS watchers;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS work_item_events;
DROP TABLE IF EXISTS incident_links;
DROP TABLE IF EXISTS ticket_categories;
DROP TABLE IF EXISTS ticket_queue_members;
DROP TABLE IF EXISTS ticket_queues;
DROP TABLE IF EXISTS ticket_details;
DROP TABLE IF EXISTS rfs_details;
DROP TABLE IF EXISTS reminder_details;
DROP TABLE IF EXISTS task_details;
DROP TABLE IF EXISTS work_items;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS target_bindings;
DROP TABLE IF EXISTS notification_targets;
DROP TABLE IF EXISTS escalation_policies;
DROP TABLE IF EXISTS sla_targets;
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS business_calendars;
DROP TABLE IF EXISTS notification_providers;
DROP TABLE IF EXISTS workflow_definitions;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS organizations;
