# Ingat.in — Master Plan

> **Sumber tunggal kebenaran** untuk pembangunan `Ingat.in`.
> Dokumen ini menggabungkan rencana final v4 (terkunci) + konvensi kerja Level A
> (wajib libatkan agent/subagent) + fase F1–F15 + risiko & mitigasi.
>
> Terakhir diperbarui: F1 — Infrastruktur, skema database, dan dokumentasi.

---

## 1. Identitas Produk

| Item | Nilai |
|---|---|
| Nama | **Ingat.in** |
| Fungsi awal | Hub reminder & notifikasi self-hosted untuk NOC/ISP |
| Fungsi jangka panjang | **Ticketing internal NOC + SLA** di atas model data yang sama |
| Repo | `/root/opencode/remindersys` |
| Bahasa backend | **Go 1.25** (chi + pgx/v5 + goose) |
| Frontend | React + TypeScript + Vite + **Tailwind CSS** (preset m2c) + nginx |
| Database | PostgreSQL (reuse host di server ini; varian portabel untuk server baru) |
| Kanál notifikasi | Telegram Bot API + WhatsApp (WAHA utama; Fonnte/Wablas/Starsender/custom HTTP via panel) |
| Timezone | Simpan UTC, tampilkan **WIB (Asia/Jakarta)** |

---

## 2. Keputusan Terkunci

| # | Poin | Keputusan | Alasan |
|---|---|---|---|
| 1 | Ikon | **Material Symbols** | Identik dengan `m2c` |
| 2 | Theme | **Light-first**; dark ditunda ke F8 via `[data-theme]` | `m2c` praktis light-only (0 pemakaian `dark:`) |
| 3 | Styling | **Tailwind CSS** + preset dari `m2c/tailwind.config.js` | Konsistensi token |
| 4 | Ticket | **Skema DB dibuat di F1**; route & UI ditunda | Hindari migrasi berat nanti |
| 5 | Router | **chi** | Konsisten dengan `mcnvpn` |
| 6 | Auth | JWT access **72 jam** + refresh token rotating 30 hari (hash di DB, revokable) | Sesi NOC panjang, tetap bisa dicabut |
| 7 | Deploy | **1 image · 1 `docker-compose.yml`**; blok `postgres` di-comment | Sederhana; portabel saat pindah server |
| 8 | Arsitektur | `work_items` generik + extension 1:1 + `work_item_events` append-only | Ticket/SLA tidak butuh refactor |
| 9 | Bahasa | **Go** (bukan Python) | Prioritas stabilitas longterm, bukan kecepatan rilis |
| 10 | Verifikasi | **Level A — ketat** | Lihat §4 |

**Catatan keputusan #6 (JWT 72 jam).** Token panjang dibatasi risikonya oleh:
refresh token **rotating** yang disimpan sebagai SHA-256 hash di tabel `refresh_tokens`
(bisa dicabut kapan saja), kolom `users.token_version` (dinaikkan saat ganti password /
revoke semua sesi), dan setting opsional `session_idle_timeout_minutes` (default `0` = nonaktif).
Konfigurasi: `INGATIN_ACCESS_TOKEN_MINUTES=4320`, `INGATIN_REFRESH_TOKEN_DAYS=30`.

---

## 3. Adopsi Pola dari `mcnvpn` (terverifikasi)

Semua klaim di bawah berasal dari pembacaan file nyata di `/root/opencode/mcnvpn`
(lihat laporan verifikasi F1). Yang diadopsi:

| Komponen | Sumber `mcnvpn` | Yang diadopsi |
|---|---|---|
| Enkripsi secret | `internal/crypto/crypto.go` | AES-256-GCM, nonce di-prepend, base64 StdEncoding, `Encrypt(key, plaintext)` / `Decrypt(key, encoded)`; empty input = no-op sukses; key 32 byte divalidasi di config |
| Koneksi & migrasi DB | `internal/db/db.go` | `pgxpool.New(ctx,url)` + `Ping`; goose v3 dengan `//go:embed migrations/*.sql`; `goose.OpenDBWithDriver("pgx", url)` + blank import `_ "github.com/jackc/pgx/v5/stdlib"`; hanya `Up` yang dijalankan program |
| Repository | `internal/repository/store.go` | `Store{pool *pgxpool.Pool}`, `New(pool)`, `Pool()`, `Tx(ctx, fn)`, `Now()` UTC; raw SQL dengan `$1..$n`; `errors.Is(err, pgx.ErrNoRows) → ErrNotFound`; helper `scanXxx(rows pgx.Rows)` |
| Scheduler | `internal/worker/worker.go` | `robfig/cron/v3` (`cron.New()` 5-field), `Start()`/`Stop()` dengan `<-ctx.Done()`, tiap job punya `context.WithTimeout` sendiri, dependency injection via constructor |
| Backup | `internal/backup/backup.go` | `pg_dump --no-owner --no-acl <url-tanpa-password>` + `PGPASSWORD` env, pipe ke `gzip -9`, sha256 streaming, retensi via `Prune`, opsional FTP |
| Entrypoint | `cmd/server/main.go` | Urutan: config → signal ctx → **migrate → connect** → store → services → worker.Start → api → `http.Server` → graceful shutdown 10s |
| Dockerfile | `backend/Dockerfile` | 2 tahap (`golang:1.25-alpine` → `alpine:3.20`), `CGO_ENABLED=0`, runtime `apk add postgresql-client gzip ca-certificates tzdata`, non-root user |
| Compose | `docker-compose.yml` | Tanpa `version:`, `restart: unless-stopped`, `depends_on` + `condition`, healthcheck `CMD-SHELL`, `env_file` |
| Migrasi | `internal/db/migrations/000NN_*.sql` | Anotasi `-- +goose Up` / `-- +goose Down`, `CREATE EXTENSION IF NOT EXISTS pgcrypto`, `UUID PRIMARY KEY DEFAULT gen_random_uuid()`, `TIMESTAMPTZ NOT NULL DEFAULT now()` |
| API | `internal/api/server.go` | `Server` struct + method handler, middleware `RequestID → Recoverer → Logger → securityHeaders → cors`, grup `chi.Router` + `Use(...)`, helper `writeJSON`/`writeErr`/`writeInternalError` (bentuk `{"error":"..."}`), `chi.URLParam` |

**Yang TIDAK diadopsi:** `routeros/`, `provisioning/`, `billing/`, payment gateway, avatar upload.

**Catatan penting:** `mcnvpn` **tidak** menyentuh `pg_hba.conf` sama sekali. Ingat.in juga tidak akan mengubahnya.

---

## 4. Konvensi Kerja: Level A (Verifikasi Ketat)

### 4.1 Aturan Wajib

