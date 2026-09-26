# Ingat.in

<img width="2559" height="1189" alt="image" src="https://github.com/user-attachments/assets/dd824f74-c3e5-4704-8148-6bdb274a6917" />

Hub **reminder & notifikasi self-hosted** untuk NOC/ISP, dirancang untuk berkembang
menjadi **platform ticketing internal NOC + SLA**.

Ingat.in mengingatkan hal-hal yang mudah terlupa: todo task yang harus dikirim notifikasinya,
trial layanan yang akan habis, dan tanggal **RFS** (Ready For Service) yang mendekat —
semuanya dikirim ke **Telegram** dan **WhatsApp**.

---

## Fitur

| Area | Keterangan |
|---|---|
| **Todo Tasks** | Task terencana; notifikasi otomatis terkirim saat dibuat |
| **Daily Task** | Checklist harian bergaya todo list (papan 3 kolom) dengan carry-over task belum selesai + ringkasan otomatis |
| **Ticketing** | Insiden/permintaan/change dengan SLA otomatis (first response → closed), reopen & siklus SLA |
| **Reminder** | Reminder dengan titik peringatan berjenjang (H-7/H-3/H-2/H-1/H-0) + eskalasi LATE |
| **Aktivasi / EWO (RFS)** | Data RFS diisi sales; NOC otomatis diingatkan saat tanggal RFS mendekat; PIC berupa tim; data teknis aktivasi; cancel/hapus ber-alasan |
| **Notifikasi** | Telegram + WhatsApp; target berupa grup/tim atau personal; template bisa diedit |
| **Notification Center** | Providers, Notification Targets, Template & Escalation Policies (akses diatur izin `providers.view`) |
| **Provider WA** | WAHA (bawaan), atau Fonnte/Wablas/Starsender/custom HTTP — bisa diganti dari panel |
| **Bot Telegram (inbound)** | Perintah dari grup Telegram (allowlist) untuk membuat tiket/todo |
| **Catatan (Sticky Notes)** | Catatan tim dengan warna, pin, visibilitas internal/eksternal & berbagi antar-tim |
| **Google Sheets** | Todo Task & Daily Task otomatis tercatat ke spreadsheet (service account, antrean tahan gangguan) |
| **KPI & SLA** | Siklus SLA per tiket, penanganan (owner + ikut menangani), KPI per person dengan grafik |
| **Lampiran & Penanganan** | Lampiran pendukung (50 MB) + catatan Issue/Troubleshooting/Solusi pada tiket |
| **Tema** | Mode Terang / Gelap / Ikut Sistem |
| **RBAC dinamis** | Peran & matriks izin (role × action) dapat diubah dari panel |
| **Pembedaan per tim** | Daily Task, Todo & Catatan terpisah per tim |
| **Backup & Restore** | Cadangan database dari panel (pg_dump), unduh, unggah berkas untuk restore, **auto upload ke FTP** |
| **Audit & Outbox** | Setiap pengiriman tercatat: status, percobaan, error |

---

## Arsitektur Singkat

```
Browser → ingatin-web (nginx :8091) → /api → ingatin -mode=api (:8081)
                                              ingatin -mode=worker (cron jobs)
                                              waha (:8082) → WhatsApp
                                              PostgreSQL (reuse host)
```

- **Bahasa:** Go 1.25 (chi + pgx/v5 + goose)
- **Frontend:** React + TypeScript + Vite + Tailwind CSS
- **Satu binary, tiga mode:** `api` · `worker` · `migrate`
- **Satu image, satu `docker-compose.yml`** (service: `migrate`, `api`, `worker`, `waha`, `web`)

---

## Prasyarat

- Docker Engine + Docker Compose v2 (`docker compose`)
- `openssl` dan `curl`
- PostgreSQL **tidak perlu** di host — installer menjalankan PostgreSQL 16 sebagai
  container (Mode B). Lihat `DEPLOYMENT.md §5.2` untuk Mode A (reuse host).

---

## Mulai Cepat

