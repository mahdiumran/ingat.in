# Ingat.in — Deployment Guide

Panduan deploy, konfigurasi, dan pemulihan **Ingat.in** (Go + PostgreSQL + WAHA).

> Baca `PLAN.md` untuk arsitektur dan keputusan desain. Dokumen ini fokus pada operasi deploy.

---

## 1. Prasyarat

### 1.1 Server

| Kebutuhan | Minimum | Catatan |
|---|---|---|
| OS | Debian 12 / Ubuntu 22.04+ | Rujukan: Debian 12 (bookworm) |
| Docker Engine | 24+ | `docker --version` |
| Docker Compose | v2 plugin | `docker compose version` |
| PostgreSQL | — | **Tidak perlu di host** (Mode B memakai container PostgreSQL 16) |
| RAM | 1.5 GB bebas | WAHA/Chromium ±400–800 MB + PostgreSQL + API |
| Disk | 5 GB bebas | image + data + backup + WAHA session |

> Untuk Mode A (reuse PostgreSQL host) diperlukan PostgreSQL 14+ di host dan
> akses `sudo` ke user `postgres`. Lihat §5.2.

### 1.2 Port (default)

| Port | Service | Bind | Keterangan |
|---|---|---|---|
| `8091` | Web UI (nginx) | `0.0.0.0` | Satu-satunya port yang perlu dibuka ke LAN |
| `8081` | API | `127.0.0.1` | Hanya loopback; diakses lewat proxy `web` |
| `8082` | WAHA | `127.0.0.1` | Hanya loopback; QR lewat nginx `:8010` |
| `5432` | PostgreSQL | `127.0.0.1` | Hanya loopback; untuk psql/backup dari host |
| `8010` | QR dashboard WAHA (nginx host) | LAN/Tailscale | Opsional (F3) |

Semua port dapat diubah lewat `.env` (`INGATIN_WEB_PORT`, `INGATIN_API_PORT`,
`INGATIN_WAHA_PORT`, `INGATIN_PG_PORT`). Cek ketersediaan:

```bash
ss -ltnp | grep -E ':(8091|8081|8082|5432|8010)\b' || echo "semua port bebas"
```

---

## 2. Instalasi Cepat (Mode B — PostgreSQL container)

Cara termudah, cocok untuk **server baru tanpa PostgreSQL host**:

```bash
cd /path/ke/ingat.in
./install.sh
```

`install.sh` melakukan:

1. Preflight: Docker & Compose v2, `openssl`, `curl`, cek disk/RAM, dan cek
   ketersediaan port `8091` / `8081` / `8082` / `5432`.
2. Generate `.env` dengan secret acak (mode `600`): `INGATIN_SECRET_KEY`,
   `INGATIN_CREDENTIAL_KEY` (32 byte), password database, `WAHA_API_KEY`,
   kredensial dashboard WAHA. Semua variabel lain (port, retensi, backup,
   lampiran, dll.) ikut ditulis dengan nilai default yang aman.
3. `docker compose build --pull` lalu `docker compose up -d`. PostgreSQL 16
   berjalan sebagai container dengan volume `pgdata` — tidak perlu PostgreSQL
   host.
4. Menunggu PostgreSQL `healthy`, migrasi selesai, dan API sehat.
5. (Opsional) menyiapkan situs nginx host untuk dashboard QR WAHA di port `8010`.
6. Mencetak URL akses dan kredensial admin awal.

Opsi: `--no-build` (lewati build), `--no-qr-site` (jangan sentuh nginx host).

Setelah selesai:

```
Web UI    : http://<IP-SERVER>:8091
API       : http://127.0.0.1:8081/api/health
WAHA      : http://127.0.0.1:8082  (via nginx QR :8010, F3)
```

> Port host dapat diubah lewat `.env` (`INGATIN_WEB_PORT`, `INGATIN_API_PORT`,
> `INGATIN_WAHA_PORT`, `INGATIN_PG_PORT`) bila bentrok dengan service lain.

---

