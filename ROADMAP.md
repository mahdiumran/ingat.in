# Ingat.in — Roadmap

Rencana pengembangan lanjutan setelah F1–F9 (reminder hub) selesai.
Skema database untuk seluruh fase di bawah **sudah dibuat sejak F1** di
`backend/internal/db/migrations/0001_init.sql` — sehingga fase lanjutan tidak
memerlukan perubahan skema besar.

---

## Ringkasan

| Fase | Nama | Ketergantungan | Estimasi relatif |
|---|---|---|---|
| F1 | Infrastruktur, skema, dokumentasi | — | ✅ selesai |
| F2 | Auth & RBAC | F1 | ✅ |
| F3 | Provider layer + WAHA | F2 | ✅ |
| F4 | Targets, bindings, templates | F3 | ✅ |
| F5 | Work items generik | F4 | ✅ |
| F6 | Reminder + escalation | F5 | ✅ |
| F7 | RFS | F6 | ✅ |
| F8 | Frontend lengkap | F5–F7 | ✅ |
| F9 | Ops hardening | F8 | ✅ |
| **F10** | **Ticketing NOC** | F9 | Besar |
| **F11** | **SLA engine** | F10 | Besar |
| F12 | Inbound & integrasi | F11 | Sedang |
| F13 | Kanal notifikasi tambahan | F9 | Kecil |
| F14 | Portal customer / multi-tenant | F12 | Sedang |
| F15 | Reporting & analytics | F11 | Sedang |

---

## F10 — Ticketing Internal NOC

**Tujuan:** Ingat.in menjadi sistem ticketing penuh untuk NOC.

**Sudah tersedia dari F1 (tidak perlu migrasi baru):**

| Objek | Status |
|---|---|
| `work_items.item_type ∈ {incident, request, change}` | ✅ CHECK constraint |
| `ticket_details` (category, subcategory, impact, urgency, assignment_group, escalation_level, reopen_count) | ✅ tabel |
| `ticket_queues`, `ticket_queue_members` | ✅ tabel |
| `ticket_categories` | ✅ tabel |
| `incident_links` (parent/child, link_type) | ✅ tabel |
| `work_items.parent_id` (self-FK) + `sla_policy_id` | ✅ kolom |
| `work_item_events` (`assigned`, `escalated`, `reopened`, `sla_warning`, `sla_breached`) | ✅ event type |
| `workflow_definitions` untuk `incident`/`request`/`change` | ✅ seed |
| Reference number `INC-`/`REQ-`/`CHG-` | ✅ `workitems/numbering.go` |
| Team/queue assignment (`teams`, `team_id`) | ✅ tabel + kolom |

**Pekerjaan yang tersisa (kode + UI saja):**

1. **Backend**
   - Repository `repository/tickets.go`: CRUD `ticket_details`, queue assignment.
   - Service `workitems/tickets.go`: create incident/request/change, auto-classify
     berdasarkan kategori, eskalasi level otomatis bila belum ditangani.
   - API `api/tickets.go`: endpoint list/detail/create/update/assign/comment/close/reopen.
   - Auto-assignment: round-robin ke member queue, atau berdasarkan team.
   - Aturan eskalasi NOC (mis. L1 → L2 setelah 30 menit tanpa respons).
2. **Frontend**
   - Halaman *Tickets*: tabel dengan filter (queue, status, priority, owner, umur tiket).
   - Halaman *Ticket Detail*: reuse `WorkItemDetail` + tab Ticket (impact/urgency, queue, assignment).
   - Papan *My Queue* / *Team Queue*.
   - Aksi massal (assign, ubah status, tutup).
3. **Notifikasi**
   - Template: `TICKET_CREATED`, `TICKET_ASSIGNED`, `TICKET_ESCALATED`, `TICKET_RESOLVED`, `TICKET_REOPENED`.
   - Target per queue (route notifikasi berdasarkan `ticket_queues`).

**Kriteria selesai:**
- Incident dapat dibuat, di-assign, dieskalasi, diselesaikan, dan ditutup lewat UI.
- Setiap perubahan status tercatat di `work_item_events`.
- Notifikasi terkirim ke target queue yang benar.
- `PLAN-TICKETING.md` ditulis terpisah dan menjadi rujukan UI detail.

---

## F11 — SLA Engine

**Tujuan:** mengukur dan menegakkan SLA pada work item (terutama tiket).

**Sudah tersedia dari F1:**

