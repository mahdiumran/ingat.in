-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    code TEXT NOT NULL UNIQUE,
    contacts JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin','agent','noc','sales','viewer','customer')),
    team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    telegram_chat_id TEXT NOT NULL DEFAULT '',
    wa_number TEXT NOT NULL DEFAULT '',
    token_version INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    rotated_from UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_type TEXT NOT NULL,
    name TEXT NOT NULL,
    states_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    transitions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    initial_state TEXT NOT NULL,
    terminal_states_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (item_type, name)
);

CREATE TABLE ref_counters (
    id BIGSERIAL PRIMARY KEY,
    prefix TEXT NOT NULL,
    year INTEGER NOT NULL,
    last_value BIGINT NOT NULL DEFAULT 0,
    UNIQUE (prefix, year)
);

CREATE TABLE escalation_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    offsets_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    applies_to_item_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    quiet_hours_from TEXT,
    quiet_hours_to TEXT,
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notification_targets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL DEFAULT 'group',
    notes TEXT NOT NULL DEFAULT '',
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notification_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL,
    kind TEXT NOT NULL,
    label TEXT NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    api_key_enc TEXT NOT NULL DEFAULT '',
    has_api_key BOOLEAN NOT NULL DEFAULT FALSE,
    extra_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_test_at TIMESTAMPTZ,
    last_test_ok BOOLEAN,
    last_test_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notification_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL,
    item_type TEXT,
    channel TEXT,
    subject_tpl TEXT NOT NULL DEFAULT '',
    body_tpl TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical')),
    description TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE target_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id UUID NOT NULL REFERENCES notification_targets(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    destination TEXT NOT NULL,
    provider_id UUID REFERENCES notification_providers(id) ON DELETE SET NULL,
    label TEXT NOT NULL DEFAULT '',
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (target_id, channel, destination)
);

CREATE TABLE sla_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    warning_pct INTEGER NOT NULL DEFAULT 80 CHECK (warning_pct BETWEEN 1 AND 100),
    use_business_hours BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE work_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_no TEXT NOT NULL UNIQUE,
    item_type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','critical')),
    status TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT '',
    owner_username TEXT NOT NULL DEFAULT '',
    requester_username TEXT NOT NULL DEFAULT '',
    team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    target_id UUID REFERENCES notification_targets(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    parent_id UUID REFERENCES work_items(id) ON DELETE SET NULL,
    start_at TIMESTAMPTZ,
    due_at TIMESTAMPTZ,
    expire_at TIMESTAMPTZ,
    sla_policy_id UUID REFERENCES sla_policies(id) ON DELETE SET NULL,
    sla_state TEXT NOT NULL DEFAULT 'none' CHECK (sla_state IN ('none','on_track','at_risk','breached','paused')),
    sla_breached_at TIMESTAMPTZ,
    first_response_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    paused_total_seconds BIGINT NOT NULL DEFAULT 0 CHECK (paused_total_seconds >= 0),
    device_ref TEXT NOT NULL DEFAULT '',
    service_ref TEXT NOT NULL DEFAULT '',
    customer_ref TEXT NOT NULL DEFAULT '',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_work_items_item_type CHECK (item_type IN ('task','reminder','rfs','incident','request','change')),
    CHECK (jsonb_typeof(tags) = 'array')
);