1. **Kriteria verifikasi dijalankan nyata.** Output `go build`, `go test`, `curl`, `psql`,
   `docker compose ps` dicatat sebagai bukti. Kalimat "seharusnya jalan" tidak diterima.
2. **Penulis kode tidak memverifikasi sendiri.** Minimal satu subagent lain
   (`reviewer` atau `tester`) memverifikasi setiap fase.
3. **Klaim tentang file/pola existing wajib berbasis pembacaan file nyata**
   (`explore`, `codebase-memory`, `read`, `grep`), bukan asumsi.
4. **Fase belum selesai bila verifikator belum melaporkan lulus.** Jika gagal →
   perbaiki dulu, jangan lanjut ke fase berikutnya.
5. **Hasil tiap fase disimpan ke agentmemory** (`memory_save`: pattern/architecture/bug)
   agar sesi berikutnya tidak mengulang riset.
6. **Tidak ada perubahan destruktif** pada DB/aset existing tanpa konfirmasi user.

### 4.2 Pembagian Agent per Fase

| Fase | Eksekutor | Verifikator (wajib) | Fokus verifikasi |
|---|---|---|---|
| **F1** infra + skema + docs | orchestrator + `docwriter` | `explore` (pola `mcnvpn`) + `reviewer` (skema/migrasi) | build sukses, migrasi jalan, seed lengkap, docs akurat |
| F2 auth + RBAC | `be-developer` | `reviewer` + `tester` | JWT 72 jam, refresh revokable, matriks role 403 |
| F3 provider + WAHA | `be-developer` | `reviewer` (SSRF `custom_http`, enkripsi key) + `tester` (retry/backoff) | test-connection, QR, status sesi |
| F4 targets + template | `be-developer` | `tester` | binding TG+WA, quiet hours |
| F5 work_items generik | `be-developer` | `tester` + `codebase-memory` | idempotency `event_key`, timeline |
| F6 reminder + escalation | `be-developer` | `tester` (restart worker, no duplikat) + `reviewer` | offset H-n & LATE benar |
| F7 RFS | `be-developer` | `tester` | policy RFS, Tandai Aktif |
| F8 frontend | `fe-developer` | `reviewer` (kepatuhan token m2c) + `explore` | visual & responsive |
| F9 ops hardening | orchestrator + `docwriter` | `reviewer` | backup/restore terverifikasi |
| F10 ticketing (nanti) | `fullstack` | `explore` + `reviewer` + `tester` | UI + queue + assignment |
| F11 SLA (nanti) | `fullstack` | `tester` + `reviewer` | clock, pause, breach |

### 4.3 Format Laporan Penutup Fase

Setiap fase ditutup dengan tabel: **Apa → Perintah verifikasi → Hasil nyata → Lulus/Gagal**,
plus daftar temuan reviewer dan tindak lanjutnya.

---

## 5. Arsitektur

```
   ┌──────────────────── Linux host (Debian 12) ────────────────────┐
   │  ingatin-web (nginx :8091, bridge)  ── /api → 127.0.0.1:8081   │
   │                                                                │
   │  ingatin -mode=api     :8081  host-net   chi + pgx             │
   │  ingatin -mode=worker         host-net   cron jobs             │
   │  ingatin -mode=migrate        run-once   goose up              │
   │  waha                  :8082  host-net   WAHA                  │
   │  nginx site :8010 (LAN-only)             QR dashboard WAHA     │
   │                                                                │
   │  PostgreSQL host (reuse) — DB: ingatin                          │
   └────────────────────────────────────────────────────────────────┘
        │                    │                        │
  Telegram Bot API    WAHA /api/sendText    Fonnte/Wablas/Starsender
```

**Satu binary, tiga mode:** `ingatin -mode=api|worker|migrate`.
`worker` dapat dipindah ke host lain tanpa mengubah kode.

### 5.1 Port Map (terverifikasi bebas di server ini)

| Port | Service | Bind | Keterangan |
|---|---|---|---|
| `8091` | Web UI (nginx) | `0.0.0.0` | Akses utama |
| `8081` | API | `127.0.0.1` | Via proxy web / diagnostik |
| `8082` | WAHA | `127.0.0.1` | Tidak terekspos ke LAN |
| `8010` | nginx QR WAHA | LAN/Tailscale | Dijadwalkan di F3 |

### 5.2 Worker Jobs

> **Status F1:** penjadwalan (cron) sudah berjalan dan tercatat di log, tetapi
> **badan setiap job masih placeholder** (`return nil`) — logika sesungguhnya
> diisi pada fase yang tertera. "F1 selesai" berarti infrastruktur siap,
> bukan berarti notifikasi sudah terkirim.

| Job | Interval | Fase | Status F1 |
|---|---|---|---|
| `fanout` — materialisasi offset jatuh tempo → outbox | 60 s | F5 | placeholder |
| `sender` — kirim outbox (batch 50, `FOR UPDATE SKIP LOCKED`) | 15 s | F5 | placeholder |
| `digest` — ringkasan harian ke NOC | 08:00 WIB | F6 | placeholder |
| `sla_tick` — clock/pause/breach | 60 s | F11 | placeholder |
| `retention` — pangkas outbox `sent` & audit > 90 hari | 03:00 WIB | F9 | placeholder |

### 5.3 Notification Engine

- **Idempotent:** semua pengiriman lewat `notification_outbox` dengan `event_key` UNIQUE
  → tidak ada duplikat walau worker restart.
- **Retry:** backoff `1, 2, 5, 15, 60` menit (5 percobaan) → `failed` + alert admin.
- **Dedup per channel:** target dengan binding Telegram **dan** WhatsApp menerima keduanya;
  gagal satu channel tidak membatalkan yang lain.
- **Quiet hours:** `info`/`warning` di luar jam kerja ditahan ke 08:00 berikutnya; `critical` selalu lolos.
- **Provider abstrak:** `providers/base.go` (interface) + satu file per implementasi +
  `registry.go` untuk resolusi provider default per channel.

---

## 6. Data Model

Skema lengkap dibuat di `00001_init.sql`. Ringkasan:

