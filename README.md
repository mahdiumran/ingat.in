# Ingat.in

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
| **Daily Task** | Checklist harian bergaya todo list (papan 3 kolom) dengan carry-over task belum selesai |
| **Ticketing** | Insiden/permintaan/change dengan SLA otomatis (first response → closed) |
| **Reminder** | Reminder dengan titik peringatan berjenjang (H-7/H-3/H-2/H-1/H-0) + eskalasi LATE |
| **RFS** | Data RFS diisi admin sales; NOC otomatis diingatkan saat tanggal RFS mendekat |
| **Notifikasi** | Telegram + WhatsApp; target berupa grup/tim atau personal; template bisa diedit |
| **Provider WA** | WAHA (bawaan), atau Fonnte/Wablas/Starsender/custom HTTP — bisa diganti dari panel |
| **Google Sheets** | Todo Task & Daily Task otomatis tercatat ke spreadsheet (service account, antrean tahan gangguan) |
| **KPI & SLA** | Siklus SLA per tiket, penanganan (owner + ikut menangani), KPI per person dengan grafik |
| **Lampiran & Penanganan** | Lampiran pendukung (50 MB) + catatan Issue/Troubleshooting/Solusi pada tiket |
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
- **Satu image, satu `docker-compose.yml`**

---

## Mulai Cepat

```bash
cd /root/opencode/remindersys
./install.sh
```

Akses:

```
Web UI   : http://<IP-SERVER>:8091
API      : http://127.0.0.1:8081/api/health
```

Kredensial admin awal tercetak oleh installer (atau lihat `.env`).

---

## Dokumentasi

| Dokumen | Isi |
|---|---|
| [`PLAN.md`](PLAN.md) | Plan master, keputusan terkunci, konvensi agent, fase F1–F15 |
| [`DEPLOYMENT.md`](DEPLOYMENT.md) | Deploy, env, DB (host & container), nginx/TLS, WAHA/QR, troubleshooting |
| [`OPERATIONS.md`](OPERATIONS.md) | Runbook harian, health, outbox gagal, backup, hardening |
| [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) | Backup & restore database PostgreSQL (postgres, **bukan MySQL**) secara rinci |
| [`DEVELOPMENT.md`](DEVELOPMENT.md) | Setup dev, konvensi kode, cara tambah migrasi/provider/template |
| [`ROADMAP.md`](ROADMAP.md) | Rencana F10–F15 (ticketing, SLA, inbound, reporting) |

---

## Perintah Umum

```bash
# Status & log
docker compose ps
docker compose logs -f api worker

# Migrasi (idempoten)
docker compose run --rm migrate -mode=migrate

# Restart / rebuild
docker compose restart api worker
docker compose up -d --build

# Verifikasi
./scripts/smoke.sh
```

---

## Struktur Repo

```
remindersys/
├── PLAN.md  DEPLOYMENT.md  OPERATIONS.md  DEVELOPMENT.md  ROADMAP.md
├── docker-compose.yml           # 1 image, 5 service
├── install.sh  uninstall.sh  .env.example  VERSION
├── scripts/                     # provision-db, backup, restore, smoke
├── backend/
│   ├── cmd/ingatin/             # entrypoint (-mode api|worker|migrate)
│   └── internal/                # config, crypto, db, models, repository,
│                                # auth, api, workitems, notify, providers, sla, backup, worker
└── frontend/
    └── src/                     # design tokens, components, pages
```

---

## Status

**F1 selesai:** infrastruktur, skema database lengkap (termasuk siap-ticketing & SLA),
seed data, dan dokumentasi.

Fase berikutnya: F2 auth & RBAC → F3 provider + WAHA → F4 targets → F5 work items → …

Lihat `PLAN.md` §8 untuk daftar fase lengkap.