| Objek | Status |
|---|---|
| `sla_policies`, `sla_targets` (metric, target_minutes, warning_pct, business_hours_only) | ✅ tabel |
| `business_calendars` (workdays, jam kerja, hari libur) | ✅ tabel |
| `work_items`: `sla_policy_id`, `sla_state`, `sla_breached_at`, `first_response_at`, `resolved_at`, `paused_total_seconds` | ✅ kolom |
| `work_item_events` sebagai sumber perhitungan | ✅ append-only |
| Seed `SLA-NOC-DEFAULT` | ✅ (mesin belum aktif) |

**Pekerjaan:**

1. **Clock** (`sla/clock.go`)
   - Hitung sisa waktu dengan memasukkan kalender bisnis (jam kerja + hari libur).
   - Hitung `paused_total_seconds` dari rentang status `pending_customer` (waktu tunggu tidak dihitung).
   - Metric: `first_response`, `resolution`, `update`.
2. **Tick job** (`sla/tick.go`) — berjalan tiap 60 detik:
   - Evaluasi semua work item aktif terhadap `sla_targets`.
   - Set `sla_state ∈ {on_track, at_risk, breached}`.
   - Tembak event `SLA_WARNING` (pada `warning_pct`, mis. 80%) dan `SLA_BREACH` ke outbox.
3. **UI**
   - Kolom countdown SLA di tabel tiket (warna: hijau → amber → merah).
   - Tab *SLA* pada Work Item Detail: target, terpakai, sisa, waktu pause.
   - Dashboard: kartu SLA at-risk & breached.
4. **Laporan**
   - Agregat per queue/team/agent: % pencapaian, rata-rata waktu respons & penyelesaian.
   - Export CSV.

**Kriteria selesai:**
- SLA clock akurat terhadap kalender bisnis (diuji dengan unit test kasus batas:
  tiket dibuat Jumat malam, hari libur, pause panjang).
- Peringatan & breach menghasilkan notifikasi.
- Laporan SLA per periode dapat diekspor.

---

## F12 — Inbound & Integrasi

**Tujuan:** tiket bisa masuk dari luar panel, bukan hanya dibuat manual.

| Sumber | Pendekatan |
|---|---|
| **Email** | Worker `imap` membaca mailbox NOC, membuat tiket/komentar dari email masuk |
| **Webhook generik** | `POST /api/hooks/{token}` — untuk monitoring system (Zabbix, LibreNMS, PRTG) membuat tiket otomatis saat alarm |
| **Telegram command** | ✅ **Selesai** — bot command `/open`, `/ticket`, `/list`, `/solved`, `/hold`, `/rekap`, `/id`, `/help` dari chat NOC (webhook `POST /api/hooks/telegram/{secret}`) |
| **API publik** | Token per integrasi + rate limit |

### F12 — Telegram command bot (selesai)

- **Inbound** via webhook publik `POST /api/hooks/telegram/{secret}`; secret dari
  `INGATIN_TELEGRAM_WEBHOOK_SECRET` (dibandingkan konstan-waktu + header
  `X-Telegram-Bot-Api-Secret-Token`).
- **Allowlist grup** dikelola admin di **Master Data → "Grup Telegram Bot"**
  (kind `telegram_chat`; `Kode` = chat_id). Tidak perlu deploy ulang.
  `meta_json` per-grup menyiapkan `allow_open`/`allow_ticket`/dst. (default semua
  boleh).
- **Idempotensi** via tabel `bot_update_log` (kiriman ulang Telegram diabaikan).
- **Audit** command via `bot_command_log`.
- Semua mutasi memakai layanan yang sama dengan panel (workflow, `AppendEvent`,
  `enqueueCreated`, sheet sync) → tidak mem-bypass aturan. `Source = "telegram"`.
- Kode: `internal/bot/*`, `internal/api/telegram_hook_routes.go`,
  `internal/repository/bot.go`, migrasi `00024_telegram_bot.sql`.


Tambahan skema (migrasi baru, kecil):
`inbound_sources` (jenis, kredensial terenkripsi, default queue, is_active),
`webhook_tokens` (token hash, label, is_active, last_used_at).

**Manfaat utama:** alarm monitoring otomatis menjadi tiket dengan dedup
(mis. alert yang sama tidak membuat tiket baru selama tiket sebelumnya belum ditutup).

---

## F13 — Kanal Notifikasi Tambahan

Provider sudah abstrak sejak F1. Yang perlu ditambah hanya implementasi + entri registry:

| Kanal | Catatan |
|---|---|
| Slack / Discord | HTTP webhook — implementasi paling sederhana |
| Whatsapp Cloud API (Meta resmi) | Template approval, biaya per percakapan |
| SMTP email | Untuk notifikasi formal & laporan harian |
| SMS gateway | Untuk eskalasi kritikal di luar jam kerja |
| Push (opsional) | ntfy / Gotify untuk notifikasi self-hosted |

Skema tidak berubah: `target_bindings.channel` + `notification_providers.kind`.

---

## F18 — Sinkronisasi Google Spreadsheet ✅ SELESAI

**Tujuan:** Todo Task dan Daily Task yang dibuat NOC otomatis tercatat di Google
Spreadsheet sebagai daftar/history kerjaan, sehingga tidak perlu update manual.

| Aspek | Implementasi |
|---|---|
| Cakupan | `item_type IN (task, daily_task)` — item baru & perubahan setelahnya |
| Kredensial | Service account JSON, di-upload dari panel admin (terenkripsi AES-GCM) |
| Target | Spreadsheet ID + nama sheet diisi admin dari panel; tombol **Buat Sheet Tab** |
| Mode | Realtime (antrean DB `sheet_sync_queue`, worker ≤ 30 dtk) |
| Baris | Satu baris per item; status diperbarui in-place via kolom **Ref**; hapus → ditandai **Dihapus** |
| Jejak | Kolom **Diperbarui Oleh** + **Keterangan** (diisi saat Daily Task selesai) |
| Keandalan | Idempotent `event_key`, backoff 1/2/5/15/60 mnt, recovery job |

Migrasi `00011` menambah `sheet_sync_config` + `sheet_sync_queue`. UI di grup
**MANAJEMEN → Google Sheets**. Interval: `INGATIN_SHEET_SYNC_INTERVAL_SECONDS`.

---

## F20/F21 — SLA/KPI, Assign, Lampiran & Penanganan ✅ SELESAI

| Aspek | Implementasi |
|---|---|
| Siklus SLA | Tabel `ticket_sla_cycles` (per siklus, `closed_at`+`reopened_at`) |
| SLA policy | Per prioritas (critical/high/normal/low), kalender 24 jam |
| Assign | 1 owner + banyak collaborator "ikut menangani" (kredit KPI setara) |
| Force unlock | Admin buka tiket closed tanpa aturan transisi (alasan wajib) |
| KPI | `GET /api/kpi/sla` + halaman KPI dengan grafik Recharts |
| Ekspor | CSV + tombol Google Sheets |
| Lampiran | Maks 50 MB, unduh ber-auth, hanya di aplikasi |
| Penanganan | 3 field: issue_found/troubleshooting/action_solution |

Migrasi `00014` (siklus, collaborator, kolom penanganan, event `unlocked`,
policy per prioritas) + `00015` (template SLA).

---

## F22 — RBAC Dinamis, Manajemen KPI, Ringkasan Tugas & Kategori Tim ✅ SELESAI

| Aspek | Implementasi |
|---|---|
| Catatan penanganan | 3 kolom (issue/troubleshooting/action) **selalu dapat disunting** — tanpa tombol Edit |
| KPI manajemen | Halaman KPI khusus peran manajerial (izin `kpi.view`) |
| Filter KPI | `?person=<username>` + dropdown petugas (`GET /api/kpi/users`) |
| Peran dinamis | Tabel `roles` (tambah/ubah/hapus dari panel) + peran `manager`, `spv`, `owner` |
| Matriks izin | Action `items/providers/masterdata/users/teams/roles.write` + `kpi.view` |
| Label terlambat | Badge merah "Terlambat" pada daily task yang melewati `due_at` |
| Menunggu pelanggan | Tombol "Menunggu Konfirmasi Pelanggan" (state `waiting_customer`) |
| Ringkasan tugas | Notifikasi ringkasan harian pending/in_progress/done; mode `on_change` / `interval` / `off` |
| Kategori tim | `teams.category` + master-data kind `team_category`; CRUD tim di Users & Teams |

Migrasi `00016` (tabel `roles`, drop CHECK role, `teams.category`, kind
`team_category`, settings ringkasan, template `DAILY_SUMMARY`) + `00017`
(izin `teams.write` untuk manager).

**Endpoint baru:** `GET/POST/PATCH/DELETE /api/roles`, `GET /api/kpi/users`,
`GET/POST /api/settings/daily-summary`, `POST /api/settings/daily-summary/preview`.
`/auth/me` kini menyertakan `permissions` + `is_super`.

---

## F23 — Menu "Aktivasi / EWO" (RFS) ✅ SELESAI

