# Rekonstruksi Migration PostgreSQL Ingat.in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Memulihkan sepuluh migration PostgreSQL bertahap agar fresh database Ingat.in dapat dimigrasikan, dibuild, diuji, dan dipakai melalui API.

**Architecture:** Goose menjalankan file SQL berurutan dari `backend/internal/db/migrations/`, lalu `db.go` meng-embed seluruh file ke binary. `00001` membuat schema F1, `00002` membuat seed baseline, `00003`–`00010` menerapkan koreksi dan fitur schema historis. Scope hanya database PostgreSQL kosong.

**Tech Stack:** PostgreSQL 16+, Goose v3, pgx/v5, Go 1.25, Docker Compose, Bash smoke test.

---

## File Map

**Create:**
- `backend/internal/db/migrations/00001_init.sql`: schema F1 dan tabel runtime dasar.
- `backend/internal/db/migrations/00002_seed.sql`: seed baseline idempoten.
- `backend/internal/db/migrations/00003_add_template_null_index.sql`: indeks parsial template channel-null.
- `backend/internal/db/migrations/00004_fix_notification_templates.sql`: koreksi template F7.
- `backend/internal/db/migrations/00005_add_master_data.sql`: master data dan role permissions.
- `backend/internal/db/migrations/00006_add_customer_master_data.sql`: customer kind dan data customer.
- `backend/internal/db/migrations/00007_update_task_workflow.sql`: workflow Todo final.
- `backend/internal/db/migrations/00008_add_ticket_fields_and_master_data.sql`: ticket fields dan master data ticket.
- `backend/internal/db/migrations/00009_add_notification_descriptions.sql`: deskripsi pada body template.
- `backend/internal/db/migrations/00010_add_daily_tasks.sql`: daily task, workflow, template, indeks.
- `backend/internal/db/migrations/migrations_test.go`: kontrak file migration, versi, marker goose.

**Modify only if verification proves necessary:**
- `backend/internal/repository/*.go`: hanya untuk mismatch schema/query yang tidak dapat diselesaikan SQL.
- `scripts/smoke.sh`: hanya jika acceptance contract memang salah, bukan untuk menyembunyikan migration failure.
- `PLAN.md` atau `DEVELOPMENT.md`: hanya koreksi dokumentasi yang terbukti tidak cocok dengan schema final.

**Do not modify:** `backend/internal/db/db.go`, frontend, auth behavior, provider behavior, or unrelated feature code.

---

### Task 1: Add migration contract test

**Files:**
- Create: `backend/internal/db/migrations/migrations_test.go`

- [ ] **Step 1: Write the failing contract test**

Test package `migrations` must inspect the embedded directory using `os.DirFS(".")` and assert exactly these basenames in lexical order:

```text
00001_init.sql
00002_seed.sql
00003_add_template_null_index.sql
00004_fix_notification_templates.sql
00005_add_master_data.sql
00006_add_customer_master_data.sql
00007_update_task_workflow.sql
00008_add_ticket_fields_and_master_data.sql
00009_add_notification_descriptions.sql
00010_add_daily_tasks.sql
```

For every file, assert it contains both `-- +goose Up` and `-- +goose Down`. Assert each filename begins with its expected five-digit version. The test should fail now because the directory is absent.

- [ ] **Step 2: Run the contract test**

Run:

```bash
cd backend && go test ./internal/db/migrations -run TestMigrationFiles -v
```

Expected: FAIL because migration files do not yet exist.

- [ ] **Step 3: Create the directory and empty migration files with goose markers**

Create the ten files with these exact headers before adding SQL:

```sql
-- +goose Up

-- +goose Down
```

- [ ] **Step 4: Run the contract test again**

