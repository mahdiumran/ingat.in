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
| PostgreSQL | 14+ (host atau container) | Server ini: PostgreSQL **15.19** host |
| `sudo` akses ke user `postgres` | — | Untuk provisioning role & DB (mode reuse host) |
| RAM | 1.5 GB bebas | WAHA/Chromium butuh ±400–800 MB |
| Disk | 5 GB bebas | DB + backup + WAHA session |

### 1.2 Port (default)

| Port | Service | Bind | Wajib bebas |
|---|---|---|---|
| `8091` | Web UI | `0.0.0.0` | Ya |
| `8081` | API | `127.0.0.1` | Ya |
| `8082` | WAHA | `127.0.0.1` | Ya |
| `8010` | QR dashboard WAHA (nginx) | LAN/Tailscale | F3 |

Cek ketersediaan:

```bash
ss -ltnp | grep -E ':(8091|8081|8082|8010)\b' || echo "semua port bebas"
```

---

## 2. Instalasi Cepat (Mode Reuse PostgreSQL Host)

Mode ini dipakai di server ini: PostgreSQL host yang sudah ada dipakai ulang.

```bash
cd /root/opencode/remindersys
./install.sh
```

`install.sh` melakukan:

1. Cek Docker & Docker Compose.
2. Generate `.env` dengan secret acak (mode `600`):
   `INGATIN_SECRET_KEY`, `INGATIN_CREDENTIAL_KEY` (32 byte), password role `ingatin`,
   `WAHA_API_KEY`, kredensial dashboard WAHA.
3. Provision role `ingatin` + database `ingatin` di PostgreSQL host (via `sudo -u postgres`).
4. `docker compose build` lalu `docker compose up -d`.
5. Menjalankan migrasi (service `migrate`) dan menunggu `api`/`worker` sehat.
6. Mencetak URL akses dan kredensial admin awal.

Setelah selesai:

```
Web UI    : http://<IP-SERVER>:8091
API       : http://127.0.0.1:8081/api/health
WAHA      : http://127.0.0.1:8082  (via nginx QR :8010, F3)
```

---

## 3. Instalasi Manual (Langkah per Langkah)

```bash
cd /root/opencode/remindersys

# 1) Siapkan .env
cp .env.example .env
#    WAJIB diganti: INGATIN_SECRET_KEY, INGATIN_CREDENTIAL_KEY,
#    INGATIN_DB_PASSWORD, INGATIN_ADMIN_PASSWORD, WAHA_API_KEY
chmod 600 .env
#    Generate secret acak:
#    openssl rand -hex 32                 -> INGATIN_SECRET_KEY
#    openssl rand -base64 24 | cut -c1-32 -> INGATIN_CREDENTIAL_KEY (tepat 32 byte)
#    openssl rand -hex 20                 -> INGATIN_DB_PASSWORD / WAHA_API_KEY

# 2) Provision database (reuse host PG)
./scripts/provision-db.sh

# 3) Build & jalankan
docker compose build
docker compose up -d

# 4) Verifikasi
docker compose ps
curl -s http://127.0.0.1:8081/api/health
```

---

## 4. Environment Variables

Semua variabel memakai prefix `INGATIN_*`. Lihat `.env.example` untuk daftar lengkap.

### 4.1 Wajib

| Variabel | Contoh | Keterangan |
|---|---|---|
| `INGATIN_SECRET_KEY` | `openssl rand -hex 32` | Signing JWT. **Ganti dari default.** |
| `INGATIN_CREDENTIAL_KEY` | tepat **32 karakter** | AES-256-GCM untuk enkripsi API key provider. **Panjang harus 32 byte** atau aplikasi menolak start. |
| `INGATIN_DB_URL` | `postgres://ingatin:***@127.0.0.1:5432/ingatin?sslmode=disable` | Mode host. Lihat §6 untuk mode container. |
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
| `INGATIN_WAHA_BASE_URL` | `http://127.0.0.1:8082` | Alamat WAHA |
| `INGATIN_WAHA_API_KEY` | di-generate | Header `X-Api-Key` |
| `WAHA_API_KEY` | sama dengan di atas | Dibaca container WAHA |
| `WAHA_DASHBOARD_USERNAME` / `_PASSWORD` | di-generate | Login dashboard QR |

### 4.4 Notifikasi (diisi nanti — F4)

Token Telegram dan nomor WhatsApp **tidak** diisi di `.env`; diisi lewat panel
*Providers* dan *Notification Targets* setelah login. Ini memungkinkan perubahan
tanpa rebuild container.

---

## 5. Detail Database

### 5.1 Mode A — Reuse PostgreSQL Host (dipakai sekarang)

Karakteristik PG host di server ini (terverifikasi):

| Item | Nilai |
|---|---|
| Versi | PostgreSQL 15.19 (Debian) |
| Cluster | `15/main`, port `5432`, unix socket `/var/run/postgresql` |
| `listen_addresses` | `localhost` (hanya `127.0.0.1` + `::1`) |
| `pg_hba.conf` | `host all all 127.0.0.1/32 trust`; `::1/128 scram-sha-256`; `local all all peer` |
| Database existing | `juniper_manage`, `juniper_manage_test`, `mcnvpn`, `postgres` |
| `max_connections` | 100 |

Karena `listen_addresses=localhost`, container **di bridge network tidak bisa**
menghubungi `172.17.0.1:5432`. Solusi yang dipakai: service `api`, `worker`, dan `waha`
memakai **`network_mode: host`**, sehingga menghubungi `127.0.0.1:5432` langsung —
**tanpa mengubah konfigurasi PostgreSQL host** (aman untuk `juniper_manage` dan `mcnvpn`
yang sedang produksi).