Menu **RFS** diubah menjadi **"Aktivasi / EWO"** dengan halaman tersendiri,
terpisah dari **Ticketing** (`item_type = rfs` tetap).

| Aspek | Implementasi |
|---|---|
| Field utama | Nama Customer, **Product** (jenis paket), **Priority**, Bandwidth, **Tanggal RFS**, Tag |
| Jenis paket | Master Data kind baru `service_package` (Internet + Metro, Metro, Internet Only, CDN) — dapat diubah/ditambah dari panel |
| Tag | Preset `change_service`, `upgrade`, `new_install` (label "New"), `request`, `urgent` + tag bebas |
| Aksi | **In Progress**, **Troubleshoot** (→ `pending_troubleshoot` + catatan penanganan), **Close Ticket** (→ `activated`, tampil "Closed/Completed"), **Tandai Aktif**, **Cancel**, **Delete** |
| Catatan penanganan | 3 kolom baru pada `rfs_details`: issue_found / troubleshooting / action_solution (modal Troubleshoot + halaman detail) |
| Cancel | admin/manager bebas; **sales hanya pembuat RFS** |
| Delete | **alasan wajib**; admin (force delete) atau pembuat/owner RFS |
| Filter | "Hanya belum aktivasi" + filter status & tahap instalasi |

Migrasi `00018` (kind `service_package`, preset tag, kolom penanganan `rfs_details`).

**Endpoint baru:** `POST /api/items/{id}/rfs-cancel`,
`POST /api/items/{id}/rfs-delete`. State workflow RFS baru: `pending_troubleshoot`.

---

## F24 — Jenis Daily Task + Tiket Otomatis ✅ SELESAI

**Tujuan:** Daily Task punya jenis pekerjaan, dan tugas yang menemukan gangguan
dapat langsung membuat tiket tanpa input ulang.

| Aspek | Detail |
|---|---|
| Jenis | Master Data kind baru `daily_task_type` dengan meta `dapat_membuat_tiket` |
| Jenis awal | Pengecekan Gangguan, Backup, Monitoring (dapat tiket) + Administrasi, Lainnya (tanpa tiket) — **wajib dipilih** |
| Hasil | Dialog "Tandai Selesai" memilih **Normal** atau **Bermasalah (Gangguan)** |
| Tiket otomatis | Hasil "Bermasalah" → opsi **Buat tiket** (incident **atau** request), membuat tiket + menautkan ke task |
| Idempotent | Satu task maksimal menghasilkan satu tiket (tabel `daily_ticket_links`) |
| Izin eskalasi | Admin, pembuat/owner, **anggota tim yang sama**, atau **role/peran operasional yang sama** dengan pembuat/owner task |
| Pewarisan | Tiket mewarisi owner, tim, target, ref, tag, dan deskripsi (catatan + deskripsi task) |
| Notifikasi | Template baru `TICKET_FROM_DAILY` (menyertakan ref task sumber) |
| Badge | Kartu menampilkan badge jenis + badge "Bermasalah" |

Migrasi `00019` (kind `daily_task_type`, kolom `task_details.daily_task_type` +
`result_status`, tabel `daily_ticket_links`).

**Endpoint baru:** `POST /api/items/{id}/escalate-ticket`.
`POST /api/items/{id}/status` menerima `result_status` (normal/bermasalah).

---

## F25 — Peningkatan RFS/Aktivasi-EWO, PIC Team & Penanganan ✅ SELESAI

**Tujuan:** memperkuat alur Aktivasi/EWO, kepemilikan tiket, dan data teknis aktivasi.

| Aspek | Detail |
|---|---|
| Target notifikasi per tim | `teams.target_id` → resolusi target: item → tim (termasuk PIC team RFS) → default |
| PIC Team | `rfs_details.pic_team_id` menggantikan input bebas PIC NOC (kolom lama tetap untuk data historis) |
| Data Aktivasi | Tabel `rfs_activation_data` (IP, VLAN, Interface/Port, Bandwidth test, Ping test, Packet loss) + tab **Aktivasi** (RFS) |
| Tab Evidence | Lampiran RFS dipindah ke tab **Evidence**; tipe lain tetap di tab Aktivitas |
| Owner lock | Bila owner terisi, hanya **admin** yang dapat menggantinya, **alasan wajib**; non-admin ditolak (403) |
| Ikuti Tiket | Self-claim (owner = diri sendiri) untuk RFS/incident/request, hanya saat owner kosong; "Ikut Menangani" tetap |
| Lepas penangan | **Hanya admin** boleh melepas penangan lain; non-admin hanya bisa melepas diri sendiri |
| Catatan Penanganan | Hanya tampil pada status `pending_troubleshoot`; di luar itu pakai field Catatan (deskripsi/komentar) |
| Konfirmasi status | Modal konfirmasi untuk **semua** perpindahan status; dropdown "Rubah Status" generik dihapus untuk RFS |
| Postpone | Tombol Postpone (→ `postponed`) pada RFS (daftar + detail) |