Run the same command. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrations
git commit -m "test: define migration file contract"
```

---

### Task 2: Implement `00001_init.sql` core schema

**Files:**
- Modify: `backend/internal/db/migrations/00001_init.sql`

- [ ] **Step 1: Add PostgreSQL extension and core tables**

Under `-- +goose Up`, add `CREATE EXTENSION IF NOT EXISTS pgcrypto;`, then create these tables in dependency order:

1. `organizations`: UUID id, name, unique code, `contacts JSONB NOT NULL DEFAULT '{}'`, active flag, timestamps.
2. `teams`: UUID id, unique name, description, active flag, timestamps.
3. `users`: UUID id, unique username, unique email, full name, password hash, role, nullable team/organization FKs, Telegram/WA fields, token version default 0, active flag, login timestamp, timestamps. Role CHECK includes `admin`, `agent`, `noc`, `sales`, `viewer`, `customer`.
4. `refresh_tokens`: UUID id, user FK cascade, unique token hash, user agent, IP, expiry, revocation, rotated-from self-reference, timestamp.
5. `workflow_definitions`: UUID id, unique `(item_type, name)`, states/transitions/terminal states JSONB, initial state, default/active flags, timestamps.
6. `ref_counters`: bigserial id, unique `(prefix, year)`, last value default 0.
7. `escalation_policies`: UUID id, unique name, offsets JSONB, applicable item types JSONB, quiet hours, max attempts default 5, default/active flags, timestamps.
8. `notification_targets`: UUID id, name, kind, notes, nullable organization FK, active flag, timestamps.
9. `notification_providers`: UUID id, channel/kind/label/base URL, encrypted API key, `has_api_key`, extra JSONB, default/active flags, test metadata, timestamps.
10. `notification_templates`: UUID id, key, nullable item type/channel, subject/body, severity, description, updated-by, active flag, timestamps. Add unique `(key, channel)` for non-null channels through a later explicit index strategy, not a global constraint that mishandles NULL.
11. `target_bindings`: UUID id, target FK cascade, channel, destination, nullable provider FK, label, primary/active flags, verification timestamp, timestamps.
12. `notification_outbox`: bigserial id, nullable work item/provider FKs, unique event key, source/template/offset/severity/channel/destination, payload JSONB, rendered subject/body, status, attempts/max attempts, next attempt, errors/provider message ID, sent timestamp, timestamps.

- [ ] **Step 2: Add work item tables**

Create `work_items` with exactly the columns consumed by `workItemColumns`: UUID id, unique ref number, item type, title, description, priority, status, stage, owner/requester, nullable team/organization/target, source, nullable parent, start/due/expire timestamps, nullable SLA policy, SLA state default `none`, SLA breach/first response/resolved/closed timestamps, paused seconds default 0, device/service/customer refs, tags JSONB default `[]`, soft-delete flag default false, creator, timestamps. Add FKs and self-parent relationship. Add item-type CHECK for `task`, `reminder`, `rfs`, `incident`, `request`, `change`; priority CHECK for values used by API. Do not globally CHECK status.

Create:
- `work_item_events`: bigserial id, work item FK cascade, event type, actor, from/to values, detail JSONB, created timestamp.
- `comments`: UUID id, work item FK cascade, author, body, internal flag, timestamps.
- `attachments`: UUID id, work item FK cascade, filename, stored path, size, MIME, uploader, timestamp.
- `watchers`: UUID id, work item FK cascade, nullable user/target FKs, unique work-item/user-target combination.

- [ ] **Step 3: Add one-to-one extensions**

Create tables with `work_item_id` as primary key and FK cascade:

- `task_details`: checklist JSONB default `[]`, progress default 0 with range CHECK, nullable estimate.
- `reminder_details`: category, subject name/type, nullable recurrence, nullable escalation policy FK, activation timestamp, last offset.
- `rfs_details`: customer, service ID/package, bandwidth, NOC/Sales PIC, sales username, site, install stage with CHECK for `planned`, `provisioning`, `delivered`, `activated`, `cancelled`, `postponed`.
- `ticket_details`: category, subcategory, incident type, impact/urgency with `low|medium|high` CHECK, assignment group, escalation/reopen counters.

- [ ] **Step 4: Add ticketing and SLA support tables**

Create `ticket_queues`, `ticket_queue_members`, `ticket_categories`, `incident_links`, `sla_policies`, `sla_targets`, and `business_calendars` with UUID primary keys, timestamps where used, and FKs to their parent entities. Include the columns queried by future ticket/SLA paths from `PLAN.md`: queue name/description/active, queue membership, category hierarchy, linked incidents, SLA policy metric targets, warning percentage, business-hours flag, and calendar JSON/configuration.

- [ ] **Step 5: Add audit and settings tables**

Create `audit_logs` with bigserial id, username, action, entity type/id, detail JSONB, IP, success, timestamp. Create `settings` with text primary key, value JSONB, updated-by, updated-at.

- [ ] **Step 6: Add indexes required by current queries**

Add indexes on user username/email, refresh token hash/user, work item ref/type/status/due/parent/non-deleted lookup, event work item/time, comments work item/time, target bindings target/active, providers channel/default, outbox status/next-at, audit timestamp, and settings key. Keep index names deterministic.

- [ ] **Step 7: Add development-only down migration**

Under `-- +goose Down`, drop tables in reverse dependency order, then drop the extension only if no other extension dependency exists. Do not use `CASCADE` on the whole public schema.

- [ ] **Step 8: Run static checks**

```bash
cd backend
gofmt -w internal/db/migrations/migrations_test.go
go test ./internal/db/migrations -run TestMigrationFiles -v
git diff --check
```

- [ ] **Step 9: Commit**

```bash
git add backend/internal/db/migrations/00001_init.sql
git commit -m "feat(db): reconstruct initial PostgreSQL schema"
```

---

### Task 3: Implement `00002_seed.sql`

**Files:**
- Modify: `backend/internal/db/migrations/00002_seed.sql`

- [ ] **Step 1: Seed stable baseline rows**

Use deterministic natural keys and `ON CONFLICT` for organizations, teams, workflow definitions, policies, target, providers-independent target bindings only when safe, and settings. Never insert an admin or secret. Use JSONB literals with explicit casts. Ensure at least one organization, three teams, six workflows, two policies, one target, and ten templates exist after all migrations.

Workflow baseline must define task, reminder, RFS, incident, request, and change. Task may use the pre-`00007` baseline states because `00007` owns the final Todo vocabulary.

- [ ] **Step 2: Seed exact escalation policy content**

`TRIAL-3D.offsets_json` must contain labels `H-2`, `H-1`, `H-0`, `LATE-1H`. `RFS-DEFAULT.offsets_json` must contain `H-7`, `H-3`, `H-1`, `H-0`, `LATE-4H`. Use stable JSON keys consumed by `notify` rendering.

- [ ] **Step 3: Seed notification templates**

Create explicit Telegram and WhatsApp rows, avoiding NULL channel for baseline templates. Include the keys used by `notify.createdTemplateFor`, reminder fanout, RFS fanout, ticket creation, and digest. Set valid severities and non-empty body templates.

- [ ] **Step 4: Seed settings**

Insert timezone `Asia/Jakarta`, working-hours settings, quiet-hours settings, outbox retention, audit retention, and other keys read by `repository/settings.go`. Re-running the migration must update no existing operator override unexpectedly, so use `ON CONFLICT DO NOTHING` for settings defaults.

- [ ] **Step 5: Validate SQL on a disposable PostgreSQL database**

```bash
# From repository root, with PostgreSQL available:
createdb ingatin_test_reconstruct 2>/dev/null || true
INGATIN_DB_URL='postgres://ingatin:<password>@127.0.0.1:5432/ingatin_test_reconstruct?sslmode=disable' \
  go run ./backend/cmd/ingatin -mode=migrate
