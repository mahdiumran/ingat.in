# Ingat.in — Operations Runbook

Panduan operasi harian untuk operator NOC/admin.

---

## 1. Cek Kesehatan Cepat

```bash
cd /path/ke/ingat.in

# Semua service
docker compose ps

# API
curl -s http://127.0.0.1:8081/api/health
curl -s http://127.0.0.1:8081/api/version

# Web + proxy
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8091
curl -s http://127.0.0.1:8091/api/health

# Smoke test
./scripts/smoke.sh
```

Interpretasi `docker compose ps`:

| Status | Arti |
|---|---|
| `running` / `healthy` | Normal |
| `restarting` | Crash loop — lihat log |
| `exited (0)` | Normal **hanya** untuk `migrate` |
| `exited (non-0)` | Gagal — lihat log |

---

## 2. Log

```bash
docker compose logs --tail=100 api
docker compose logs --tail=100 worker
docker compose logs --tail=100 waha
docker compose logs -f api worker          # streaming
docker compose logs --since 30m api        # rentang waktu
```

Untuk log aplikasi persisten, lihat `INGATIN_DATA_DIR` (volume `ingatin_data`).

---

## 3. Database

```bash
# Koneksi
psql -h 127.0.0.1 -U ingatin -d ingatin

# Ukuran DB & tabel terbesar
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) AS size
   FROM pg_catalog.pg_statio_user_tables ORDER BY pg_total_relation_size(relid) DESC LIMIT 15"

# Koneksi aktif
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT count(*), state FROM pg_stat_activity WHERE datname='ingatin' GROUP BY state"

# Versi migrasi
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  'SELECT version_id, is_applied FROM goose_db_version ORDER BY id DESC LIMIT 5'
```

Bila tabel `work_item_events` membesar, pantau:

```bash
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT date_trunc('month', created_at) AS bulan, count(*)
   FROM work_item_events GROUP BY 1 ORDER BY 1 DESC LIMIT 12"
```

---

## 4. Notification Outbox

Tabel `notification_outbox` adalah sumber kebenaran pengiriman.

```bash
# Ringkasan status
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT status, count(*) FROM notification_outbox GROUP BY status ORDER BY 2 DESC"

# Pengiriman gagal terakhir (baca last_error)
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT id, event_key, channel, attempts, left(last_error, 120) AS err, created_at
   FROM notification_outbox WHERE status='failed'
   ORDER BY created_at DESC LIMIT 20"

# Yang masih menunggu (pending) — perhatikan next_attempt_at
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT id, event_key, channel, attempts, next_attempt_at
   FROM notification_outbox WHERE status='pending'
   ORDER BY next_attempt_at LIMIT 20"
```

Tindakan umum:

| Kondisi | Tindakan |
|---|---|
| Banyak `failed` pada satu channel | Periksa provider (dashboard *Providers* → Test Koneksi) |
| `failed` karena sesi WA logout | Scan ulang QR WAHA |
| Notifikasi tertahan (quiet hours) | Normal untuk `info`/`warning`; `critical` selalu dikirim |
| Outbox menumpuk `pending` | Pastikan service `worker` jalan (`docker compose ps worker`) |

Retry manual sebuah outbox (setelah provider diperbaiki):

```bash
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "UPDATE notification_outbox
   SET status='pending', attempts=0, next_attempt_at=now(), last_error=NULL
   WHERE id=<ID>"
```

Retry massal untuk satu channel yang sempat down:

```bash
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "UPDATE notification_outbox
   SET status='pending', attempts=0, next_attempt_at=now()
   WHERE status='failed' AND channel='whatsapp' AND created_at > now() - interval '24 hours'"
```

> Perhatikan: `event_key` bersifat UNIQUE, jadi tidak akan terjadi duplikat **antar event**.
> Retry manual akan mengirim ulang event yang sama (disengaja).

---

## 4.1 Sinkronisasi Google Spreadsheet (F18)