```
CORE
  users                 id, username, email, password_hash, role, team_id, organization_id,
                        telegram_chat_id, wa_number, token_version, is_active, ...
  teams                 id, name, description, is_active
  organizations         id, name, code, contacts, is_active
  refresh_tokens        id, user_id, token_hash, expires_at, revoked_at, user_agent, ip
  work_items            ← INTI (lihat §6.1)
  work_item_events      ← append-only: timeline + SLA + audit
  comments              id, work_item_id, author, body, is_internal
  attachments           id, work_item_id, filename, stored_path, size, mime
  watchers              id, work_item_id, user_id|target_id
  workflow_definitions  id, item_type, name, states_json, transitions_json, is_default

EXTENSION 1:1 (nullable)
  task_details          checklist, progress_pct, estimate_minutes
  reminder_details      category, subject_name, subject_type, recurrence_rule,
                        escalation_policy_id, last_offset_fired
  rfs_details           customer_name, service_id, service_package, bandwidth,
                        pic_noc, pic_sales, sales_username, site, install_stage
  ticket_details        category, subcategory, impact, urgency, assignment_group,
                        escalation_level, reopen_count          ← F1, tanpa UI

TICKETING SUPPORT (F1, tanpa UI)
  ticket_queues, ticket_queue_members, ticket_categories, incident_links

NOTIFIKASI
  notification_targets, target_bindings, notification_providers,
  escalation_policies, notification_templates, notification_outbox (event_key UNIQUE)

SLA (skema siap, mesin F11)
  sla_policies, sla_targets (metric, target_minutes, warning_pct, business_hours_only),
  business_calendars

AUDIT
  audit_logs, settings
```

### 6.1 Kolom Inti `work_items`

```
id, ref_no, item_type, title, description, priority, status, stage,
owner_username, requester_username, team_id, organization_id, target_id,
source, parent_id (self-FK),
start_at, due_at, expire_at, first_response_at, resolved_at, closed_at,
paused_total_seconds, sla_policy_id, sla_state, sla_breached_at,
device_ref, service_ref, customer_ref, tags, is_deleted,
created_by, created_at, updated_at
```

`item_type ∈ {task, reminder, rfs, incident, request, change}` (CHECK constraint).

**Kolom "masa depan" tersedia sejak `00001`** sehingga ticketing/SLA tidak
memerlukan `ALTER TABLE` berat. Perhatikan: sebagian kolom tersebut sengaja
`NOT NULL DEFAULT` pada nilai aman, bukan nullable, agar query filter tidak
menghadapi NULL tak terduga:

| Kolom | Bentuk | Alasan |
|---|---|---|
| `team_id`, `organization_id`, `target_id`, `parent_id`, `sla_policy_id` | nullable (FK) | relasi boleh belum ada |
| `start_at`, `due_at`, `expire_at`, `first_response_at`, `resolved_at`, `closed_at`, `sla_breached_at` | nullable (timestamp) | waktu belum terjadi |
| `paused_total_seconds` | `BIGINT NOT NULL DEFAULT 0` | akumulator; 0 lebih aman daripada NULL di aritmetika SLA |
| `sla_state` | `TEXT NOT NULL DEFAULT 'none'` | selalu punya nilai yang dapat difilter |

`item_type` dan `priority` memiliki CHECK constraint. `status` **tidak**
dibatasi CHECK secara sengaja: daftar status berasal dari
`workflow_definitions` per `item_type`, sehingga validasi dilakukan di lapisan
service (`internal/workitems`) — lihat §4 keputusan #8.

### 6.2 Reference Number

Format per tipe: `TSK-2026-0001`, `REM-2026-0001`, `RFS-2026-0001`,
`INC-2026-0001`, `REQ-2026-0001`, `CHG-2026-0001`.
Di-generate transaksional dengan lock pada tabel counter (aman untuk konkurensi).

### 6.3 Seed Data (F1)

- 1 admin (dari env saat bootstrap) + organisasi `Internal` + teams `NOC`, `Sales`
- Workflow definitions: `task`, `reminder`, `rfs`, `incident`, `request`, `change`
- Escalation policies: `TRIAL-3D` (H-2/H-1/H-0/LATE-1H), `RFS-DEFAULT` (H-7/H-3/H-1/H-0/LATE-4H)
- SLA policy placeholder: `SLA-NOC-DEFAULT` (first_response 15 mnt, resolution 240 mnt, warning 80%) — mesin belum aktif
- Notification target: `NOC-Team`
- Notification templates default
- Settings: `timezone=Asia/Jakarta`, jam kerja, quiet hours, retensi

---

## 7. Design System (dari `m2c`)

Sumber: `m2c/DESIGN.md` + `m2c/tailwind.config.js` + `m2c/index.html`.

| Token | Nilai |
|---|---|
| primary | `#006d36` |
| accent (brand/CTA) | `#4ADE80` |
| primary-container / fixed | `#4ADE80`, `#6dfe9c`, `#4de082` |
| accent soft | `#BBF7D0` |
| background / surface | `#FDFDFD` / `#f9f9ff` |
| surface container | `#f1f3ff` → `#e1e8fd` → `#dce2f7` |
| text primary / secondary | `#111827` / `#4B5563` |
| border / outline | `#E5E7EB` / `#6d7b6d` |
| error | `#ba1a1a` (+ container `#ffdad6`) |
| Font headline/display | **Inter** |
| Font body | **Space Grotesk** |
| Font label/code | **JetBrains Mono** |
| Radius | control/card `8px`, modal `12px`, pill `9999px` |
| Shadow | `shadow-sm` dominan; `shadow-md` untuk elevated |
| Spacing | base 8, gap 16, card 24 |
| Ikon | Material Symbols Outlined |

**Adaptasi untuk Ingat.in:**
- Shell: topbar fixed `bg-[#FDFDFD]/90 backdrop-blur-md border-b border-border shadow-sm`
  + sidebar `232px`/`72px` (desktop), bottom-sheet nav (mobile, pola menu m2c).
- `ref_no`, ID, IP, timestamp, countdown selalu **JetBrains Mono**.
- Kartu: `bg-white border border-border rounded-xl shadow-sm p-6`.
- Login: reuse animasi network canvas (adaptasi `m2c/js/canvas.js`), subtle.
- Token terpusat di `frontend/src/design/tokens.ts` + preset Tailwind; **tidak ada warna hard-coded** di komponen.

---

## 8. Fase Eksekusi

### F1 — Infrastruktur, Skema, Dokumentasi

| # | Langkah | Status |
|---|---|---|
| 1 | Skeleton Go: `go.mod`, `cmd/ingatin/main.go` (`-mode`) | ✅ |
| 2 | `internal/config` — env `INGATIN_*`, fail-fast | ✅ |
| 3 | `internal/crypto` — AES-256-GCM + unit test | ✅ |
| 4 | `internal/db` — pgx pool + goose embed + `00001_init.sql` | ✅ |
| 5 | `scripts/provision-db.sh` (idempoten) | ✅ |
| 6 | `internal/models` + `internal/repository/store.go` | ✅ |
| 7 | `internal/workitems` — numbering/events/workflows + unit test | ✅ |
| 8 | `internal/api` — chi router + `/api/health` + `/api/version` | ✅ |
| 9 | `00002_seed.sql` | ✅ |
| 10 | `Dockerfile` multi-stage | ✅ |
| 11 | `docker-compose.yml` (1 image, 5 service) | ✅ |
| 12 | Frontend scaffold (Vite + React + Tailwind preset m2c) | ✅ |
| 13 | `frontend/nginx/default.conf` — SPA + `/api` proxy | ✅ |
| 14 | `install.sh` + `VERSION` | ✅ |
| 15 | Docs (README/DEVELOPMENT/DEPLOYMENT/OPERATIONS/ROADMAP) | ✅ |
| 16 | `.env.example`, `uninstall.sh`, `scripts/{backup,restore,smoke}.sh` | ✅ |
| V | Verifikasi Level A (`explore` + `reviewer` + `codebase-memory` + jalankan nyata) | ✅ lihat §8.1 |