```

Expected: goose reaches version 2 without SQL errors. The exact test database credential must come from the local environment, never be committed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/db/migrations/00002_seed.sql
git commit -m "feat(db): seed baseline Ingat.in data"
```

---

### Task 4: Implement template migrations `00003` and `00004`

**Files:**
- Modify: `backend/internal/db/migrations/00003_add_template_null_index.sql`
- Modify: `backend/internal/db/migrations/00004_fix_notification_templates.sql`

- [ ] **Step 1: Add the partial unique index**

Create exactly:

```sql
CREATE UNIQUE INDEX ux_notification_templates_key_no_channel
ON notification_templates (key)
WHERE channel IS NULL;
```

Create a separate unique index for `(key, channel)` where `channel IS NOT NULL`. Down migration drops both by name.

- [ ] **Step 2: Correct template rows idempotently**

Update or insert the RFS templates using the exact Go template field `{{.PicNOC}}`, not `{{.PicNoc}}`. Keep Telegram and WhatsApp rows explicit. Preserve user-edited templates when possible by targeting known seed keys and only correcting the known broken placeholder in fresh DB scope.

- [ ] **Step 3: Add the regression test assertions**

Extend `backend/internal/repository/integration_test.go` only if the existing test cannot prove both named indexes. The test must verify the partial index exists and that two NULL-channel inserts with `ON CONFLICT (key) WHERE channel IS NULL DO NOTHING` leave one row.