Service `web` (nginx) tetap di bridge network dan meneruskan `/api` ke `127.0.0.1:8081`.

```bash
# Provision manual (idempoten)
./scripts/provision-db.sh

# Verifikasi
psql -h 127.0.0.1 -U ingatin -d ingatin -c '\dt'
```

### 5.2 Mode B — PostgreSQL Container (untuk server baru)

`docker-compose.yml` sudah memuat blok `postgres` **dalam keadaan di-comment**.
Untuk mengaktifkan:

1. Buka `docker-compose.yml`, hapus komentar pada service `postgres` (dan volume `pgdata`).
2. Pada service `api` dan `worker`:
   - **Hapus** baris `network_mode: host`.
   - Tambahkan `depends_on: postgres: {condition: service_healthy}`.
   - Ubah port API menjadi pemetaan: `ports: ["127.0.0.1:8081:8081"]`.
3. Pada `waha`: hapus `network_mode: host`, tambahkan `ports: ["127.0.0.1:8082:3000"]`,
   dan ubah `INGATIN_WAHA_BASE_URL` / `WHATSAPP_API_URL` menjadi `http://waha:3000`.
4. Ubah `.env`:
   ```env
   INGATIN_DB_URL=postgres://ingatin:<PASSWORD>@postgres:5432/ingatin?sslmode=disable
   ```
5. Jalankan:
   ```bash
   docker compose up -d --build
   ```

Skema dan migrasi **identik** di kedua mode — tidak ada perubahan kode.

### 5.3 Migrasi

Migrasi dijalankan oleh service `migrate` (binary yang sama, `-mode=migrate`) sebelum
`api`/`worker` start (`depends_on: service_completed_successfully`).

```bash
# Status migrasi
docker compose run --rm migrate -mode=migrate   # idempotent, aman diulang

# Riwayat (goose menyimpan di tabel goose_db_version)
psql -h 127.0.0.1 -U ingatin -d ingatin -c 'SELECT * FROM goose_db_version ORDER BY id'
```

**Aturan:** migrasi yang sudah pernah dijalankan **tidak boleh diedit**. Tambah file baru
`000NN_nama.sql` di `backend/internal/db/migrations/`.

---

## 6. Docker Compose

Satu image, satu file compose. Lima service:

| Service | Image | Network | Peran |
|---|---|---|---|
| `migrate` | `ingatin:local` | host | Run-once: `-mode=migrate`, lalu exit 0 |
| `api` | `ingatin:local` | host | HTTP API `:8081` |
| `worker` | `ingatin:local` | host | Cron jobs (fanout, sender, digest, retention, sla) |
| `waha` | `devlikeapro/waha` | host | WhatsApp gateway `:8082` |
| `web` | build `./frontend` | bridge | nginx SPA + proxy `/api` → `127.0.0.1:8081`, port `8091` |

Volumes: `ingatin_data` (data aplikasi), `waha_sessions` (sesi WhatsApp),
`pgdata` (hanya bila Mode B aktif).

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

## 9. Verifikasi Pasca-Deploy

```bash
# 1) Container
docker compose ps

# 2) Migrasi
psql -h 127.0.0.1 -U ingatin -d ingatin -c 'SELECT count(*) FROM goose_db_version'

# 3) Tabel inti ada
psql -h 127.0.0.1 -U ingatin -d ingatin -c '\dt' | grep -E 'work_items|notification_outbox|users'

# 4) Seed lengkap
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
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

### 10.1 Backup

```bash
./scripts/backup.sh
```

Menghasilkan `backups/ingatin-db-YYYYMMDD-HHMMSS.sql.gz` + `.sha256`,
retensi default 14 hari (dapat diubah via `KEEP_DAYS`).

Jadwalkan harian 02:00 WIB di host:

```cron
0 2 * * * cd /root/opencode/remindersys && ./scripts/backup.sh >> /var/log/ingatin-backup.log 2>&1
```

### 10.2 Restore

```bash
./scripts/restore.sh backups/ingatin-db-20260101-020000.sql.gz
```

Script memverifikasi checksum, membuat backup pengaman sebelum menimpa,
lalu restore. **Hentikan `api` dan `worker` lebih dulu:**

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
cd /root/opencode/remindersys
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
| `connection refused` ke `127.0.0.1:5432` dari `api` | `network_mode: host` hilang | Pastikan `api`/`worker` memakai host network (Mode A) |
| `password authentication failed for user "ingatin"` | Role belum dibuat / password beda | Jalankan `./scripts/provision-db.sh`; sinkronkan `INGATIN_DB_URL` |
| `migrate` gagal `permission denied for schema public` | Role bukan pemilik DB | `sudo -u postgres psql -c 'ALTER DATABASE ingatin OWNER TO ingatin'` |
| Web `502 Bad Gateway` | `api` belum sehat | `docker compose logs api`; cek `docker compose ps` |
| Web terbuka tapi `/api` 404 | proxy nginx salah | Cek `frontend/nginx/default.conf` mengarah ke `127.0.0.1:8081` |
| WAHA `SCAN_QR_CODE` terus | belum scan / sesi kedaluwarsa | Scan ulang di `:8010` |
| WAHA `FAILED` | versi WhatsApp Web tak cocok | `docker compose restart waha`; bila tetap, ganti tag image |
| Notifikasi tidak terkirim | outbox `failed` | Cek Audit → Outbox, baca `last_error`; verifikasi provider & binding |
| Backup gagal `pg_dump: command not found` | image runtime tanpa client | Pastikan Dockerfile runtime `apk add postgresql-client gzip` |

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