## 3. Instalasi Manual (Langkah per Langkah)

```bash
cd /path/ke/ingat.in

# 1) Siapkan .env
cp .env.example .env
#    WAJIB diganti: INGATIN_SECRET_KEY, INGATIN_CREDENTIAL_KEY,
#    INGATIN_DB_PASSWORD, INGATIN_ADMIN_PASSWORD, WAHA_API_KEY
chmod 600 .env
#    Generate secret acak:
#    openssl rand -hex 32                 -> INGATIN_SECRET_KEY
#    openssl rand -base64 24 | cut -c1-32 -> INGATIN_CREDENTIAL_KEY (tepat 32 byte)
#    openssl rand -hex 20                 -> INGATIN_DB_PASSWORD / WAHA_API_KEY
#    (.env.example pada Mode B sudah memakai host `postgres` dan `waha`.)

# 2) Build & jalankan (PostgreSQL container dibuat otomatis)
docker compose build
docker compose up -d

# 3) Verifikasi
docker compose ps
curl -s http://127.0.0.1:8081/api/health
```

> Tidak perlu `scripts/provision-db.sh` pada Mode B — role & database dibuat
> otomatis oleh image `postgres` dari variabel `POSTGRES_*` di compose.

---

## 4. Environment Variables

Semua variabel memakai prefix `INGATIN_*`. Lihat `.env.example` untuk daftar lengkap.

### 4.1 Wajib

| Variabel | Contoh | Keterangan |
|---|---|---|
| `INGATIN_SECRET_KEY` | `openssl rand -hex 32` | Signing JWT. **Ganti dari default.** |
| `INGATIN_CREDENTIAL_KEY` | tepat **32 karakter** | AES-256-GCM untuk enkripsi API key provider. **Panjang harus 32 byte** atau aplikasi menolak start. |
| `INGATIN_DB_URL` | `postgres://ingatin:***@postgres:5432/ingatin?sslmode=disable` | Mode B: host `postgres`. Mode A: ganti ke `127.0.0.1`. |
| `INGATIN_ADMIN_USERNAME` | `admin` | Dibuat saat bootstrap bila belum ada. |
| `INGATIN_ADMIN_PASSWORD` | kata sandi kuat | Diganti setelah login pertama. |

### 4.2 Umum

| Variabel | Default | Keterangan |
|---|---|---|
| `INGATIN_ADDR` | `127.0.0.1:8081` | Alamat listen API |
| `INGATIN_ACCESS_TOKEN_MINUTES` | `4320` | **72 jam** |
| `INGATIN_REFRESH_TOKEN_DAYS` | `30` | Umur refresh token |
| `INGATIN_SESSION_IDLE_TIMEOUT_MINUTES` | `0` | `0` = nonaktif |
| `INGATIN_TIMEZONE` | `Asia/Jakarta` | Zona tampilan (penyimpanan tetap UTC) |
| `INGATIN_DATA_DIR` | `/app/data` | Log, backup lokal, template |
| `INGATIN_FRONTEND_ORIGIN` | `http://localhost:8091` | CORS |
| `INGATIN_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `INGATIN_MODE` | — | Diisi otomatis oleh compose (`api`/`worker`/`migrate`) |

### 4.3 WAHA (diisi nanti — F3)

| Variabel | Default | Keterangan |
|---|---|---|
| `INGATIN_WAHA_BASE_URL` | `http://waha:3000` | Alamat WAHA (host = service Compose `waha`, port internal 3000) |
| `INGATIN_WAHA_API_KEY` | di-generate | Header `X-Api-Key` |
| `INGATIN_WAHA_QR_URL` | kosong / `http://<ip>:8010` | URL dashboard QR yang ditampilkan di panel |
| `WAHA_API_KEY` | sama dengan di atas | Dibaca container WAHA |
| `WAHA_DASHBOARD_USERNAME` / `_PASSWORD` | di-generate | Login dashboard QR |

### 4.4 Notifikasi (diisi nanti — F4)