Tabel `sheet_sync_queue` menyimpan antrean penulisan Todo Task ke spreadsheet.

```bash
# Ringkasan status antrean
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT status, count(*) FROM sheet_sync_queue GROUP BY status ORDER BY 2 DESC"

# Entri gagal terakhir
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT id, ref_no, op, action, attempts, left(last_error, 120) AS err, created_at
   FROM sheet_sync_queue WHERE status='failed'
   ORDER BY created_at DESC LIMIT 20"

# Kondisi sinkronisasi terakhir
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT enabled, spreadsheet_id, sheet_name, header_written, last_sync_at, last_error
   FROM sheet_sync_config"
```

| Kondisi | Tindakan |
|---|---|
| `last_error` memuat `(403)` | Bagikan spreadsheet ke `client_email` (Editor) di panel *Google Sheets* |
| `last_error` memuat `(404)` | Periksa `Spreadsheet ID`; klik **Buat Sheet Tab** |
| `last_error` memuat `(429)` | Kuota; worker mencoba ulang otomatis, tidak perlu tindakan |
| Antrean menumpuk `pending` | Pastikan service `worker` jalan; periksa toggle **Aktif** |
| Ingin berhenti sementara | Matikan toggle **Aktif** — antrean tetap tersimpan, tidak dihabiskan |

Retry manual satu entri (setelah masalah diperbaiki):

```bash
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "UPDATE sheet_sync_queue
   SET status='pending', attempts=0, next_attempt_at=now(), last_error=''
   WHERE id=<ID>"
```

> Entri unik per work item (`event_key=sheet:<id>`) — satu Todo Task selalu
> menempati satu baris di spreadsheet. Perubahan status menimpa baris itu, bukan
> menambah baris baru.

---

## 5. Sesi & Autentikasi

JWT access berlaku **72 jam**; refresh token 30 hari dan tersimpan sebagai hash.

```bash
# Sesi aktif per user
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT u.username, count(*) AS sesi, max(r.expires_at) AS exp_terjauh
   FROM refresh_tokens r JOIN users u ON u.id=r.user_id
   WHERE r.revoked_at IS NULL AND r.expires_at > now()
   GROUP BY 1 ORDER BY 2 DESC"

# Paksa login ulang untuk satu user (cabut semua sesi)
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=(SELECT id FROM users WHERE username='<USER>');
   UPDATE users SET token_version=token_version+1 WHERE username='<USER>'"
```

Saat ganti password user, `token_version` otomatis dinaikkan (F2) sehingga access token
lama langsung tidak valid.

---

## 6. Backup & Restore

> Panduan rinci (termasuk perbedaan dari MySQL, verifikasi, dan uji restore ke DB
> sementara) ada di [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md).

```bash
./scripts/backup.sh                        # manual
ls -lh backups/                            # daftar backup
./scripts/restore.sh backups/<file>.sql.gz # restore (stop api/worker dulu)
```

Jadwal host (02:00 WIB):

```cron
0 2 * * * cd /path/ke/ingat.in && ./scripts/backup.sh >> /var/log/ingatin-backup.log 2>&1
```

Verifikasi integritas backup:

```bash
cd backups && sha256sum -c ingatin-db-YYYYMMDD-HHMMSS.sql.gz.sha256
```

**Uji restore berkala** (minimal per kuartal) ke database sementara agar tidak menimpa produksi.

---

## 7. WAHA

```bash
docker compose ps waha
docker compose logs --tail=100 waha

# Status sesi via API (ganti <API_KEY>)
curl -s -H "X-Api-Key: <API_KEY>" http://127.0.0.1:8082/api/sessions | head -c 500

# Restart
docker compose restart waha
```

Sesi tersimpan di volume `waha_sessions`. `docker compose down` **tidak** menghapusnya.
Hilang sesi → buka `:8010`, scan QR baru.

Hemat memori: tambahkan `WHATSAPP_DEFAULT_ENGINE=GOWS` pada service `waha`.

---

## 7.1 Bot Command Telegram (F12)