### 8.1 Hasil Verifikasi Level A — F1

**Eksekutor verifikasi:** `explore` (ekstraksi pola `mcnvpn`), `reviewer` (review
skema/migrasi adversarial), `codebase-memory` (verifikasi klaim terhadap source),
plus pengujian runtime langsung.

**Bukti runtime (dijalankan nyata):**

| Verifikasi | Perintah | Hasil |
|---|---|---|
| Build | `go build ./...` | lulus |
| Lint statis | `go vet ./...` dan `go vet -tags=integration ./...` | bersih |
| Format | `gofmt -l .` | tidak ada output |
| Unit test | `go test ./...` | lulus (crypto 11 test, workitems 8 test) |
| Integrasi | `go test -tags=integration ./...` (PostgreSQL nyata) | lulus 8 test |
| Migrasi dari nol | `docker compose up migrate` | skema versi **3** |
| Idempotensi seed | migrasi dijalankan 3× | jumlah baris tidak berubah |
| Container | `docker compose ps` | api healthy, web healthy, worker up, waha up |
| Health API | `curl /api/health` | `{"status":"ok","database":"ok"}` |
| Proxy web | `curl :8091/api/health` | 200 |
| Migrasi dari container | `docker run --network host ingatin:local -mode=migrate` | sukses (membuktikan desain host-network) |
| Backup | `./scripts/backup.sh` + `sha256sum -c` | arsip 31 tabel, checksum OK |
| Smoke test | `./scripts/smoke.sh` | **20 lulus, 0 gagal** (exit 0) |

**Temuan review dan perbaikannya:**

| ID | Dampak | Temuan | Tindakan |
|---|---|---|---|
| A1 | BLOCKER | Tidak ada jalur penulisan tabel extension — `CreateWorkItem` hanya menulis tabel inti, sehingga `GetWorkItem` selalu mengembalikan extension `nil` | **Diperbaiki:** dibuat `internal/repository/work_item_extensions.go` (7 fungsi) + test `TestExtensionWritePath` untuk task/reminder/rfs/ticket |
| A11 | MAJOR | `UNIQUE (key, channel)` tidak menduplikasi-baris `channel IS NULL` (PostgreSQL menganggap NULL berbeda), sehingga seed yang dijalankan ulang menambah duplikat | **Diperbaiki:** migrasi `00003` menambahkan indeks unik parsial `ux_notification_templates_key_no_channel` + seed kini selalu memakai channel eksplisit. Dibuktikan gagal-dulu-lalu-lulus (`TestSeedTemplateNullChannelIdempotent`) |
| A5 | MAJOR (laten) | `sla_state` tidak punya konstanta Go dan dapat ditulis nilai sembarang | **Diperbaiki:** konstanta `models.SLAState*` (dan `RFSStage*`, `Reminder*`, `SubjectType*`, `Level*`) ditambahkan |
| A10 | — | Kekhawatiran `[]string` ↔ JSONB | **Diverifikasi OK** lewat `TestTagsJSONBRoundTrip` (tidak perlu perbaikan) |
| A17 | MINOR | `priority`/`target_id` belum terindeks; pencarian `lower() LIKE '%…%'` tidak dapat memakai indeks B-tree | **Dicatat** untuk F5 (tambahkan indeks bila filter itu terbukti panas) |
| A18 | MINOR | Tabel anak/extension tidak punya `is_deleted` | **Kebijakan ditetapkan:** query anak wajib melalui parent yang sudah difilter `NOT is_deleted` (didokumentasikan di `DEVELOPMENT.md`) |
| A19 | NIT | `%`/`_` pada pencarian tidak di-escape | **Dicatat** untuk F5 |
| — | MINOR | PLAN menulis nama migrasi 4 digit (`0001`) sedangkan aktual 5 digit (`00001`) | **Diperbaiki** di dokumen ini |
| — | MINOR | PLAN menyatakan `paused_total_seconds` nullable padahal `NOT NULL DEFAULT 0` | **Diperbaiki** di §6.1 (beserta alasannya) |

**Catatan penting:** review menemukan bahwa `golang-jwt` **belum** menjadi
dependency. Ini benar dan disengaja — autentikasi baru dipasang pada F2, dan
`go mod tidy` hanya menyimpan dependency yang benar-benar diimpor. Library JWT
ditambahkan saat F2.

### F2 — Auth & RBAC ✅ SELESAI
JWT access 72 jam + refresh rotating 30 hari (hash di DB, revokable), deteksi
penggunaan ulang refresh token (mencabut SELURUH sesi + naikkan `token_version`),
users/teams/organisations CRUD, RBAC middleware (`requireRole`/`requireAdmin`),
audit trail (mencatat sukses & gagal), logout-all, ganti password.
**Bukti:** klaim JWT terverifikasi 72,0 jam; viewer→/api/users 403 & /api/teams 200;
reuse refresh token → 401 + seluruh sesi dicabut; admin terakhir dilindungi.
**Test:** 6 unit test auth.

### F3 — Provider Layer + WAHA ✅ SELESAI
Interface `providers.Provider` + Telegram (escaping HTML, klasifikasi retryable),
WAHA (sendText, sesi start/stop/logout, normalisasi nomor 08→62, chatId @c.us/@g.us),
Fonnte/Wablas/Starsender/custom HTTP (field & header dapat dikonfigurasi), registry,
enkripsi API key AES-256-GCM, panel Providers dengan Test Koneksi / Test Kirim / Sesi QR,
nginx site `:8010` (basic auth, LAN-only).
**Bukti:** pesan uji terkirim nyata ke mock; WAHA `/api/sessions` 200 via X-Api-Key;
QR site 401 tanpa auth, 200 dengan auth.
**Test:** 26 unit test provider (httptest).

### F4 — Targets, Bindings, Templates ✅ SELESAI
CRUD target (grup/personal) + binding per kanal + provider override, editor policy
eskalasi (offset H-n/LATE dengan preset Trial & RFS), daftar template, jam tenang,
verifikasi binding lewat Kirim Uji (menandai `verified_at`).
**Bukti:** binding Telegram dibuat, diuji, dan menerima pesan nyata; preseed
TRIAL-3D (H-2/H-1/H-0/LATE-1H) dan RFS-DEFAULT (H-7/H-3/H-1/H-0/LATE-4H) tersedia.