Token Telegram dan nomor WhatsApp **tidak** diisi di `.env`; diisi lewat panel
*Providers* dan *Notification Targets* setelah login. Ini memungkinkan perubahan
tanpa rebuild container.

### 4.5 Sinkronisasi Google Spreadsheet (F18)

Hanya interval worker yang ada di `.env`; kredensial & target diisi dari panel:

| Variabel | Default | Keterangan |
|---|---|---|
| `INGATIN_SHEET_SYNC_INTERVAL_SECONDS` | `30` | Interval worker menulis antrean ke spreadsheet |

Spreadsheet ID, nama sheet, dan service account JSON dikelola dari panel
**MANAJEMEN → Google Sheets** (lihat §8.2).

---

## 5. Detail Database

### 5.1 Mode B — PostgreSQL Container (default, direkomendasikan)

`docker-compose.yml` bawaan menjalankan PostgreSQL 16 sebagai container dengan
volume `pgdata`. Semua service berada pada **bridge network milik Compose** dan
saling menjangkau lewat nama service (`postgres`, `waha`, `api`).

| Item | Nilai |
|---|---|
| Image | `postgres:16-alpine` |
| Container | `ingatin-postgres` |
| Data | volume `pgdata` |
| Port host | `127.0.0.1:${INGATIN_PG_PORT:-5432}` (hanya loopback) |
| Kredensial | `INGATIN_DB_NAME` / `INGATIN_DB_USER` / `INGATIN_DB_PASSWORD` |

Role & database dibuat otomatis oleh image `postgres` dari variabel `POSTGRES_*`.
**Tidak perlu** `scripts/provision-db.sh`.

```bash
# Jalankan
docker compose up -d

# Verifikasi dari host (port di-loopback)
PGPASSWORD="$(grep INGATIN_DB_PASSWORD .env | cut -d= -f2-)" \
  psql -h 127.0.0.1 -p "${INGATIN_PG_PORT:-5432}" -U ingatin -d ingatin -c '\dt'

# Bila 5432 sudah dipakai PostgreSQL host, ubah di .env:
#   INGATIN_PG_PORT=5433
```

Keunggulan Mode B: portabel (tanpa ketergantungan PostgreSQL host), tidak
menyentuh `postgresql.conf`/`pg_hba.conf` host, dan versi klien/server selalu
cocok (backup/restore dijalankan di dalam container — lihat §10).

### 5.2 Mode A — Reuse PostgreSQL Host (alternatif)

Gunakan bila server sudah punya PostgreSQL host yang ingin dipakai ulang dan
host tidak dapat diubah `listen_addresses`-nya.

> Catatan: container di bridge network **tidak dapat** menjangkau PostgreSQL host
> yang hanya listen di `127.0.0.1`. Mode A memerlukan `network_mode: host` pada
> `api`/`worker`, yang berarti port API (`8081`) tidak lagi terisolasi dari host.
> **Mode B lebih disarankan.**

Langkah Mode A:

1. Di `.env`:
   ```env
   INGATIN_DB_URL=postgres://ingatin:<PASSWORD>@127.0.0.1:5432/ingatin?sslmode=disable
   ```
2. Provision role & database di host (idempoten):
   ```bash
   ./scripts/provision-db.sh
   ```
3. Sesuaikan `docker-compose.yml` bila perlu (lihat riwayat commit Mode A), lalu
   `docker compose up -d`.

Skema dan migrasi **identik** di kedua mode — tidak ada perubahan kode.

### 5.4 Reset password / volume PostgreSQL (error `28P01`)

Gejala: `migrate` gagal dengan
`FATAL: password authentication failed for user "ingatin" (SQLSTATE 28P01)`.

Penyebab: `POSTGRES_PASSWORD` hanya diterapkan saat volume `pgdata` **pertama
kali dibuat**. Bila volume sudah ada dari percobaan sebelumnya (mis. `.env`
pernah dihapus/di-generate ulang sehingga password berubah), Postgres memakai
password LAMA di dalam volume, bukan yang di `.env`.

Perbaikan (mengembalikan DB container ke password di `.env`):

