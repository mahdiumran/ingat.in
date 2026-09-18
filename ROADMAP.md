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
| **Telegram command** | Bot command `/ticket <judul>` untuk membuat tiket dari chat NOC |
| **API publik** | Token per integrasi + rate limit |

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