### F5 — Work Items Generik ✅ SELESAI
CRUD `work_items` (task/reminder/rfs/incident/request/change) + extension 1:1 +
state machine tervalidasi + `work_item_events` append-only + komentar + halaman
Todos & Work Item Detail + API Dashboard. Notifikasi pembuatan item masuk outbox.
**Bukti end-to-end:** todo dibuat → outbox → worker mengirim → pesan Telegram nyata
berisi ref_no, prioritas, owner, dan due (WIB).
**Test:** 11 unit test workitems + integrasi DB (numbering, extension, transisi).

### F6 — Reminder + Escalation ✅ SELESAI
Preset "Trial Dedicated — 3 Hari", fan-out offset (H-n & LATE) dengan penghitungan
waktu absolut, transisi status otomatis (active→expiring→expired), `last_offset_fired`
anti-duplikat, digest harian 08:00 WIB.
**Bukti end-to-end:** reminder expire 2 jam → fanout menghitung offset **H-1** →
terkirim dengan subjek, expire (WIB), dan sisa waktu; timeline mencatat
`created` + `status_changed`.
**Test:** 52 unit test notify (render, offset, backoff, sisa waktu, placeholder).

### F7 — RFS ✅ SELESAI
CRUD RFS (customer, paket, bandwidth, PIC NOC/Sales, site, tahap instalasi),
hitung mundur berwarna, aksi "Tandai Aktif", ringkasan 7 hari/terlewat.
**Bukti end-to-end:** RFS dibuat → status `planned→in_progress→activated` + tahap
`activated`; notifikasi RFS terkirim lengkap (customer, paket, bandwidth, PIC NOC,
Sales, site, device).
**Bug nyata ditemukan & diperbaiki:** template seed memakai `{{.PicNoc}}` sedangkan
struct memakai `PicNOC` → render gagal → outbox `failed`. Diperbaiki di migrasi
`00004` + ditambah 3 test regresi yang akan menangkap kelas bug ini.

### F8 — Frontend Lengkap ✅ SELESAI
Dashboard (kartu statistik + Perlu Perhatian + distribusi + status sistem),
Work Item Detail (timeline, komentar, panel detail per tipe), Audit Trail
(filter + paginasi), Users & Teams (CRUD + proteksi admin terakhir), Profil
(ganti password + sesi aktif), Providers, Targets, Policies, Todos, Reminders, RFS.
Semua halaman punya state loading/error/empty; waktu dirender WIB.
Catatan: dark mode `[data-theme]` ditunda (m2c light-first) — struktur token siap.

### F9 — Ops Hardening ✅ SELESAI
`backup.sh` (pg_dump + gzip + sha256 + rotasi) + cron host harian 02:00 WIB,
job retensi (outbox sent, audit, refresh token kedaluwarsa), recovery outbox
`sending` menggantung, healthcheck compose, nginx QR site `:8010` (basic auth),
`uninstall.sh` (dengan `--purge` & `--drop-db`), dokumentasi lengkap.
**Bukti:** backup 16K berisi 31 tabel + checksum OK; cron terpasang;
QR site 401/200 sesuai auth.

### F10 — Ticketing NOC *(jalur tumbuh — `PLAN-TICKETING.md` terpisah)*
UI Ticket, queue, assignment, alur NOC. **Skema & seed sudah ada dari F1.**

### F11 — SLA Engine *(jalur tumbuh)*
Clock, pause, breach, UI & laporan. **Skema sudah ada dari F1.**

### F12 — Master Data & Role Permissions ✅ SELESAI
Wadah generik data referensi untuk menopang ticketing (dan kebutuhan lain):
tabel `master_data` (kind/code/label/parent/meta), registri `master_data_kinds`,
dan matriks izin `role_permissions` yang dapat diedit.
**Tata letak panel (revisi):** hub Master Data menampilkan setiap kelompok sebagai
**KOTAK** pada grid; setiap kelompok dibuka pada **halaman tersendiri** (bukan tab
dalam satu tabel besar), termasuk **Role Permissions** sebagai halaman sendiri.
Kelompok sudah tersedia: **Customer**, Kategori Tiket (EWO), Tag Tiket, Produk,
Lokasi POP, Site, Kategori RFS, Kategori Reminder. Kelompok baru dapat dibuat
langsung (kind terbuka) tanpa migrasi skema.
- Endpoint: `GET/POST/PATCH/DELETE /api/master-data`, `POST /api/master-data/kinds`,
  `GET/POST /api/role-permissions`.
- Izin tulis dijaga middleware `requirePermission("masterdata.write")` yang
  membaca matriks; **admin selalu boleh** dan tidak dapat dicabut dari panel.
- `meta_json` di-*merge* saat PATCH sehingga perubahan sebagian tidak menghapus
  atribut lain.
- **Bukti:** 409 pada kode duplikat per kind; noc boleh tulis (201), viewer
  ditolak (403); admin-revoke ditolak (400).

### F12.1 — Master Data Customer ✅ SELESAI
Kelompok `customer` dengan atribut terstruktur pada `meta_json`:
**No. Pelanggan** (auto `CUST-<tahun>-<urut>` via `ref_counters`), **Nama**,
**Jenis** (Personal/Corporate), **PIC** (wajib hanya untuk Corporate),
**Telepon**, **Email**, **Alamat**, **Kapasitas**. Form & tabel khusus customer
(kolom PIC hanya tampil untuk corporate). Validasi server: jenis wajib
personal/corporate; corporate tanpa PIC ditolak (400).
- **Bukti:** kode auto `CUST-2026-0001` / `CUST-2026-0002`; corporate tanpa PIC
  → 400 pada create **dan** patch; PATCH sebagian mempertahankan atribut lain.

### F13 — Pusat Notifikasi & Aksi Operasional ✅ SELESAI
1. **Edit Reminder/Todo/RFS** — `PATCH /api/items/{id}` kini benar-benar
   menerapkan blok `reminder` (sebelumnya diterima tetapi **diabaikan**),
   menambah dukungan `start_at`, dan mencatat event `updated` termasuk
   perubahan extension. UI: tombol Edit pada Todos, Reminders, RFS.
2. **Kirim notifikasi manual** — `POST /api/items/{id}/notify` (opsional
   `reset_reminder_offset`) mengantrikan ulang melalui outbox sehingga
   memperoleh retry & audit; tombol "kirim" pada daftar dan detail.
3. **Lonceng notifikasi sidebar** — `GET /api/notifications` (feed dari
   `work_item_events`) + `POST /api/notifications/read` (posisi baca
   per-pengguna di `settings`). Menampilkan todo/reminder/RFS/tiket terbaru,
   menghitung "belum dibaca", dan membuka detail item saat diklik.