Migrasi `00020` (`teams.target_id`, `rfs_details.pic_team_id`, tabel `rfs_activation_data`).

**Endpoint diperbarui:** `POST /api/items/{id}/assign` menerima `reason` (wajib saat
admin mengganti owner terisi) + `force`; `PATCH /api/items/{id}` menerima
`rfs.pic_team_id` + `rfs.activation`.

---

## F26 — Catatan (Sticky Notes) ✅ SELESAI

**Tujuan:** catatan cepat tim yang dapat dibagikan antar tim.

| Aspek | Detail |
|---|---|
| Bentuk | Sticky notes **mandiri** (tidak menempel ke work item), judul/isi/warna/pin |
| Visibilitas | Flag **internal / eksternal** |
| Tim pemilik | `notes.owner_team_id`; tim pemilik selalu dapat melihat |
| Berbagi | Tombol **Bagikan** → pilih **multi tim** (`note_shares`, many-to-many) |
| Scope baca | Tim pemilik + tim yang di-share; admin/super melihat semua |
| Izin baris | Edit/hapus = admin, pembuat, atau anggota tim pemilik |
| RBAC | Aksi baru `notes.view`, `notes.write` (didaftarkan pada Role Permissions) |
| Menu | **OPERASIONAL**, tepat di bawah "Aktivasi / EWO" (ikon `sticky_note_2`) |

Migrasi `00021` (tabel `notes` + `note_shares` + izin `notes.view/write`).

**Endpoint baru:** `GET/POST /api/notes`, `GET/PATCH/DELETE /api/notes/{id}`,
`PATCH /api/notes/{id}/shares` (berbagi multi-tim).

---

## F27 — Waktu Selesai & Diselesaikan Oleh ✅ SELESAI
**Tujuan:** memperjelas kapan dan oleh siapa sebuah item diselesaikan, serta
mencatatnya ke spreadsheet.

| Aspek | Detail |
|---|---|
| Waktu Selesai | Turunan `resolved_at` → fallback `closed_at` (`completed_at`) |
| Diselesaikan Oleh | Actor event status-final (`done/closed/activated/completed/fulfilled/resolved`); fallback `updated_by_username` |
| Tampilan | Field **Waktu Selesai** + **Diselesaikan oleh** pada bagian Detail & Informasi |
| Timeline | Event penyelesaian ditandai badge **Selesai** (warna success) |
| Spreadsheet | Kolom baru **Selesai (WIB)** + **Diselesaikan Oleh** (sisip sebelum Keterangan); `LastColumn` O → Q |

Tanpa migrasi DB (field turunan dihitung saat detail dimuat).

---

## F28 — Dark Mode (Light / Dark / System) ✅ SELESAI

**Tujuan:** dukungan mode gelap penuh dengan pemilih tema, tanpa mengubah
seluruh komponen.

| Aspek | Detail |
|---|---|
| Pendekatan | Token warna → **CSS variable** (`:root` = Light, `.dark` = Dark) di `styles.css`; Tailwind memetakan token ke `rgb(var(--x) / <alpha-value>)` |
| Mode | `light` \| `dark` \| `system` (default **system** / ikut OS) |
| Persistensi | `localStorage['ingatin_theme']` |
| Toggle | Topbar, di sebelah kanan tombol notifikasi (juga di drawer mobile) |
| Anti-FOUC | Script inline di `index.html` menetapkan kelas tema sebelum React mount |
| Ikut OS | `matchMedia('(prefers-color-scheme: dark)')` + listener saat mode=system |
| Chart | Palet Recharts dibaca dari `--palette-*` (MutationObserver) |
| Native | `color-scheme: light/dark` (scrollbar & form) |

**File:** `src/styles.css`, `tailwind.config.js`, `index.html`,
`src/lib/theme.ts` (baru), `src/components/ThemeToggle.tsx` (baru),
`src/App.tsx`, `src/pages/Notes.tsx`, `src/pages/Kpi.tsx`,
`src/design/tokens.ts` (komentar + varian dark tag).
Tanpa migrasi DB — deploy frontend-only (`docker compose build web`).