CREATE TABLE work_item_events (
    id BIGSERIAL PRIMARY KEY,
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN (
        'created','updated','status_changed','stage_changed','assigned','unassigned',
        'escalated','reopened','commented','attachment_added','due_changed','expire_changed',
        'sla_warning','sla_breached','sla_paused','sla_resumed','resolved','closed',
        'cancelled','notified','notify_failed'
    )),
    actor_username TEXT NOT NULL DEFAULT '',
    from_value TEXT NOT NULL DEFAULT '',
    to_value TEXT NOT NULL DEFAULT '',
    detail_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    author_username TEXT NOT NULL,
    body TEXT NOT NULL,
    is_internal BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    stored_path TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    mime TEXT NOT NULL DEFAULT 'application/octet-stream',
    uploaded_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE watchers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    target_id UUID REFERENCES notification_targets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((user_id IS NOT NULL)::integer + (target_id IS NOT NULL)::integer = 1)
);
CREATE UNIQUE INDEX ux_watchers_work_item_user ON watchers(work_item_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX ux_watchers_work_item_target ON watchers(work_item_id, target_id) WHERE target_id IS NOT NULL;

CREATE TABLE task_details (
    work_item_id UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    checklist_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    progress_pct INTEGER NOT NULL DEFAULT 0 CHECK (progress_pct BETWEEN 0 AND 100),
    estimate_minutes INTEGER CHECK (estimate_minutes IS NULL OR estimate_minutes >= 0),
    CHECK (jsonb_typeof(checklist_json) = 'array')
);

CREATE TABLE reminder_details (
    work_item_id UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    category TEXT NOT NULL DEFAULT 'generic' CHECK (category IN ('trial','rfs','maintenance','generic','custom')),
    subject_name TEXT NOT NULL DEFAULT '',
    subject_type TEXT NOT NULL DEFAULT 'none' CHECK (subject_type IN ('customer','service','device','none')),
    recurrence_rule TEXT,
    escalation_policy_id UUID REFERENCES escalation_policies(id) ON DELETE SET NULL,
    activated_at TIMESTAMPTZ,
    last_offset_fired TEXT NOT NULL DEFAULT ''
);

CREATE TABLE rfs_details (
    work_item_id UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    customer_name TEXT NOT NULL DEFAULT '',
    service_id TEXT NOT NULL DEFAULT '',
    service_package TEXT NOT NULL DEFAULT '',
    bandwidth TEXT NOT NULL DEFAULT '',
    pic_noc TEXT NOT NULL DEFAULT '',
    pic_sales TEXT NOT NULL DEFAULT '',
    sales_username TEXT NOT NULL DEFAULT '',
    site TEXT NOT NULL DEFAULT '',
    install_stage TEXT NOT NULL DEFAULT 'planned' CHECK (install_stage IN ('planned','provisioning','delivered','activated','cancelled','postponed'))
);

CREATE TABLE ticket_details (
    work_item_id UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
    category TEXT NOT NULL DEFAULT '',
    subcategory TEXT NOT NULL DEFAULT '',
    incident_type TEXT NOT NULL DEFAULT '',
    impact TEXT NOT NULL DEFAULT 'medium' CHECK (impact IN ('low','medium','high')),
    urgency TEXT NOT NULL DEFAULT 'medium' CHECK (urgency IN ('low','medium','high')),
    assignment_group TEXT NOT NULL DEFAULT '',
    escalation_level INTEGER NOT NULL DEFAULT 1 CHECK (escalation_level >= 0),
    reopen_count INTEGER NOT NULL DEFAULT 0 CHECK (reopen_count >= 0)
);

CREATE TABLE ticket_queues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ticket_queue_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id UUID NOT NULL REFERENCES ticket_queues(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (queue_id, user_id)
);

CREATE TABLE ticket_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    code TEXT NOT NULL UNIQUE,
    parent_id UUID REFERENCES ticket_categories(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE incident_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    linked_work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    link_type TEXT NOT NULL DEFAULT 'related',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (incident_id, linked_work_item_id, link_type),
    CHECK (incident_id <> linked_work_item_id)
);

CREATE TABLE business_calendars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    timezone TEXT NOT NULL DEFAULT 'Asia/Jakarta',
    schedule_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    holidays_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sla_targets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES sla_policies(id) ON DELETE CASCADE,
    priority TEXT NOT NULL CHECK (priority IN ('low','normal','high','critical')),
    response_minutes INTEGER NOT NULL CHECK (response_minutes > 0),
    resolution_minutes INTEGER NOT NULL CHECK (resolution_minutes > 0),
    warning_pct INTEGER NOT NULL DEFAULT 80 CHECK (warning_pct BETWEEN 1 AND 100),
    use_business_hours BOOLEAN NOT NULL DEFAULT FALSE,
    calendar_id UUID REFERENCES business_calendars(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, priority)
);

CREATE TABLE notification_outbox (
    id BIGSERIAL PRIMARY KEY,
    work_item_id UUID REFERENCES work_items(id) ON DELETE SET NULL,
    event_key TEXT NOT NULL UNIQUE,
    source_type TEXT NOT NULL DEFAULT '',
    template_key TEXT NOT NULL DEFAULT '',
    offset_label TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info','warning','critical')),
    channel TEXT NOT NULL,
    destination TEXT NOT NULL,
    provider_id UUID REFERENCES notification_providers(id) ON DELETE SET NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    rendered_subject TEXT NOT NULL DEFAULT '',
    rendered_body TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','suppressed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip TEXT NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by TEXT NOT NULL DEFAULT 'system',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ix_users_username ON users(username);
CREATE INDEX ix_users_email ON users(email);
CREATE INDEX ix_refresh_tokens_hash ON refresh_tokens(token_hash);
CREATE INDEX ix_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX ix_work_items_ref_no ON work_items(ref_no);
CREATE INDEX ix_work_items_type_status ON work_items(item_type, status);
CREATE INDEX ix_work_items_due_at ON work_items(due_at);
CREATE INDEX ix_work_items_expire_at ON work_items(expire_at);
CREATE INDEX ix_work_items_parent ON work_items(parent_id);
CREATE INDEX ix_work_items_active_lookup ON work_items(item_type, status, created_at DESC) WHERE NOT is_deleted;
CREATE INDEX ix_work_item_events_item_time ON work_item_events(work_item_id, created_at DESC);
CREATE INDEX ix_comments_item_time ON comments(work_item_id, created_at);
CREATE INDEX ix_target_bindings_target ON target_bindings(target_id);
CREATE INDEX ix_target_bindings_active ON target_bindings(target_id, channel) WHERE is_active;
CREATE INDEX ix_notification_providers_channel ON notification_providers(channel, is_default DESC) WHERE is_active;
CREATE INDEX ix_notification_outbox_pending ON notification_outbox(status, next_attempt_at);
CREATE INDEX ix_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX ix_settings_key ON settings(key);

-- +goose Down
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS notification_outbox;
DROP TABLE IF EXISTS sla_targets;
DROP TABLE IF EXISTS business_calendars;
DROP TABLE IF EXISTS incident_links;
DROP TABLE IF EXISTS ticket_categories;
DROP TABLE IF EXISTS ticket_queue_members;
DROP TABLE IF EXISTS ticket_queues;
DROP TABLE IF EXISTS ticket_details;
DROP TABLE IF EXISTS rfs_details;
DROP TABLE IF EXISTS reminder_details;
DROP TABLE IF EXISTS task_details;
DROP TABLE IF EXISTS watchers;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS work_item_events;
DROP TABLE IF EXISTS work_items;
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS target_bindings;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS notification_providers;
DROP TABLE IF EXISTS notification_targets;
DROP TABLE IF EXISTS escalation_policies;
DROP TABLE IF EXISTS ref_counters;
DROP TABLE IF EXISTS workflow_definitions;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS organizations;
DROP EXTENSION IF EXISTS pgcrypto;
