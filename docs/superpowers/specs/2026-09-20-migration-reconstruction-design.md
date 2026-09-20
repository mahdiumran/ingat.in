# Design Spec: Rekonstruksi Migration PostgreSQL Ingat.in

**Tanggal:** 2026-09-20  
**Status:** Disetujui untuk ditulis dan direview  
**Scope:** Fresh PostgreSQL database only

## Tujuan

Memulihkan migration yang hilang agar backend dapat di-embed, dibangun, dijalankan, diuji, dan dipakai melalui API. Migration asli tidak tersedia di upstream `mahdiumran/ingat.in` (`5dbb73cf`), sehingga SQL direkonstruksi dari kontrak backend, model, integration test, `PLAN.md`, `DEVELOPMENT.md`, dan `scripts/smoke.sh`.

## Batasan

- Database lama berisi data tidak didukung.
- Tidak ada password, token, atau secret di SQL migration.
- Bootstrap admin tetap dilakukan aplikasi dari environment.
- Migration dijalankan goose melalui `backend/internal/db/db.go` dan di-embed dengan `//go:embed migrations/*.sql`.
- `Down` disediakan untuk development saja, bukan prosedur rollback produksi.
- Tidak mengubah fitur baru di luar pemulihan schema dan data seed yang diwajibkan kontrak saat ini.

## Struktur migration

### `00001_init.sql`

Membuat extension `pgcrypto`, helper timestamp convention, seluruh tabel dasar, foreign key, check constraint, unique constraint, dan indeks awal:

- Organisasi, tim, users, refresh tokens.
- Workflow definitions dan reference counters.
- `work_items`, events, comments, attachments, watchers.
- Extension 1:1: task, reminder, RFS, ticket.
- Ticketing/SLA support: queue, queue members, categories, incident links, SLA policies, SLA targets, business calendars.
- Notification targets, bindings, providers, escalation policies, templates, outbox.
- Audit logs dan settings.
- Master-data tables yang diperlukan query runtime dapat dibuat pada migration lanjutan sesuai milestone dokumentasi.

Kontrak utama: UUID memakai `gen_random_uuid()`, timestamp memakai `TIMESTAMPTZ NOT NULL DEFAULT now()`, `work_items.paused_total_seconds` default `0`, `sla_state` default `none`, dan `item_type` mencakup tipe domain yang telah didukung pada tahap tersebut.

### `00002_seed.sql`

Seed idempoten untuk baseline F1:

- Organisasi `Internal`.
- Tim minimal `NOC`, `Sales`, dan satu tim operasional tambahan.
- Workflow task, reminder, RFS, incident, request, change.
- Policy `TRIAL-3D` dengan `H-2`, `H-1`, `H-0`, `LATE-1H`.
- Policy `RFS-DEFAULT` dengan `H-7`, `H-3`, `H-1`, `H-0`, `LATE-4H`.
- Placeholder SLA policy.
- Target `NOC-Team`.
- Template notifikasi Telegram dan WhatsApp.
- Settings runtime dasar.

Seed tidak membuat admin karena password harus berasal dari bootstrap aplikasi.

### `00003_add_template_null_index.sql`

Menambahkan indeks unik parsial:

```sql
ux_notification_templates_key_no_channel
  ON notification_templates (key)
  WHERE channel IS NULL
```

Indeks ini mendukung `ON CONFLICT (key) WHERE channel IS NULL` dan mencegah duplikasi template channel-null.

### `00004_fix_notification_templates.sql`

Memperbaiki seed template yang diperlukan runtime, termasuk placeholder RFS yang harus cocok dengan field `PicNOC`, serta memastikan template channel memakai conflict target yang benar. Tidak menghapus data pengguna.

### `00005_add_master_data.sql`

Membuat dan seed:

- `master_data` dengan hierarki `parent_id` dan `meta_json`.
- `master_data_kinds`.
- `role_permissions`.

Seed minimal mencakup kelompok master data dan izin admin yang diperlukan endpoint/panel.

### `00006_add_customer_master_data.sql`

Menambahkan kind `customer` dan seed customer/master-data awal. Kode customer memakai `ref_counters`; atribut terstruktur disimpan dalam `meta_json` sesuai kontrak F12.1.

### `00007_update_task_workflow.sql`

Memperbarui workflow task ke status:

```text
accepted -> on_progress -> expired -> canceled -> closed
```

Migration fresh DB hanya perlu memastikan workflow final tersedia. Tidak ada data lama yang perlu dipetakan karena scope tidak mendukung database existing.

### `00008_add_ticket_fields_and_master_data.sql`

Menambahkan `ticket_details.incident_type` bila belum ada, seed jenis gangguan, warna tag, kategori tiket, serta subkategori dengan `parent_id`. Constraint/seed mendukung validasi impact dan urgency di API.

### `00009_add_notification_descriptions.sql`

Memperbarui body template Telegram dan WhatsApp untuk menyertakan:

```text
Deskripsi : {{.Description}}
```

Minimal mencakup `TODO_CREATED`, `REMINDER_OFFSET`, dan `RFS_UPCOMING`.

### `00010_add_daily_tasks.sql`

Memperluas `work_items.item_type` dengan `daily_task`, menambahkan workflow:

```text
pending -> in_progress -> done
pending <-> done
pending -> canceled
```

Menambahkan template `DAILY_TASK_CREATED`, indeks `ix_work_items_daily(item_type, start_at)`, dan prefix reference `DTK` melalui kontrak counter aplikasi.

## Integritas data

- FK memakai `ON DELETE` sesuai lifecycle: relasi parent yang wajib mencegah orphan; sesi/token dan extension mengikuti parent dengan cascade bila sesuai kontrak repository.
- `notification_outbox.event_key` unique untuk idempotensi enqueue.
- `notification_templates` memakai unique constraint/index terpisah untuk channel bernilai dan channel `NULL`.
- `work_items.ref_no` unique.
- Counter memakai unique `(prefix, year)` dan atomic upsert.
- Status work item tidak dibatasi daftar global; validasi status dilakukan workflow service per `item_type`.
- Semua input yang berasal dari HTTP tetap divalidasi di Go, bukan hanya mengandalkan constraint database.

## Validasi penerimaan

Pada PostgreSQL kosong:

1. Backend build sukses dan migration ter-embed.
2. Goose menerapkan `00001` sampai `00010`; schema version menjadi `10`.
3. Database memiliki minimal 30 tabel publik.
4. Seed memenuhi seluruh pemeriksaan `scripts/smoke.sh`.
5. Indeks `ux_notification_templates_key_no_channel` tersedia.
6. Migration dijalankan ulang tanpa duplikasi atau error.
7. `go test ./...`, `go vet ./...`, dan integration test dengan PostgreSQL lulus.
8. API health/database health, login, proxy frontend, dan smoke test lulus.
9. Tidak ada perubahan pada fitur frontend yang tidak diperlukan untuk acceptance migration.

## Output dan commit

Output implementasi dibatasi pada:

- `backend/internal/db/migrations/00001_*.sql` hingga `00010_*.sql`.
- Satu test kontrak schema yang memeriksa daftar migration `00001`–`00010`, urutan versi, dan marker goose.
- Dokumentasi desain ini dan implementation plan terpisah.

Setelah design spec direview, implementation plan akan memecah pekerjaan per migration, menjalankan tes gagal-dulu-lalu-lulus untuk kontrak penting, lalu melakukan validasi end-to-end.