```bash
# 1. Clone repo
git clone <URL-REPO> ingat.in
cd ingat.in

# 2. Pasang (preflight, buat .env + secret, build, jalankan semua service)
./install.sh
```

Installer bersifat **idempoten** — `.env` yang sudah ada tidak akan ditimpa, sehingga
aman dijalankan ulang untuk memperbarui.

Akses:

```
Web UI   : http://<IP-SERVER>:8091
API      : http://127.0.0.1:8081/api/health
```

Kredensial admin awal tercetak oleh installer (atau lihat `.env`).

> **Catatan:** `install.sh` dan seluruh skrip di `scripts/` memakai lokasi relatif
> terhadap repo (`SCRIPT_DIR`/`PROJECT_DIR`), jadi dapat dijalankan dari direktori
> mana pun. Tidak ada path absolut yang di-hardcode.

---

## Update / Deploy Ulang

Setelah menarik perubahan (`git pull`):

```bash
# Perubahan frontend → build ulang image web
docker compose build web

# Perubahan Go/SQL → build ulang image api
docker compose build api

# Bila ada migrasi baru → terapkan migrasi dulu, lalu jalankan service
docker compose run --rm migrate
docker compose up -d api worker web
```

> **Penting:** service `migrate` memakai image `ingatin:local`, jadi **build `api`
> terlebih dahulu** sebelum menjalankan `migrate` bila ada migrasi baru.

Lakukan **hard-refresh** browser (Ctrl/Cmd+Shift+R) setelah deploy frontend.

---

## Dokumentasi

| Dokumen | Isi |
|---|---|
| [`PLAN.md`](PLAN.md) | Plan master, keputusan terkunci, konvensi agent, daftar fase |
| [`ROADMAP.md`](ROADMAP.md) | Rincian tiap fase yang dikerjakan (F1–F34) |
| [`DEPLOYMENT.md`](DEPLOYMENT.md) | Deploy, env, DB (host & container), nginx/TLS, WAHA/QR, troubleshooting |
| [`OPERATIONS.md`](OPERATIONS.md) | Runbook harian, health, outbox gagal, hardening |
| [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) | Backup & restore database PostgreSQL (postgres, **bukan MySQL**) secara rinci |
| [`DEVELOPMENT.md`](DEVELOPMENT.md) | Setup dev, konvensi kode, cara tambah migrasi/provider/template |

---

## Perintah Umum

```bash
# Status & log
docker compose ps
docker compose logs -f api worker

# Migrasi (idempoten)
docker compose run --rm migrate

# Restart / rebuild
docker compose restart api worker
docker compose up -d --build

# Backup manual (di luar fitur panel)
./scripts/backup.sh

# Verifikasi
./scripts/smoke.sh
```

---

## Struktur Repo

```
ingat.in/
├── README.md  PLAN.md  ROADMAP.md  DEPLOYMENT.md  OPERATIONS.md
├── BACKUP_RESTORE.md  DEVELOPMENT.md  DESIGN.md
├── docker-compose.yml           # 1 image, 6 service (PostgreSQL container)
├── install.sh  uninstall.sh  .env.example  VERSION
├── scripts/                     # provision-db (Mode A), backup, restore, smoke
├── backend/
│   ├── cmd/ingatin/             # entrypoint (-mode api|worker|migrate)
│   └── internal/                # config, crypto, db(+migrations), models,
│                                # repository, auth, api, workitems, notify,
│                                # providers, sla, sheets, bot, attachments,
│                                # backup, kpi, worker
└── frontend/
    └── src/                     # design tokens, components, pages
```

---

## Status

Aplikasi **production-ready** dan telah di-deploy. Seluruh fase **F1–F34 selesai**
(auth & RBAC, provider/WAHA, targets, work items, ticketing & SLA, KPI, lampiran,
Google Sheets, catatan, dark mode, bot Telegram, pembedaan per tim, Notification
Center berbasis izin, hingga **Backup & Restore + auto upload FTP**).

Fase berikutnya yang direncanakan: lihat [`ROADMAP.md`](ROADMAP.md) (mis. Portal
Customer & Multi-Tenant, Reporting & Analytics).