- [ ] **Step 4: Run migration and focused integration tests**

```bash
cd backend
go test ./internal/repository -run 'TestSeedTemplateNullChannel' -v
```

Expected: tests skip without `INGATIN_DB_URL`; with a disposable PostgreSQL URL, both tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrations/00003_add_template_null_index.sql backend/internal/db/migrations/00004_fix_notification_templates.sql
# Add integration_test.go only if it changed.
git commit -m "fix(db): make notification templates idempotent"
```

---

### Task 5: Implement master-data migrations `00005` and `00006`

**Files:**
- Modify: `backend/internal/db/migrations/00005_add_master_data.sql`
- Modify: `backend/internal/db/migrations/00006_add_customer_master_data.sql`

- [ ] **Step 1: Create master-data tables**

Create `master_data_kinds` with text primary key, label, description, icon, system flag, sort order. Create `master_data` with UUID id, kind/code uniqueness, label, description, nullable self-parent, sort order, `meta_json` JSONB, active flag, creator, timestamps. Create `role_permissions` with composite primary key `(role, action)`, allowed flag, updated-by, updated-at. Add indexes for kind/parent/active.

- [ ] **Step 2: Seed base kinds and entries**

Seed at least seven kinds and eight entries, including all kinds consumed by the panel. Use explicit natural-key conflict targets. Ensure `admin` has at least four allowed permissions and non-admin role rows required by the API exist.

- [ ] **Step 3: Add customer kind and counter-compatible data**

Insert `customer` into `master_data_kinds` as a system kind. Add initial customer entries with `meta_json` keys matching the customer API: type, PIC, phone, email, address, and capacity. Do not create hard-coded auto-generated customer codes that can collide with the application counter.

- [ ] **Step 4: Run master-data acceptance queries**

```bash
psql "$INGATIN_DB_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT count(*) >= 3 FROM information_schema.tables
 WHERE table_name IN ('master_data','master_data_kinds','role_permissions');
SELECT count(*) >= 7 FROM master_data_kinds;
SELECT count(*) >= 1 FROM master_data_kinds WHERE kind='customer';
SELECT count(*) >= 8 FROM master_data;
SELECT count(*) >= 4 FROM role_permissions WHERE role='admin' AND allowed;
SQL
```

Expected: every query returns `t`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrations/00005_add_master_data.sql backend/internal/db/migrations/00006_add_customer_master_data.sql
git commit -m "feat(db): add master data and customer seeds"
```

---

### Task 6: Implement task and ticket migrations `00007` and `00008`

**Files:**
- Modify: `backend/internal/db/migrations/00007_update_task_workflow.sql`
- Modify: `backend/internal/db/migrations/00008_add_ticket_fields_and_master_data.sql`

- [ ] **Step 1: Update the task workflow**

Upsert the task workflow with states containing `accepted`, `on_progress`, `expired`, `canceled`, and `closed`. Set initial state to `accepted`, transitions to the values implemented by `workitems.DefaultWorkflows`, and terminal states to `expired`, `canceled`, and `closed`.