Bot menerima command dari grup NOC lewat webhook
`POST https://<host>/api/hooks/telegram/<secret>` dan membuat/mengubah work item
memakai aturan yang sama dengan panel.

### Aktivasi

1. Isi secret di `.env` (acak, rahasia):

   ```bash
   openssl rand -hex 20     # -> INGATIN_TELEGRAM_WEBHOOK_SECRET
   ```

2. Restart API: `docker compose up -d api`.
3. Daftarkan webhook ke Telegram (sekali; token dari @BotFather):

   ```bash
   SECRET="<isi INGATIN_TELEGRAM_WEBHOOK_SECRET>"
   curl -s "https://api.telegram.org/bot<TOKEN>/setWebhook" \
     -d "url=https://<host>/api/hooks/telegram/${SECRET}" \
     -d "secret_token=${SECRET}"
   ```

4. Cek webhook: `curl -s "https://api.telegram.org/bot<TOKEN>/getWebhookInfo"`.

### Daftarkan grup (allowlist via Master Data)

1. Kirim `/id` di grup Telegram target → bot membalas **chat id** grup.
2. Panel → **Master Data → "Grup Telegram Bot" → Tambah**:
   - **Kode** = chat id (mis. `-1001234567890`)
   - **Label** = nama grup (mis. `NOC Utama`)
   - **Aktif** = on
3. Grup langsung aktif; kirim `/help` di grup untuk memastikan.

> Hanya admin (izin `masterdata.write`) yang dapat mengubah allowlist. Nonaktifkan
> dengan mematikan toggle **Aktif** pada entri grup — tanpa menghapus.

### Command

| Command | Efek |
|---|---|
| `/open <judul> [\| <PIC>]` | Buat **Daily Task** (`pending`) |
| `/ticket <judul> [\| <PIC>]` | Buat **tiket insiden** (mulai siklus SLA) |
| `/list` | Daftar tugas aktif hari ini |
| `/solved <REF>` | Tandai selesai (mis. `/solved DTK-2026-0007`) |
| `/hold <REF>` | Tahan (menunggu konfirmasi pelanggan) |
| `/rekap [dd/mm/yyyy]` | Rekap harian (default: hari ini WIB) |
| `/id` | Lihat chat id grup |
| `/help` | Panduan |

### Troubleshooting

| Gejala | Penyebab / tindakan |
|---|---|
| Bot tidak membalas | Webhook salah: `getWebhookInfo`; cek `INGATIN_TELEGRAM_WEBHOOK_SECRET` sama dengan `secret_token` |
| "Grup ini belum terdaftar" | Tambahkan entri master data `telegram_chat` (Kode = chat id dari `/id`) |
| Command tak diproses saat webhook di-retry | Normal: `bot_update_log` mencegah proses ganda |
| "Tidak bisa memindahkan ... ke ..." | Transisi tidak sah menurut workflow; cek status item di panel |
| "Terlalu banyak perintah" | Rate limit 20 command/menit per grup |

Jejak command: tabel `bot_command_log` (`chat_id`, `command`, `result`, waktu).

---

## 8. Maintenance Rutin

| Frekuensi | Tugas |
|---|---|
| Harian | `docker compose ps`, cek outbox `failed`, cek status sesi WAHA |
| Mingguan | Cek ukuran DB, cek job retensi berjalan, review log error |
| Bulanan | Review audit trail, cek disk (`df -h`), update image bila ada patch |
| Kuartalan | **Uji restore backup**, review policy eskalasi & SLA, audit akses user |
| Tahunan | Rotasi secret (`INGATIN_SECRET_KEY`, `WAHA_API_KEY`), review hardening |

Rotasi secret (perhatikan: token lama jadi tidak valid):

```bash
# 1) Generate secret baru
NEW=$(openssl rand -hex 32)
# 2) Update .env
sed -i "s/^INGATIN_SECRET_KEY=.*/INGATIN_SECRET_KEY=$NEW/" .env
# 3) Restart
docker compose up -d api worker
# 4) Semua user harus login ulang
```