---

## F29 — Master Data Default (Snapshot Penuh) ✅ SELESAI

**Tujuan:** seluruh master data yang dipakai aplikasi menjadi konfigurasi
default yang ikut terpasang saat instalasi di server baru (tanpa restore dump).

- Migrasi `00022_master_data_defaults.sql`: **13 `master_data_kinds`** +
  **63 baris `master_data`** (team_category, ticket_category + 7 sub,
  incident_type, ticket_tag, tag_color, product, service_package,
  daily_task_type, rfs_category, reminder_category).
- Seluruh `INSERT` idempoten (`ON CONFLICT DO NOTHING`) — di DB berjalan hanya
  menambah baris yang belum ada (mis. `ticket_tag` yang dulu dibuat via panel).
- Sub-kategori tiket memakai pola `JOIN parent` (aman bila induk belum ada).
- **Bukan** bagian master data (tidak disertakan): konfigurasi Google Sheets &
  provider Telegram/WhatsApp.
- Down: hanya membersihkan milik 00022 (`ticket_tag`).

**Endpoint/berkas:** `backend/internal/db/migrations/00022_master_data_defaults.sql`.

---

## F30 — Format Notifikasi Pembuatan (Task / Daily Task / RFS-EWO) ✅ SELESAI

**Tujuan:** menyeragamkan pesan notifikasi **pembuatan** agar menampilkan
tanggal + jam dibuat, tenggat waktu, dan pembuatnya.

- Standar baris: **`Tanggal Dibuat`** (`{{.CreatedAt}}`, WIB tanggal+jam),
  **`Tenggat Waktu`** (`{{dash .DueAt}}` / `{{dash .ExpireAt}}`),
  **`Dibuat Oleh`** (`{{dash .CreatedBy}}`).
- Template diperbarui: `TODO_CREATED`, `DAILY_TASK_CREATED` (telegram+whatsapp).
- Template baru **`RFS_CREATED`** (severity info) untuk pembuatan RFS/EWO —
  menggantikan pemakaian `RFS_UPCOMING` (template pengingat) pada event create,
  sekaligus memperbaiki label offset kosong.
- Daily task tetap default tenggat **23:59 WIB**.
- Template pengingat (`RFS_UPCOMING`/`RFS_TODAY`/`RFS_LATE`) tidak diubah.

**Berkas:** `00023_notification_created_formats.sql`,
`notify/template.go` (`TemplateRFSCreated` + fallback),
`api/work_item_routes.go` (`createdTemplateFor`), `notify/notify_test.go`.

---

## F31 — Pembedaan Data per Tim (Daily Task, Todo, Catatan) ✅ SELESAI

**Tujuan:** memisahkan Daily Task & Todo (work_item `task`/`daily_task`) dan
Catatan per **tim konkret** (`teams.id`), sehingga tiap tim hanya melihat &
mengelola pekerjaannya sendiri.

| Aturan | Detail |
|---|---|
| Admin/super | Lihat & kelola **semua** tim |
| Non-admin bertim | Hanya item `team_id` = tim pengguna (list, get, tulis) |
| Non-admin tanpa tim | Hanya item miliknya sendiri (`created_by`/`owner`) |
| Item legacy tanpa tim | Hanya admin (data lama tidak di-backfill) |
| Cakupan | `task`, `daily_task`, `notes` (Catatan sudah ber-scoping) |
| Tidak berlaku | RFS/Tiket/Reminder (tetap seperti semula) |

- **Create**: `team_id` dipaksa ke tim pengguna untuk non-admin; admin bebas.
- **Read guard**: `canViewItemTeam` pada get/events/comments/komentar.
- **Write guard**: `canWriteItemTeam` pada status/update/delete/assign/
  collaborator/escalate.
- **Dashboard (Y)**: scope tim **hanya** membatasi baris `task`/`daily_task`;
  tiket/RFS/reminder tetap global bagi non-admin.
- Tanpa share lintas tim untuk Todo/Daily.

**Berkas:** `internal/api/team_scope.go` (baru), `work_item_routes.go`,
`collaborator_routes.go`, `daily_escalate_routes.go`,
`internal/repository/work_items.go` (`TeamID`/`OwnerScopeUsername`),
`internal/repository/events.go` (`DashboardScope`), FE `DailyTasks.tsx`,
`Todos.tsx`, `api.ts`. **Tanpa migrasi DB.**

---