- [ ] **Step 2: Seed ticket master data**

Insert at least five `incident_type` entries, seven `tag_color` entries, ticket categories, and at least three subcategories with non-null `parent_id`. Use deterministic codes and idempotent conflict handling.

- [ ] **Step 3: Ensure ticket incident field and constraints**

Add `ticket_details.incident_type` only if missing. Keep impact and urgency validation aligned with Go constants `low`, `medium`, `high`. Do not add a global status CHECK.

- [ ] **Step 4: Validate the workflow and ticket data**

```bash
psql "$INGATIN_DB_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT states_json::text FROM workflow_definitions WHERE item_type='task';
SELECT count(*) >= 5 FROM master_data WHERE kind='incident_type';
SELECT count(*) >= 7 FROM master_data WHERE kind='tag_color';
SELECT count(*) >= 3 FROM master_data WHERE kind='ticket_category' AND parent_id IS NOT NULL;
SELECT count(*) = 1 FROM information_schema.columns
 WHERE table_name='ticket_details' AND column_name='incident_type';
SQL
```

Expected: task JSON contains the five new states; all booleans return `t`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrations/00007_update_task_workflow.sql backend/internal/db/migrations/00008_add_ticket_fields_and_master_data.sql
git commit -m "feat(db): finalize task workflow and ticket metadata"
```

---

### Task 7: Implement notification and Daily Task migrations `00009` and `00010`

**Files:**
- Modify: `backend/internal/db/migrations/00009_add_notification_descriptions.sql`
- Modify: `backend/internal/db/migrations/00010_add_daily_tasks.sql`

- [ ] **Step 1: Update description-bearing templates**

Update fresh seed rows for `TODO_CREATED`, `REMINDER_OFFSET`, and `RFS_UPCOMING` in Telegram and WhatsApp so each body contains the literal `Deskripsi : {{.Description}}`. Preserve the rest of each template body. Use an idempotent update keyed by template key and channel.

- [ ] **Step 2: Add Daily Task support**

Update the `work_items.item_type` CHECK to include `daily_task`. Upsert a `daily_task` workflow with states `pending`, `in_progress`, `done`, and `canceled`, including reopen transition `done -> pending`. Insert Telegram and WhatsApp `DAILY_TASK_CREATED` templates containing the description line. Add index `ix_work_items_daily` on `(item_type, start_at)`.

- [ ] **Step 3: Validate final acceptance rows**

```bash
psql "$INGATIN_DB_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT count(*) >= 3 FROM notification_templates
 WHERE body_tpl LIKE '%Deskripsi :%' AND key IN ('TODO_CREATED','REMINDER_OFFSET','RFS_UPCOMING');
SELECT count(*) >= 1 FROM workflow_definitions WHERE item_type='daily_task';
SELECT count(*) = 1 FROM pg_indexes
 WHERE tablename='work_items' AND indexname='ix_work_items_daily';
SELECT count(*) >= 1 FROM notification_templates WHERE key='DAILY_TASK_CREATED';
SQL
```

Expected: all queries return `t`.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/db/migrations/00009_add_notification_descriptions.sql backend/internal/db/migrations/00010_add_daily_tasks.sql
git commit -m "feat(db): add notification descriptions and daily tasks"
```

---

### Task 8: Fresh-database migration and idempotency validation

**Files:**
- No source changes expected.

- [ ] **Step 1: Build the backend image**

```bash
docker compose build migrate
```

Expected: exit 0 and image `ingatin:local` built successfully.

- [ ] **Step 2: Reset only the disposable test database**

Use a database whose name contains `test` or `reconstruct`. Drop and recreate only that database with the local PostgreSQL administrator. Never run the reset command against a production database.

- [ ] **Step 3: Apply all migrations once**

```bash
INGATIN_DB_URL="$TEST_DB_URL" docker compose run --rm \
  -e INGATIN_DB_URL="$TEST_DB_URL" migrate -mode=migrate
```