```bash
# Cara termudah — installer mendeteksi volume lama dan membuat ulang:
./install.sh --reset-db

# Atau manual:
docker compose down
docker volume rm "$(basename "$PWD")_pgdata"   # nama volume = <project>_pgdata
docker compose up -d
```

> ⚠️ `--reset-db` **menghapus seluruh data PostgreSQL container**. Bila data
> perlu dipertahankan, backup dulu (`./scripts/backup.sh`) sebelum reset dan
> restore setelahnya.

`install.sh` mencetak peringatan otomatis bila menemukan volume `pgdata` yang
sudah ada, sehingga kejadian ini mudah dikenali.

### 5.5 Migrasi

Migrasi dijalankan oleh service `migrate` (binary yang sama, `-mode=migrate`) sebelum
`api`/`worker` start (`depends_on: service_completed_successfully`).

```bash
# Status migrasi
docker compose run --rm migrate -mode=migrate   # idempotent, aman diulang

# Riwayat (goose menyimpan di tabel goose_db_version)
docker compose exec postgres \
  psql -U ingatin -d ingatin -c 'SELECT * FROM goose_db_version ORDER BY id'
```

**Aturan:** migrasi yang sudah pernah dijalankan **tidak boleh diedit**. Tambah file baru
`000NN_nama.sql` di `backend/internal/db/migrations/`.

**Master data default (F29):** seluruh master data referensi (kategori tiket,
tag, warna tag, produk, jenis paket, jenis daily task, kategori tim/RFS/reminder,
dsb.) di-*seed* lewat migrasi `00022_master_data_defaults.sql`. Karena itu,
instalasi di server baru cukup menjalankan `migrate` — master data default sudah
terpasang tanpa perlu restore dump. Migrasi bersifat idempoten: di database yang
sudah berjalan tidak ada baris yang ditimpa/diduplikasi.

---

## 6. Docker Compose

Satu image, satu file compose. Enam service (Mode B):

| Service | Image | Network | Port host | Peran |
|---|---|---|---|---|
| `postgres` | `postgres:16-alpine` | bridge | `127.0.0.1:5432` | Database |
| `migrate` | `ingatin:local` | bridge | — | Run-once: `-mode=migrate`, lalu exit 0 |
| `api` | `ingatin:local` | bridge | `127.0.0.1:8081` | HTTP API |
| `worker` | `ingatin:local` | bridge | — | Cron jobs (fanout, sender, digest, retention, sla) |
| `waha` | `devlikeapro/waha` | bridge | `127.0.0.1:8082` | WhatsApp gateway (internal `:3000`) |
| `web` | build `./frontend` | bridge | `0.0.0.0:8091` | nginx SPA + proxy `/api` → `api:8081` |

Volumes: `ingatin_data` (data aplikasi), `waha_sessions` (sesi WhatsApp),
`pgdata` (data PostgreSQL).

Hanya `web` yang bind ke `0.0.0.0`; seluruh port lain hanya loopback.

Perintah umum:

```bash
docker compose ps                     # status
docker compose logs -f api worker     # log streaming
docker compose up -d --build          # rebuild + restart
docker compose down                   # stop (data tetap)
docker compose down -v                # stop + hapus volume (DESTRUKTIF)
```

---

## 7. Nginx & TLS

### 7.1 Reverse Proxy Eksternal (opsional)

Untuk HTTPS atau nama domain, tambahkan site nginx host:

```nginx
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name ingat.example.com;

    ssl_certificate     /etc/nginx/ssl/ingat.crt;
    ssl_certificate_key /etc/nginx/ssl/ingat.key;
    ssl_protocols       TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:8091;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Aktifkan: `ln -s /etc/nginx/sites-available/ingat /etc/nginx/sites-enabled/ && nginx -t && systemctl reload nginx`

Jika memakai TLS, set `INGATIN_FRONTEND_ORIGIN` ke URL HTTPS tersebut.

### 7.2 Nginx QR WAHA (`:8010`) — F3

Halaman scan QR WAHA diekspos hanya ke LAN/Tailscale dengan basic-auth:

```nginx
server {
    listen 8010;
    server_name _;

    auth_basic           "Ingat.in WAHA";
    auth_basic_user_file /etc/nginx/.htpasswd-ingatin;

    location / {
        proxy_pass http://127.0.0.1:8082;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
    }
}
```

Buat kredensial: `htpasswd -c /etc/nginx/.htpasswd-ingatin noc`

---

## 8. WAHA — Setup & QR

1. Pastikan service `waha` jalan: `docker compose ps waha`.
2. Buka dashboard QR: `http://<IP-LAN>:8010` (F3) atau langsung `http://127.0.0.1:8082`
   dari server (tunnel SSH bila perlu).
3. Login dengan `WAHA_DASHBOARD_USERNAME` / `WAHA_DASHBOARD_PASSWORD` dari `.env`.
4. Start session **default**, lalu scan QR dengan WhatsApp di ponsel pengirim
   (Perangkat Tertaut → Tautkan Perangkat).
5. Tunggu status menjadi `WORKING`.
6. Di panel Ingat.in → *Providers*, klik **Test Koneksi**, lalu **Test Kirim** ke nomor uji.
   (Provider WAHA dibuat otomatis dari `INGATIN_WAHA_BASE_URL` +
   `INGATIN_WAHA_API_KEY` saat pertama dijalankan.)

### 8.1 Troubleshooting WAHA (temuan lapangan)

**A. Dashboard `:8010` selalu 401 walau password benar.**
Berkas `/etc/nginx/.htpasswd-ingatin` masih memakai hash password **lama**.
`nginx` tidak membaca `.env`, jadi mengganti `WAHA_DASHBOARD_PASSWORD` di `.env`
tidak otomatis memperbarui basic auth. Perbaiki dengan regenerasi:

```bash
PASS="$(grep '^WAHA_DASHBOARD_PASSWORD=' .env | cut -d= -f2-)"
htpasswd -cB -b /etc/nginx/.htpasswd-ingatin noc "$PASS"
chown root:www-data /etc/nginx/.htpasswd-ingatin && chmod 640 /etc/nginx/.htpasswd-ingatin
nginx -t && systemctl reload nginx
```

Jalankan ulang `install.sh` juga memperbaikinya (§4b) karena skrip kini
menyinkronkan htpasswd setiap kali dijalankan.

**B. Panel melaporkan "WAHA menolak API key (401)".**
Ada **dua** variabel dan keduanya harus bernilai sama:
`WAHA_API_KEY` (dipakai container WAHA) dan `INGATIN_WAHA_API_KEY` (dipakai
aplikasi saat memanggil WAHA). Bukti cepat:

```bash
KEY="$(grep '^WAHA_API_KEY=' .env | cut -d= -f2-)"
curl -s -o /dev/null -w '%{http_code}\n' -H "X-Api-Key: $KEY" http://127.0.0.1:8082/api/sessions
# 200 = kunci benar
```

Bila berbeda, samakan `INGATIN_WAHA_API_KEY` dengan `WAHA_API_KEY` lalu
`docker compose up -d api worker`.

**C. "sesi default tidak ditemukan".**
Sesi memang belum dibuat. Buka dashboard QR dan klik **Start**, atau dari panel
*Providers* → **Mulai Sesi** (idempoten: membuat sesi bila belum ada).

**Hemat memori (opsional):** set `WHATSAPP_DEFAULT_ENGINE=GOWS` pada service `waha`
untuk memakai engine Go (tanpa Chromium). Catat: fitur tertentu (mis. kirim media) bisa berbeda.

**Sesi hilang / logout:** scan ulang QR. Sesi tersimpan di volume `waha_sessions`;
`docker compose down` tidak menghapusnya.

---

## 8.2 Google Sheets — Sinkronisasi Todo & Daily Task (F18)