## F32 — Akses Notification Center (izin `providers.view`) ✅ SELESAI

**Tujuan:** halaman Notification Center hanya dapat dilihat admin (super user)
dan role yang diberi izin; Escalation Policies tetap terbuka untuk semua.

- Izin baru **`providers.view`** (migrasi `00025_providers_view_permission.sql`):
  seed admin/adminnoc/noc = TRUE, role lain FALSE (dapat diubah lewat Role
  Permissions).
- Backend membatasi baca `GET /providers`, `GET /targets`, `GET /templates`
  dengan `requirePermission("providers.view")`. `GET /policies` tetap terbuka.
- Endpoint baru **`GET /targets/options`** (id+nama, tanpa binding) tetap
  terbuka agar dropdown target pada form (Todo/Daily/RFS/Tiket/Reminder/Users)
  berfungsi bagi semua role.
- FE: menu **Notification Targets** & **Providers** + kotak *Notifikasi
  Telegram & WA* di Master Data dijaga `providers.view`; dropdown form memakai
  `targetOptions()`.

**Berkas:** `migrations/00025_providers_view_permission.sql`,
`api/server.go`, `api/target_routes.go`, `repository/targets.go`,
`repository/master_data.go`, `api/master_data_routes.go`; FE `App.tsx`,
`api.ts`, `MasterData.tsx`, `RolePermissionsPage.tsx`, `Reminders.tsx`,
`Rfs.tsx`, `Tickets.tsx`, `Todos.tsx`, `Users.tsx`.

---

## F33 — Perbaikan Data Aktivasi & Akses Notification Center ✅ SELESAI

**Dua perbaikan:**

1. **Data aktivasi RFS persisten tampil.** Bug FE di `WorkItemDetail.tsx`:
   efek sinkronisasi draf data aktivasi menandai `activationLoaded` pada render
   pertama saat `detail.data` masih `null`, sehingga data yang sudah tersimpan
   tidak pernah ditampilkan. Diperbaiki dengan menunggu `detail.data` benar-benar
   termuat sebelum menandai "loaded". **Tanpa migrasi DB.**

2. **Notification Center hanya untuk admin (default).** Migrasi
   `00026_providers_view_admin_only.sql` mencabut izin `providers.view` dari
   `noc` & `adminnoc` (kebijakan F32 sebelumnya memberikannya). Kini default
   **hanya role `admin`**; role lain dapat diberi izin manual lewat Role
   Permissions. `GET /providers`, `/targets`, `/templates` → **403** untuk role
   tanpa izin (idempoten; `Down` mengembalikan kebijakan F32).

**Berkas:** `migrations/00026_providers_view_admin_only.sql`;
FE `pages/WorkItemDetail.tsx`.

---

## F34 — Backup & Restore dari Website + Auto Upload FTP ✅ SELESAI

**Tujuan:** admin dapat membuat, mengunduh, memulihkan, dan menghapus cadangan
database langsung dari panel, serta mengunggah cadangan otomatis ke server FTP.

**Backend:**
- Paket baru `internal/backup`:
  - `Create` menjalankan `pg_dump -Fc -Z6` (format custom) ke `<DataDir>/backups`;
  - `Restore` memakai `pg_restore --clean --if-exists --no-owner`;
  - `List`/`Path`/`Delete`/`Prune` untuk pengelolaan berkas (anti path-traversal);
  - `ftp.go`: klien FTP minimal (stdlib — USER/PASS/TYPE/MKD/PASV/STOR) untuk
    unggah berkas; password FTP disimpan terenkripsi AES-256-GCM;
  - `config.go`: konfigurasi bertipe dari tabel `settings` (key `backup.config`).
- Migrasi `00027_backup_config.sql` menyemai setting default (nonaktif).
- Rute admin-only (`server.go`): `GET /backup`, `POST /backup` (buat manual),
  `POST /backup/config`, `POST /backup/ftp/test`, `GET /backup/{name}` (unduh),
  `DELETE /backup/{name}`, `POST /backup/{name}/upload`,
  `POST /backup/{name}/restore` (wajib `{"confirm":"RESTORE"}`).
- Worker: job `runBackupTick` (cek tiap menit, eksekusi mengikuti jadwal cron
  pada settings) membuat cadangan + unggah FTP + pangkas retensi.
- Config: `INGATIN_BACKUP_DIR`, `INGATIN_BACKUP_AUTO_ENABLED`,
  `INGATIN_BACKUP_SCHEDULE` (default `0 2 * * *`), retensi memakai
  `INGATIN_BACKUP_KEEP_DAYS`.