### F13.1 — Aksi Status, Hak Hapus/Sunting & Editor Notifikasi ✅ SELESAI
1. **Ubah status lewat dropdown (kolom Aksi)** untuk **Todo, Ticketing,
   Reminder, RFS** — komponen `StatusSelect`. Status **Todo** diganti diksi:
   `accepted → on_progress → expired → canceled → closed` (migrasi `00007`
   memetakan data lama + memperbarui `workflow_definitions`). Reminder/RFS/Tiket
   memakai state workflow masing-masing; transisi tidak sah ditolak `409`.
2. **Hapus hanya pembuat/owner atau admin** — `DELETE /api/items/{id}` menolak
   `403` bila bukan pembuat/owner/admin. Tombol Hapus di Todos & Reminders hanya
   tampil untuk yang berhak (juga di halaman detail).
3. **Deskripsi/detail hanya pembuat/owner atau admin** — `PATCH /api/items/{id}`
   menolak `403` bila mengubah `description`/extension dari bukan pemilik. Field
   operasional (prioritas, owner, tenggat, tag) tetap bebas. UI menyediakan
   penyuntingan deskripsi inline di halaman detail.
4. **Ticketing (F10) aktif** — halaman `Tickets` baru (incident/request/change)
   dengan filter, buat tiket, ubah status, dan detail.
5. **Kotak Master Data "Notifikasi Telegram & WA" (admin only)** — halaman
   `NotificationTemplatesPage` menyunting subjek/isi/severity template per kanal
   (Telegram/WhatsApp) lewat `PATCH /api/templates/{id}` (sudah `requireAdmin`).
- **Bukti:** `accepted→on_progress→closed` 200, `closed→on_progress` 409;
  non-owner PATCH deskripsi & DELETE → **403**, admin → 200; template non-admin
  403 / admin 200; smoke **30/30**.

### F15 — Ticketing lanjutan, warna tag & SLA ✅ SELESAI
1. **Warna tag** — Master Data kind `tag_color` memetakan kode tag → warna
   (merah, oren, kuning, hijau, biru, ungu, abu). `TagBadge`/`TagList` mewarnai
   tag di daftar & detail. Warna dapat dipilih di Master Data (kind `tag_color`).
2. **Kolom Owner dihapus** dari daftar; pengganti "Dibuat oleh" (creator) +
   requester ditampilkan di Detail (tab Ringkasan).
3. **Halaman detail 3 tab** — **Ringkasan | Aktivitas | Action**. Perubahan
   status (workflow) dipindahkan ke tab **Action**; Aktivitas memuat diskusi +
   timeline; Ringkasan memuat info + deskripsi + SLA.
4. **Kategori & subkategori tiket** dari Master Data dengan hierarki
   `parent_id` (kategori induk → subkategori anak); form tiket memakai dua
   dropdown bertingkat.
5. **Dampak & urgensi** jadi dropdown low/medium/high (validasi server 400 bila
   nilai lain).
6. **Tiket tanpa tenggat waktu** — SLA dihitung dari `first_response_at` (status
   pertama bukan `new`) sampai `closed_at`. Dikirim sebagai `sla_seconds`,
   `sla_running`, `sla_start_at`, `sla_end_at`; ditampilkan di kolom SLA daftar &
   kartu SLA di detail. Tone: hijau <4 jam, kuning 4–24 jam, merah >24 jam.
7. **Jenis gangguan** — Master Data kind `incident_type` (7 entri awal) +
   kolom `ticket_details.incident_type`; dipilih lewat dropdown pada form tiket.
8. **Notifikasi konfirmasi aksi** — komponen `ConfirmDialog` (modal) + toast
   untuk setiap aksi berisiko (hapus, tutup/cancel, tandai aktif). Spesifikasi di
   `DESIGN.md` §7.
- **Bukti:** migrasi `00008` (skema 8); buat tiket kategori Gangguan/Fiber +
  jenis Fiber Cut, dampak/urgensi dropdown; SLA berjalan (2 detik) lalu final
  saat closed; impact tidak sah → 400; smoke **34/34**.
- Dokumen desain baru: **`DESIGN.md`** (palet warna tag, status, pola konfirmasi).

### F16 — Cascade delete, force status & deskripsi di notifikasi ✅ SELESAI
1. **Hapus tiket = hapus anaknya** — `DELETE /api/items/{id}` kini soft-delete
   **rekursif** ke seluruh work item yang `parent_id`-nya menunjuk ke item itu
   (mis. todo yang dibuat menyertai tiket). Respons menyertakan `deleted:<n>`.
2. **Force Status (admin only)** — `POST /api/items/{id}/force-status` melompati
   aturan transisi (mis. canceled → active/closed/pending). **Alasan wajib**;
   dicatat pada `work_item_events` (`forced:true`) + audit `item.force_status`.
   Hanya route di grup admin (non-admin → 403). Kotak "Force Status" muncul di
   tab Action halaman detail untuk admin.
3. **Box "Rubah Status" hanya di tab Action** — tidak ada kotak status di luar
   tab Action (Ringkasan/Aktivitas).
4. **Reminder** — tombol "Trial Dedicated — 3 Hari" dihapus; tombol pembuatan
   diganti nama menjadi **"Add Reminder"** (preset trial ikut dihapus).
5. **Deskripsi dilampirkan di notifikasi** — template Telegram/WA untuk
   todo/tiket (`TODO_CREATED`), reminder (`REMINDER_OFFSET/_DUE_TODAY/_LATE`), dan
   RFS (`RFS_UPCOMING/_TODAY/_LATE`) kini memuat baris `Deskripsi : {{.Description}}`
   (migrasi `00009`; payload sudah membawa deskripsi).
- **Bukti:** hapus tiket + 2 todo anak → `{"deleted":3,"ok":true}` dan ketiganya
  `is_deleted=t`; force closed→in_progress 200 (transisi tak sah), tanpa alasan
  400, non-admin 403; rendered_body outbox memuat `Deskripsi : ...`; smoke **35/35**.

### F17 — Daily Task (checklist harian) ✅ SELESAI
Menu baru **Daily Task** bergaya todo list harian, terpisah dari Todo Tasks
(planned). Menumpang `work_items` dengan `item_type='daily_task'` sehingga
otomatis memperoleh notifikasi, komentar, timeline, audit, dan tags.
1. **Model** — `item_type='daily_task'` (migrasi `00010`, skema **10**): CHECK
   constraint `item_type` diperluas, `workflow_definitions` baru, template
   `DAILY_TASK_CREATED`, index `ix_work_items_daily(item_type,start_at)`.
   Prefiks referensi `DTK`. Workflow `pending → in_progress → done` (+`canceled`,
   `done ⇄ pending` untuk buka ulang). State machine ditambahkan di
   `workitems.DefaultWorkflows`.