Setiap **Todo Task** (`item_type=task`) dan **Daily Task** (`item_type=daily_task`)
yang dibuat NOC otomatis dicatat sebagai satu baris di Google Spreadsheet;
perubahan status diperbarui in-place (dicocokkan lewat kolom **Ref**). Task yang
dihapus ditandai **Dihapus** (baris tidak dibuang, riwayat tetap utuh).
Konfigurasi **tidak** disimpan di `.env` — semuanya diisi dari panel admin.

### Langkah setup

1. **Buat project & service account** di [Google Cloud Console](https://console.cloud.google.com/):
   - Buat project (atau pakai yang ada) → *APIs & Services* → aktifkan
     **Google Sheets API**.
   - *IAM & Admin* → *Service Accounts* → **Create service account**.
   - Buka service account → tab *Keys* → **Add key → Create new key → JSON**.
     Unduh berkas JSON-nya.
2. **Siapkan spreadsheet**: buat Google Sheet, lalu **Share** ke alamat
   `client_email` dari berkas JSON tersebut dengan akses **Editor**.
   *(Tanpa langkah ini, sinkronisasi gagal 403.)*
3. **Isi panel**: login sebagai admin → **MANAJEMEN → Google Sheets**:
   - **Spreadsheet ID** — dari URL:
     `docs.google.com/spreadsheets/d/<ID>/edit`
   - **Nama Sheet (tab)** — nama tab tujuan (default `Todo`). Boleh diubah kapan
     saja; karakter `[ ] * ? / \ :` tidak diizinkan.
   - **Service Account JSON** — tempel seluruh isi berkas JSON.
   - Simpan, lalu klik **Buat Sheet Tab** (membuat tab + header) dan
     **Uji Koneksi** (menulis satu baris `TEST-…`).
   - Aktifkan toggle **Aktif**.
4. Buat sebuah Todo Task dari menu *Todo Tasks*. Dalam ≤ 30 detik barisnya muncul
   di spreadsheet. Ubah statusnya → kolom **Status** dan **Diperbarui** ikut berubah
   pada baris yang sama.

### Kolom yang ditulis

| A | B | C | D | E | F | G | H | I | J | K | L | M | N | O |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Ref | Tipe | Judul | Deskripsi | Prioritas | Status | Owner | Dibuat Oleh | Diperbarui Oleh | Device | Tags | Due (WIB) | Dibuat (WIB) | Diperbarui (WIB) | Keterangan |

- **Tipe** bernilai `Todo` atau `Daily Task`.
- **Diperbarui Oleh** username operator yang terakhir mengubah item.
- **Keterangan** diisi operator saat menandai Daily Task **Selesai**.
- **Status** memakai label yang enak dibaca: `Accepted`/`On Progress`/`Expired`/
  `Canceled`/`Closed` (Task) dan `Belum Selesai`/`Selesai` (Daily Task), serta
  `Dihapus` untuk item yang dihapus.
- Untuk **Daily Task**, **Due (WIB)** berisi tanggal harian (23:59 WIB).
- Bila jumlah/urutan kolom berubah antar versi, header ditulis ulang otomatis
  (self-healing) saat worker menyinkronkan atau tombol **Buat Sheet Tab** diklik.

### Catatan operasional

- **Hanya item baru** yang disinkronkan (tidak ada backfill data lama).
- Antrean disimpan di DB (`sheet_sync_queue`): aman restart & internet mati;
  gagal → backoff 1/2/5/15/60 menit, maksimum 5 percobaan lalu `failed`.
- Interval worker diatur `INGATIN_SHEET_SYNC_INTERVAL_SECONDS` (default 30).
- Kolom **Ref** adalah kunci pencocokan; jangan diubah manual di spreadsheet.

### Troubleshooting

| Gejala | Sebab & solusi |
|---|---|
| Error `akses ditolak (403)` | Spreadsheet belum dibagikan ke `client_email` sebagai **Editor** |
| Error `tidak ditemukan (404)` | `Spreadsheet ID` salah, atau tab belum dibuat → klik **Buat Sheet Tab** |
| Error `kuota API terlampaui (429)` | Kuota Google Sheets (300 req/menit); tunggu, worker mencoba ulang otomatis |
| Pesan `service account JSON tidak valid` | JSON terpotong saat ditempel — tempel ulang utuh |
| Pesan `nama sheet tidak boleh memuat karakter "X"` | Ganti nama tab tanpa `[ ] * ? / \ :` |

Cek antrean:

```bash
docker compose exec postgres psql -U ingatin -d ingatin -c \
  "SELECT status, count(*) FROM sheet_sync_queue GROUP BY status;"
```

---

## 9. Verifikasi Pasca-Deploy

```bash
# 1) Container
docker compose ps

# 2) Migrasi
docker compose exec postgres psql -U ingatin -d ingatin -c 'SELECT count(*) FROM goose_db_version'

# 3) Tabel inti ada
docker compose exec postgres psql -U ingatin -d ingatin -c '\dt' | grep -E 'work_items|notification_outbox|users'

# 4) Seed lengkap
docker compose exec postgres psql -U ingatin -d ingatin -c \
  'SELECT (SELECT count(*) FROM organizations) org,
          (SELECT count(*) FROM teams) teams,
          (SELECT count(*) FROM workflow_definitions) workflows,
          (SELECT count(*) FROM escalation_policies) policies,
          (SELECT count(*) FROM notification_targets) targets'

# 5) API
curl -s http://127.0.0.1:8081/api/health
curl -s http://127.0.0.1:8081/api/version

# 6) Web + proxy
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8091
curl -s http://127.0.0.1:8091/api/health

# 7) Smoke test lengkap
./scripts/smoke.sh
```

---

## 10. Backup & Restore

> Rangkuman di sini; panduan lengkap ada di [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md)
> (PostgreSQL — bukan MySQL).

### 10.1 Backup

```bash
./scripts/backup.sh
```

Pada Mode B, `pg_dump` dijalankan **di dalam container PostgreSQL** (versi klien
& server selalu cocok; `postgresql-client` host tidak diperlukan). Menghasilkan
`backups/ingatin-db-YYYYMMDD-HHMMSS.sql.gz` + `.sha256`, retensi default 14 hari
(dapat diubah via `KEEP_DAYS`).

Jadwalkan harian 02:00 WIB di host:

```cron
0 2 * * * cd /path/ke/ingat.in && ./scripts/backup.sh >> /var/log/ingatin-backup.log 2>&1
```

### 10.2 Restore

```bash
./scripts/restore.sh backups/ingatin-db-20260101-020000.sql.gz
```

Script memverifikasi checksum, membuat backup pengaman sebelum menimpa,
mengosongkan schema `public`, lalu restore. **Hentikan `api` dan `worker`
lebih dulu:**

```bash
docker compose stop api worker
./scripts/restore.sh <file>
docker compose start api worker
```

### 10.3 Uji Restore Berkala

Lakukan minimal tiap kuartal ke database sementara (`ingatin_restore_test`) untuk
memastikan backup benar-benar dapat dipulihkan.

---

## 11. Upgrade & Rollback

```bash
# Upgrade
cd /path/ke/ingat.in
./scripts/backup.sh              # backup dulu (WAJIB)
git pull                          # atau ganti source
docker compose build
docker compose up -d              # migrate berjalan otomatis
docker compose ps

# Rollback (kode lama + migrasi turun manual bila perlu)
docker compose down
git checkout <tag-sebelumnya>
docker compose build && docker compose up -d
# Bila skema berubah dan perlu turun: goose down (hati-hati, destruktif)
```

---

## 12. Troubleshooting

| Gejala | Kemungkinan penyebab | Tindakan |
|---|---|---|
| `api` restart terus, log `config: invalid` | `INGATIN_CREDENTIAL_KEY` bukan 32 byte | Perbaiki `.env`; panjang tepat 32 |
| `api` gagal konek `postgres:5432` | container `postgres` belum sehat / `INGATIN_DB_URL` host salah | `docker compose ps`; host harus `postgres` (Mode B) atau `127.0.0.1` (Mode A) |
| Port `5432`/`8081`/`8082` bentrok | service lain memakai port sama | Ubah `INGATIN_PG_PORT`/`INGATIN_API_PORT`/`INGATIN_WAHA_PORT` di `.env`, lalu `docker compose up -d` |
| `password authentication failed for user "ingatin"` (SQLSTATE `28P01`) | `pgdata` lama dibuat dengan password berbeda; `POSTGRES_PASSWORD` hanya berlaku saat volume pertama kali dibuat | Jalankan `./install.sh --reset-db` (menghapus volume `pgdata` lalu membuat ulang DB dari `.env`). **Destruktif** untuk data container. |
| `migrate` gagal `permission denied for schema public` | role bukan pemilik DB (Mode A) | `sudo -u postgres psql -c 'ALTER DATABASE ingatin OWNER TO ingatin'` |
| Web `502 Bad Gateway` | `api` belum sehat | `docker compose logs api`; cek `docker compose ps` |
| Web terbuka tapi `/api` 404 | upstream nginx salah | Pastikan `INGATIN_API_UPSTREAM=http://api:8081` (Mode B) |
| WAHA `SCAN_QR_CODE` terus | belum scan / sesi kedaluwarsa | Scan ulang di `:8010` |
| WAHA `FAILED` | versi WhatsApp Web tak cocok | `docker compose restart waha`; bila tetap, ganti tag image |
| Notifikasi tidak terkirim | outbox `failed` | Cek Audit → Outbox, baca `last_error`; verifikasi provider & binding |
| Backup gagal `pg_dump: server version mismatch` | memakai pg_dump host yang lebih lama | Jalankan `./scripts/backup.sh` (otomatis memakai container di Mode B) |

Kumpulkan diagnosa:

```bash
docker compose ps
docker compose logs --tail=200 api worker waha
curl -s http://127.0.0.1:8081/api/health
```

---

## 13. Checklist Go-Live

- [ ] `.env` di-generate installer, mode `600`, semua secret acak (bukan default)
- [ ] `INGATIN_ADMIN_PASSWORD` diganti setelah login pertama
- [ ] Migrasi selesai (`goose_db_version` ada baris terbaru)
- [ ] Seed lengkap (org, teams, workflows, policies, target)
- [ ] `/api/health` OK dari host **dan** lewat `:8091`
- [ ] Backup harian terjadwal + uji restore pernah berhasil
- [ ] Notification target terverifikasi (kirim test lulus)
- [ ] Provider WA/Telegram aktif & teruji
- [ ] Sesi WAHA `WORKING` + dipantau di Dashboard
- [ ] Restart server dites: semua service naik otomatis (`restart: unless-stopped`)
- [ ] Hardening `pg_hba` dipertimbangkan (`OPERATIONS.md`)

---

## 8.3 SLA, KPI & Lampiran (F20/F21)

### Lampiran pendukung
- Batas ukuran per berkas: `INGATIN_ATTACHMENTS_MAX_MB` (default **50**).
- Jenis diizinkan: `INGATIN_ATTACHMENTS_ALLOWED_TYPES` (default gambar, PDF,
  teks/CSV, zip/gzip).
- Berkas disimpan pada volume `ingatin_data` di `/app/data/attachments/…`;
  unduhan melalui API ber-auth (`GET /api/attachments/{id}`).
- **Backup** volume `ingatin_data` sudah mencakup lampiran — pastikan skrip
  backup menyertakannya.

### KPI & SLA
- Siklus SLA per tiket (`ticket_sla_cycles`), policy per prioritas, kalender 24 jam.
- Halaman **KPI & SLA** (menu OPERASIONAL): ringkasan, 6 grafik, tabel per person,
  ekspor CSV, dan tombol ekspor Google Sheets.
- Skor KPI = `0.5·respons tepat waktu% + 0.5·penyelesaian tepat waktu%`.
- SLA tiap reopen dihitung sebagai SLA penyelesaian (tanpa metrik terpisah).