---

## 9. Hardening (Opsional)

### 9.1 `pg_hba.conf` (127.0.0.1 trust → scram)

PG host saat ini mengizinkan `127.0.0.1/32 trust` (tanpa password). Karena server ini juga
melayani `juniper_manage` dan `mcnvpn` produksi, perubahan **berisiko** dan harus dites.

Dry-run yang aman:

1. Pastikan `mcnvpn` dan `juniper_manage` punya password valid di `.env` masing-masing.
2. Backup `pg_hba.conf`.
3. Ubah hanya baris `host all all 127.0.0.1/32` menjadi `scram-sha-256`.
4. `systemctl reload postgresql` (reload, bukan restart).
5. Verifikasi kedua aplikasi lain masih dapat konek **sebelum** menutup sesi.
6. Bila gagal → kembalikan file & reload.

> Jangan lakukan tanpa jendela maintenance dan konfirmasi pemilik aplikasi lain.

### 9.2 Lainnya

- Batasi akses `:8091` ke LAN/Tailscale (firewall).
- Pasang TLS (§7 `DEPLOYMENT.md`) agar token tidak lewat jaringan terbuka.
- Set `INGATIN_SESSION_IDLE_TIMEOUT_MINUTES` > 0 bila kebijakan keamanan menuntut.
- Review user secara berkala; nonaktifkan (`is_active=false`) akun yang tidak dipakai.

---

## 10. Uninstall

```bash
# Hapus container & network, PERTAHANKAN data
./uninstall.sh

# Hapus termasuk volume (DESTRUKTIF — semua data hilang)
./uninstall.sh --purge

# Hapus juga role & database di PostgreSQL HOST (DESTRUKTIF; hanya Mode A)
./uninstall.sh --purge --drop-db
```

> Pada Mode B (default), database ada di volume `pgdata` sehingga `--purge`
> sudah menghapusnya. `--drop-db` hanya relevan untuk PostgreSQL host (Mode A).

Selalu jalankan `./scripts/backup.sh` sebelum uninstall.

---

## 11. SLA, KPI & Penanganan Tiket (F20/F21)

### Siklus SLA
Setiap tiket memiliki siklus SLA (`ticket_sla_cycles`): siklus 0 sejak dibuat,
siklus 1.. setiap dibuka kembali. `closed_at` tetap ada; reopen menambah
`reopened_at`. Riwayat tidak dihapus.

```bash
# Siklus terbuka (sedang berjalan)
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT w.ref_no, c.cycle_no, c.opened_at, c.first_response_at
   FROM ticket_sla_cycles c JOIN work_items w ON w.id=c.work_item_id
   WHERE c.closed_at IS NULL ORDER BY c.opened_at"

# Tiket ber-SLA breached
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  "SELECT ref_no, status, sla_state, sla_breached_at FROM work_items
   WHERE sla_state='breached' AND NOT is_deleted"
```

| Kondisi | Tindakan |
|---|---|
| Banyak `breached` | Tinjau beban/penanganan; tambah penangan (kolom *Ikut Menangani*) |
| `sla_state` tetap `none` | Worker `sla_tick` tidak jalan atau tiket tanpa siklus → periksa `docker compose logs worker` |
| Ingin reset siklus | Admin: Force Unlock (membuka siklus baru + `reopen_count++`) |

### Lampiran
Disimpan di volume `ingatin_data` (`/app/data/attachments`). Pantau pemakaian disk:

```bash
docker run --rm -v ingatin_ingatin_data:/d alpine sh -c 'du -sh /d/attachments 2>/dev/null || echo "kosong"'
```

Batas ukuran diatur `INGATIN_ATTACHMENTS_MAX_MB` (default 50).

### KPI
Halaman **KPI & SLA** menampilkan metrik per periode; unduh CSV atau ekspor ke
Google Sheets (konfigurasi di menu *Google Sheets*).