2. **Tanggal & carry-over** — tanggal harian disimpan pada `start_at` (00:00 WIB,
   UTC) dan `due_at` (23:59 WIB). Endpoint `GET /api/items` menerima
   `?date=YYYY-MM-DD` (default hari ini WIB) + `?carry_over=true` yang juga
   menyertakan task dari tanggal sebelumnya yang statusnya **belum terminal**
   (belum selesai) — operator tak melewatkan pekerjaan yang tertinggal.
3. **UI Kanban 3 kolom** (`DailyTasks.tsx`): Belum Selesai | Sedang Dikerjakan |
   Selesai, **tambah task langsung per kolom** (inline quick-add), tombol cepat
   (Kerjakan / Selesai / Belum), dan badge **Terlambat** untuk carry-over.
   Navigasi hari (◀ ▶ / Hari ini) + progress bar. Form lengkap "Daily Task Baru"
   menyediakan owner/prioritas/tag. Task juga dapat dijadikan anak tiket
   (`parent_id`) seperti Todo.
4. **Notifikasi** — pembuatan memakai template `DAILY_TASK_CREATED`
   (telegram/WA) lengkap dengan Deskripsi; `createdTemplateFor` memetakan
   `daily_task`. Item `daily_task` juga diarahkan ke halaman Daily Task saat
   dibuka dari lonceng notifikasi.
- **Bukti:** `dtk-2026-0001` terbuat (`DTK-2026-0001`), list hari ini + carry-over
  benar (task kemarin yang masih pending ikut muncul, yang sudah `done` tidak),
  `date` salah → 400, transisi `pending→in_progress→done` 200 dan `done→in_progress`
  409, outbox `rendered_body` memuat `📋 DAILY TASK BARU`; `go test ./...`,
  `tsc`, `vite build` bersih; smoke **35/35**.

### F18 — Auto-sync Todo & Daily Task ke Google Spreadsheet ✅ SELESAI
Setiap **Todo Task** (`item_type=task`) dan **Daily Task** (`item_type=daily_task`)
yang dibuat NOC otomatis tercatat sebagai satu baris di Google Spreadsheet; status
diperbarui **in-place** (dicocokkan lewat kolom **Ref**). NOC tidak perlu mencatat
manual — website menjadi sumber tunggal.
1. **Model** — migrasi `00011` (skema **11**): `sheet_sync_config` (tunggal:
   `enabled`, `spreadsheet_id`, `sheet_name`, service account terenkripsi,
   `header_written`, `last_sync_at/error`) + `sheet_sync_queue` (pola
   `notification_outbox`: `event_key` UNIQUE = `sheet:<item_id>`, `op`
   append/update, backoff, `FOR UPDATE SKIP LOCKED`).
2. **Paket `internal/sheets`** — klien Google Sheets API v4 (service account
   JWT→OAuth2): `EnsureHeader`, `AppendRow`, `FindRow`, `UpdateRow`,
   `CreateSheetTab`, `TestConnection`; validasi nama sheet (≤100 char, tanpa
   `[ ] * ? / \ :`); pemetaan kolom A–L; `Queue` (Enqueue/Dispatch/RecoverStuck).
3. **Konfigurasi dari panel** — Spreadsheet ID, **nama sheet**, dan service
   account JSON dikelola dari **MANAJEMEN → Google Sheets** (admin). Kredensial
   disimpan terenkripsi AES-256-GCM (pola provider) dan tidak pernah
   dikembalikan utuh oleh API (hanya `client_email` + `service_account_set`).
   Tersedia tombol **Uji Koneksi** dan **Buat Sheet Tab**.
4. **Trigger best-effort** — `syncSheet` dipanggil pada create/status/update
   Todo Task **dan Daily Task** (via `isSheetSyncedType`); kegagalan enqueue tidak
   menggagalkan operasi utama (item sudah tersimpan). Penghapusan item menandai
   baris menjadi `Dihapus` (riwayat tetap utuh). **Hanya item baru** yang
   disinkronkan (tanpa backfill data lama).
5. **Worker** — `runSheetSync` (`@every INGATIN_SHEET_SYNC_INTERVAL_SECONDS`,
   default 30) + `runSheetRecovery` (`@every 5m`). Interval masuk `Marshal()`
   konfigurasi. Bila sinkronisasi nonaktif, antrean dibiarkan menunggu (tidak
   dihabiskan) sehingga aktif kembali setelah admin melengkapi konfigurasi.
6. **Kolom sheet** (15 kolom, A–O) — Ref | Tipe | Judul | Deskripsi | Prioritas |
   Status | Owner | Dibuat Oleh | Diperbarui Oleh | Device | Tags | Due (WIB) |
   Dibuat (WIB) | Diperbarui (WIB) | Keterangan.
- **Bukti:** `GET /api/sheet-sync` 401 tanpa token, 200 untuk admin; nama sheet
  `[Bad]` → 400; service account invalid → 400; `test` tanpa kredensial → 400;
  buat Todo + ubah status → **1 entri** antrean (`op=update`,
  `action=status_changed`, payload status terbaru) dan **reminder tidak masuk
  antrean**; Daily Task ikut tersinkron (`isSheetSyncedType`); item dihapus →
  baris ditandai `Dihapus`; saat nonaktif entri tetap `pending` tanpa error. Uji
  klien terhadap server tiruan Sheets: buat tab + header idempoten, append→find
  (baris 2)→update baris yang sama. `go test ./...` (termasuk paket `sheets`)
  hijau, `tsc`+`vite build` bersih, smoke **41/41**.

### F19 — Jejak pengubah terakhir & keterangan penyelesaian ✅ SELESAI
1. **`updated_by_username`** — kolom baru di `work_items` (migrasi `00012`, skema
   **12**). Diisi otomatis pada create/update/status-change (juga force-status)
   dari username operator. Ditampilkan sebagai **"Diperbarui oleh …"** pada kartu
   Daily Task dan pada tab Ringkasan detail item (beserta waktunya).
2. **Keterangan penyelesaian** — kolom `completion_note` di `task_details`.
   Saat menandai **Daily Task Selesai**, UI menampilkan dialog konfirmasi dengan
   kotak keterangan (opsional); isinya dikirim sebagai `note` pada
   `POST /api/items/{id}/status` dan disimpan. Tampil pada detail item.
   `task_details` kini juga dibuat untuk `daily_task` (sebelumnya hanya `task`),
   dan `loadExtensions` memuatnya untuk kedua tipe.
3. **Spreadsheet** — dua kolom baru: **Diperbarui Oleh** (I) dan **Keterangan**
   (O), total 15 kolom (A–O). Header **self-healing**: bila jumlah/urutan kolom
   berubah, header ditulis ulang otomatis.