Expected log: migration completes at version 10.

- [ ] **Step 4: Apply migration again**

Run the same command a second time. Expected: goose reports no pending migrations and exits 0.

- [ ] **Step 5: Assert schema and seed counts**

Run the complete database section of `scripts/smoke.sh` with API/web checks disabled only when services are not started. Confirm at least 30 public tables, all required seed counts, named indexes, no duplicate templates, final task workflow, ticket fields, descriptions, and Daily Task index.

- [ ] **Step 6: Run migration contract and backend tests**

```bash
cd backend
go test ./...
go vet ./...
```

Expected: all unit tests pass and vet emits no diagnostics.

---

### Task 9: PostgreSQL integration and API acceptance

**Files:**
- No source changes expected unless a concrete schema/query mismatch is observed.

- [ ] **Step 1: Run tagged integration tests**

```bash
cd backend
INGATIN_DB_URL="$TEST_DB_URL" go test -tags=integration ./internal/repository/... -v
```

Expected: numbering, extension writes, status transitions, outbox uniqueness, tags JSONB round-trip, and template partial-index tests pass.

- [ ] **Step 2: Start Compose stack against the disposable DB**

```bash
INGATIN_DB_URL="$TEST_DB_URL" docker compose up -d api worker web
```

Expected: API, worker, and web start without migration dependency failures.

- [ ] **Step 3: Test health, version, login, and proxy**

```bash
curl -fsS http://127.0.0.1:8081/api/health
curl -fsS http://127.0.0.1:8081/api/version
curl -fsS http://127.0.0.1:8091/
curl -fsS http://127.0.0.1:8091/api/health
```

Expected: database health is `ok`, schema version is 10, web root is HTTP 200, and web proxy returns API health.

- [ ] **Step 4: Run the repository smoke test**

```bash
API_BASE=http://127.0.0.1:8081 WEB_BASE=http://127.0.0.1:8091 ./scripts/smoke.sh
```

Expected: exit 0 with zero failures. WAHA checks may be skipped when no key is configured.

- [ ] **Step 5: Stop disposable services and preserve logs**

```bash
docker compose down
```

Record migration output, test output, and smoke summary in the final response. Do not commit generated dumps, credentials, or logs containing secrets.

---

### Task 10: Final review and commit synchronization

**Files:**
- Review all migration SQL and test changes.

- [ ] **Step 1: Scan for secrets and accidental artifacts**

```bash
git status --short
git diff --check
grep -RInE '(password|api[_-]?key|token|secret)\s*[:=]\s*["'"']' backend/internal/db/migrations || true
```

Expected: no credentials or generated artifacts.

- [ ] **Step 2: Review migration ordering and goose markers**

```bash
cd backend
go test ./internal/db/migrations -run TestMigrationFiles -v
for f in internal/db/migrations/*.sql; do
  grep -q '^-- +goose Up$' "$f"
  grep -q '^-- +goose Down$' "$f"
done
```

Expected: ten files, ten passing contract checks.

- [ ] **Step 3: Review the complete diff**

```bash
cd ..
git diff --stat origin/main..HEAD
git log --oneline --decorate -12
git status --short --branch
```

Expected: only migration reconstruction, migration contract test, and approved design/plan documents are changed; worktree is clean.

- [ ] **Step 4: Create a final integration commit if needed**

If the prior task commits are complete and clean, do not create an empty commit. If only an uncommitted test or documentation adjustment remains, commit it with:

```bash
git add backend/internal/db/migrations backend/internal/db/migrations/migrations_test.go docs/superpowers
 git commit -m "test: verify reconstructed database migrations"
```

- [ ] **Step 5: Final completion criteria**

Declare the migration work complete only when all are true: ten files exist, contract test passes, backend build passes, goose reaches version 10 on a fresh PostgreSQL database, second migration run is idempotent, tagged integration tests pass, API health/database health are OK, frontend proxy works, smoke test exits 0, and `git status` is clean.