**Frontend:**
- Halaman baru `pages/Backup.tsx` (menu MANAJEMEN, adminOnly): daftar berkas
  (ukuran, waktu WIB, status FTP), tombol buat/unduh/unggah/pulihkan/hapus,
  form konfigurasi cadangan otomatis + FTP (host/port/user/password/dir/PASV),
  uji koneksi FTP. Pemulihan memakai `ConfirmDialog` bertone bahaya.
- `api.ts`: `backupApi` (termasuk unduhan ber-auth via blob).

**Berkas:** `internal/backup/{backup.go,ftp.go,config.go,*_test.go}`,
`internal/db/migrations/00027_backup_config.sql`, `internal/api/backup_routes.go`,
`internal/api/server.go`, `internal/worker/worker.go`, `internal/config/config.go`;
FE `pages/Backup.tsx`, `App.tsx`, `api.ts`, `.env.example`.

**Verifikasi:** pg_dump/pg_restore tersedia di image; cadangan manual 208 KB
(format custom `PGDMP`) berhasil dibuat & diunduh; unggah FTP diuji terhadap
stub server FTP (PASV+STOR) — lolos; job worker auto-backup berjalan & mencoba
unggah; role non-admin → 403 pada semua endpoint; trail audit `backup.*` tercatat.

**F34.1 — Unggah berkas cadangan dari komputer & pulihkan.** Menambahkan
`POST /api/backup/upload` (multipart field `file`; `restore=true`+`confirm=RESTORE`
untuk langsung memulihkan) dan helper `backup.SaveUpload` (memvalidasi magic
`PGDMP`, nama berkas dibentuk ulang ber-timestamp). Halaman Backup mendapat
panel **Unggah & Pulihkan dari Berkas**: pilih berkas `.dump` → *Simpan ke
Server* atau *Unggah & Pulihkan* (konfirmasi bertone bahaya). Perbaikan: helper
`request()` tidak lagi menetapkan `Content-Type: application/json` untuk body
`FormData` (memperbaiki multipart).

**Berkas (F34.1):** `internal/backup/backup.go` (`SaveUpload`, `ErrNotDump`),
`internal/api/backup_routes.go` (`handleUploadBackupFile`), `internal/api/server.go`;
FE `pages/Backup.tsx`, `api.ts`. **Tanpa migrasi DB.**

---

## F14 — Portal Customer & Multi-Tenant

**Tujuan:** customer dapat melihat status layanan/RFS/tiket miliknya sendiri.

**Sudah tersedia dari F1:** tabel `organizations` + kolom `work_items.organization_id`.

**Pekerjaan:**
- Aktifkan `organization_id` pada semua query (scope per organisasi).
- Role baru: `customer` (read-only, hanya data organisasinya).
- Halaman portal: daftar RFS milik customer, status tiket, riwayat layanan.
- Branding per organisasi (nama, logo) — reuse struktur `settings` per organisasi.
- Publik status page (opsional) untuk layanan yang sedang gangguan.

---

## F15 — Reporting & Analytics

**Tujuan:** visibilitas operasional jangka panjang.

**Sumber data:** `work_item_events` (append-only sejak F1) — tidak perlu backfill.

| Laporan | Isi |
|---|---|
| Tiket | volume per periode, per queue, per kategori, per agent |
| SLA | % pencapaian, rata-rata respons/penyelesaian, tren bulanan |
| Reminder | tingkat keberhasilan pengiriman, reminder yang paling sering expired tanpa aktivasi |
| RFS | ketepatan RFS (on-time vs delay), rata-rata delay |
| Notifikasi | success rate per provider/channel, penyebab kegagalan terbanyak |
| Beban kerja | tiket per agent, rata-rata tiket aktif, MTTR |

Implementasi: materialized view yang di-refresh berkala + halaman report + export CSV/PDF.

---

## Prinsip Evolusi

1. **Skema stabil sejak awal.** Kolom dan tabel "masa depan" sudah nullable di `0001`.
2. **Satu inti, banyak tampilan.** `work_items` + `work_item_events` melayani semua tipe;
   perbedaan hanya extension dan UI.
3. **Append-only event log.** `work_item_events` tidak pernah diubah/dihapus → SLA dan
   laporan historis tetap akurat.
4. **Provider & template dapat dikonfigurasi runtime.** Menambah kanal tidak perlu deploy ulang.
5. **Setiap fase diverifikasi Level A** (`PLAN.md` §4) sebelum lanjut.