- **Bukti:** daily task dibuat → `updated_by=admin`; ditandai selesai dengan
  keterangan → `completion_note` tersimpan & terlihat di detail; payload antrean
  memuat `updated_by` + `completion_note`; spreadsheet menampilkan
  `upd_by=admin, ket="Selesai dicek oleh NOC shift malam."`; migrasi v12;
  `go test ./...` + integration hijau; `tsc`/`vite build` bersih; smoke 41/41.

### F20 — SLA per siklus, Assign/Penangan, KPI & Grafik ✅ SELESAI
1. **Siklus SLA** (`ticket_sla_cycles`, migrasi `00014`): rincian per siklus —
   siklus 0 sejak tiket dibuat, siklus 1.. setiap reopen. `closed_at` tetap ada
   + tambahan `reopened_at`; riwayat tidak dihapus. Saat reopen, siklus baru
   dibuka & `reopen_count` naik.
2. **SLA policy per prioritas** (incident/request/change, target sama, kalender
   **24 jam**): critical 10/120, high 15/240, normal 30/480, low 60/960 menit.
   `sla_policy_id` tiket lama di-backfill.
3. **Mesin SLA** (`internal/sla` + `runSLATick`): evaluasi tiap siklus aktif
   (respons & penyelesaian vs target), set `sla_state` (`on_track`/`breached`) +
   `sla_breached_at`, kirim `SLA_BREACH` (idempoten per siklus).
4. **Assign & penangan** (`ticket_collaborators`): 1 owner + banyak "ikut
   menangani". `POST /api/items/{id}/assign`, `POST/DELETE .../collaborators`.
   Admin/owner bebas; NOC self-claim/lepas. Kredit KPI **setara**.
5. **KPI API** `GET /api/kpi/sla?from=&to=&type=&person=` → ringkasan (met%,
   avg/median/p90, breach), per prioritas, tren harian, per person
   (owner ∪ collaborator). Skor = `0.5·respons% + 0.5·penyelesaian%`.
   Ekspor `GET /api/kpi/sla/export.csv`.
6. **Halaman KPI** (`pages/Kpi.tsx`, Recharts): 6 grafik profesional (donut,
   gauge skor, bar per prioritas, area tren, bar peringkat person, bar bertumpuk
   beban kerja) + tabel per person + filter periode/preset.
- **Bukti:** siklus terbentuk saat create; close menutup siklus 0; force-unlock
  membuka siklus 1 + `reopen_count=1` + event `unlocked`; `sla_tick` menandai
  `breached` & mengirim `SLA_BREACH`; assign/collaborator/handling 200;
  `go test ./...` (sla, kpi, attachments) hijau; smoke **47/47**.

### F21 — Lampiran, Catatan Penanganan, Force Unlock, Open/Close ✅ SELESAI
1. **Lampiran pendukung** (maks **50 MB**): `internal/attachments` menyimpan ke
   `<DataDir>/attachments/<item>/…`, unduh ber-auth `GET /api/attachments/{id}`,
   daftar/unggah/hapus. Viewer dilarang; jenis MIME dibatasi (415), ukuran
   berlebih 413. Hanya di aplikasi (tidak ke spreadsheet).
2. **Catatan penanganan** — 3 field `ticket_details`: `issue_found`,
   `troubleshooting`, `action_solution` (baca semua; edit pembuat/owner/admin).
3. **Force Unlock (admin)** `POST /api/items/{id}/force-unlock` — buka tiket
   `closed`/`completed` tanpa aturan transisi, alasan wajib, membuka siklus SLA
   baru, event `unlocked` + `reopened`, audit.
4. **Tombol Open/Close** mengikuti workflow (nonaktif bila transisi tak sah).
5. **Notifikasi** — `TICKET_REOPENED` dikirim saat tiket dibuka kembali;
   template `SLA_WARNING`/`SLA_BREACH` diperkaya (migrasi `00015`).
6. **Daily Task** — tenggat default **23:59 WIB** dijamin backend.
- **Bukti:** unggah/unduh/hapus lampiran berfungsi, jenis terlarang 415;
  PATCH catatan penanganan tersimpan; force-unlock 200; smoke 47/47.

### F14+ — Inbound & Integrasi
IMAP/SMTP → ticket, webhook generik `POST /api/hooks/{token}`, Telegram command
(`/ticket`), provider kanal tambahan, portal customer, reporting.

---

## 9. Risiko & Mitigasi

| Risiko | Mitigasi |
|---|---|
| Refactor besar saat F10 (ticketing) | `work_items` + `work_item_events` + workflow generik sejak `00001`; F10 hanya UI + route |
| Skema besar tapi belum terpakai | Semua kolom nullable; UI tetap 3 menu terpisah; lebih murah dari `ALTER` produksi |
| PG host hanya listen `localhost` | api/worker/waha pakai **`network_mode: host`** → `127.0.0.1:5432`; **tanpa** mengubah `postgresql.conf`/`pg_hba.conf` (aman untuk `juniper_manage` & `mcnvpn` produksi) |
| `pg_hba 127.0.0.1/32 = trust` | Role `ingatin` tetap berpassword; service bind `127.0.0.1`; hardening opsional di `OPERATIONS.md` |
| JWT 72 jam dicuri | Refresh token hash + revokable; `token_version`; opsi idle timeout |
| Sesi WAHA logout | Status sesi di panel + alert Dashboard; outbox `failed` terlihat di Audit |
| Token/nomor WA belum tersedia | F1–F4 terverifikasi tanpa kredensial; QR + test siap; kirim nyata setelah scan |
| Duplikasi notifikasi | `event_key` UNIQUE + `FOR UPDATE SKIP LOCKED` |
| Design drift dari `m2c` | Token terpusat di `design/tokens.ts` + preset Tailwind |
| `pg_dump` tidak ada di runtime image | Dockerfile runtime `apk add postgresql-client gzip` (diadopsi dari `mcnvpn`) |
| RAM/swap server ketat | Reuse PG host (hemat ±150 MB); opsi `WHATSAPP_DEFAULT_ENGINE=GOWS` terdokumentasi |

---

## 10. Referensi Silang Dokumen

| Dokumen | Isi |
|---|---|
| `PLAN.md` | Dokumen ini — plan master + konvensi agent |
| `DEPLOYMENT.md` | Deploy, port, DB, migrasi ke server baru, nginx/TLS, troubleshooting |
| `OPERATIONS.md` | Runbook harian: health, outbox gagal, revokasi sesi, backup/restore, hardening |
| `DEVELOPMENT.md` | Setup dev, konvensi kode, cara tambah provider/template/workflow/migrasi |
| `ROADMAP.md` | F10–F15 dengan urutan & ketergantungan |
| `PLAN-TICKETING.md` *(nanti)* | Plan frontend ticketing detail (dibuat saat F10) |
