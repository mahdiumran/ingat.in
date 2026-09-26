# Backup & Restore Database — Ingat.in

Panduan khusus untuk **backup dan restore database PostgreSQL** aplikasi Ingat.in
yang berjalan di atas Docker Compose.

> **Catatan penting: ini PostgreSQL, bukan MySQL.**
> Database Ingat.in adalah **PostgreSQL** (host reuse `:5432`, atau container
> `postgres:16-alpine` pada mode B). Karena itu alat yang dipakai adalah
> **`pg_dump` / `psql`**, **bukan** `mysqldump` / `mysql`. Lihat lampiran
> [§8 Perbedaan dari MySQL](#8-lampiran--perbedaan-dari-mysql) bila Anda terbiasa
> dengan MySQL.

---

## 1. Ringkasan

| Aspek | Nilai |
|---|---|
| Engine | PostgreSQL 16 (container `ingatin-postgres`; Mode A: PostgreSQL 14+ host) |
| Alat dump | `pg_dump` — dijalankan **di dalam container** pada Mode B |
| Alat restore | `psql` — dijalankan **di dalam container** pada Mode B |
| Format | Dump SQL teks → `gzip -9` → `.sql.gz` |
| Lokasi backup | `backups/` (default; dapat dioverride `BACKUP_DIR`) |
| Nama file | `ingatin-db-YYYYMMDD-HHMMSS.sql.gz` |
| Checksum | `ingatin-db-YYYYMMDD-HHMMSS.sql.gz.sha256` |
| Retensi default | 14 hari (via `KEEP_DAYS` / `INGATIN_BACKUP_KEEP_DAYS`) |
| Script | `scripts/backup.sh`, `scripts/restore.sh` |

Backup menyalin **seluruh database** (semua tabel), bukan hanya tabel
aplikasi. Tabel `work_items` dipakai oleh `restore.sh` sebagai verifikasi baca
setelah restore.

---

## 2. Prasyarat

`gzip` harus tersedia di host. **`pg_dump`/`psql` host TIDAK diperlukan pada
Mode B**: script menjalankan `pg_dump`/`psql` di dalam container PostgreSQL
sehingga versi klien selalu cocok dengan server (menghindari error
`server version mismatch`).

Pada Mode A (PostgreSQL host), host wajib punya `postgresql-client` dengan versi
yang kompatibel:

```bash
# Debian/Ubuntu
sudo apt-get install -y postgresql-client gzip

# Alpine
sudo apk add postgresql-client gzip

# Verifikasi
pg_dump --version
psql --version
```

Script membaca koneksi dari `.env` (`INGATIN_DB_URL`). Jika variabel itu tidak
ada, script berhenti dengan error.

```env
# Mode B — PostgreSQL container (default)
INGATIN_DB_URL=postgres://ingatin:<PASSWORD>@postgres:5432/ingatin?sslmode=disable

# Mode A — PostgreSQL host (alternatif)
INGATIN_DB_URL=postgres://ingatin:<PASSWORD>@127.0.0.1:5432/ingatin?sslmode=disable
```

Pada Mode B, script otomatis menemukan container `postgres` (berdasarkan nama
`ingatin-postgres` atau label Compose). Bila nama berbeda, set
`INGATIN_PG_CONTAINER=<nama-container>`.

> Password **tidak** muncul di daftar proses: script memisahkan kredensial dari
> URL dan mengirimkan password lewat `PGPASSWORD`.

---

## 3. Backup

### 3.1 Backup manual

```bash
cd /path/ke/ingat.in
./scripts/backup.sh
```

Alur yang dilakukan `backup.sh`:

1. Memuat `.env` dan memvalidasi `INGATIN_DB_URL`.
2. Menentukan mode: Mode B (container `postgres`) atau Mode A (host) + `gzip`.
3. `pg_dump --no-owner --no-acl` (di dalam container pada Mode B) → pipe ke
   `gzip -9` → file `.part`.
4. Memverifikasi hasil tidak kosong dan arsip gzip tidak rusak (`gzip -t`).
5. Rename `.part` ke nama final (atomic).
6. Menulis checksum `sha256`.
7. **Rotasi**: menghapus file lebih tua dari `KEEP_DAYS` hari (beserta `.sha256`).

Contoh keluaran:

```
[backup] memulai backup -> ingatin-db-20260101-020000.sql.gz
[backup] menggunakan container PostgreSQL: ingatin-postgres
[backup] selesai: ingatin-db-20260101-020000.sql.gz (12M)
[backup] sha256: 3f2a...e91c
[backup] rotasi selesai (1 file dihapus)
[backup] total backup tersimpan: 14
```

### 3.2 Opsi & variabel

| Variabel | Default | Keterangan |
|---|---|---|
| `BACKUP_DIR` | `<project>/backups` | Direktori tujuan backup |
| `KEEP_DAYS` | `INGATIN_BACKUP_KEEP_DAYS` atau `14` | Retensi dalam hari |
| `INGATIN_PG_CONTAINER` | auto-detect | Nama container PostgreSQL (Mode B) |

Contoh mengubah retensi & lokasi:

```bash
KEEP_DAYS=30 BACKUP_DIR=/mnt/backup/ingatin ./scripts/backup.sh
```

### 3.3 Jadwal otomatis (cron host, 02:00 WIB)

```cron
0 2 * * * cd /path/ke/ingat.in && ./scripts/backup.sh >> /var/log/ingatin-backup.log 2>&1
```

Isi `INGATIN_BACKUP_KEEP_DAYS` di `.env` untuk mengatur retensi dari cron.

### 3.4 Backup langsung dari container PostgreSQL (alternatif)

Setara dengan isi `backup.sh` Mode B, tanpa memerlukan `pg_dump` di host:

```bash
docker compose exec postgres sh -c \
  'PGPASSWORD="$POSTGRES_PASSWORD" pg_dump --no-owner --no-acl \
   -U "$POSTGRES_USER" -d "$POSTGRES_DB" | gzip -9' \
  > backups/manual-$(date -u +%Y%m%d-%H%M%S).sql.gz
```

> Cara paling aman tetap memakai `scripts/backup.sh` karena sudah menangani
> kredensial, checksum, dan rotasi.

---

## 4. Restore

### 4.1 Prosedur lengkap (produksi)

```bash
cd /path/ke/ingat.in

# 1) Hentikan penulis agar tidak ada tulis-sementara saat restore
docker compose stop api worker

# 2) Restore (interaktif: ketik 'RESTORE' untuk konfirmasi)
./scripts/restore.sh backups/ingatin-db-20260101-020000.sql.gz

# 3) Jalankan kembali
docker compose start api worker
```

Alur yang dilakukan `restore.sh`:

1. Memverifikasi checksum `.sha256` (bila ada) — batal bila tidak cocok.
2. Memvalidasi arsip `gzip -t`.
3. Menampilkan peringatan destruktif dan meminta konfirmasi (`RESTORE`).
4. **Membuat backup pengaman** ke `backups/pre-restore-YYYYMMDD-HHMMSS.sql.gz`.
5. Mengosongkan schema `public` (`DROP SCHEMA ... CASCADE; CREATE SCHEMA public;`)
   agar dump dapat dimuat ke database yang sudah berisi skema.
6. `gunzip -c | psql -v ON_ERROR_STOP=1` (berhenti pada error pertama agar tidak
   meninggalkan restore separuh jalan).
7. Verifikasi baca `SELECT count(*) FROM work_items`.

Pada Mode B, langkah 4–6 dijalankan di dalam container `postgres`.

### 4.2 Mode non-interaktif (otomasi)

```bash
./scripts/restore.sh backups/ingatin-db-20260101-020000.sql.gz --yes
```

`--yes` melewati konfirmasi interaktif — gunakan hanya pada pipeline otomatis
yang sudah teruji. Backup pengaman **tetap** dibuat.

### 4.3 Uji restore berkala (ke DB sementara) — WAJIB per kuartal

Jangan uji ke produksi. Buat database sementara, restore ke sana, lalu hapus.

```bash
# 1) Buat DB uji (di dalam container PostgreSQL, Mode B)
docker compose exec postgres sh -c \
  'psql -U "$POSTGRES_USER" -d postgres -c "CREATE DATABASE ingatin_restore_test OWNER $POSTGRES_USER"'

# 2) Restore ke DB uji (tanpa menyentuh produksi)
gunzip -c backups/ingatin-db-YYYYMMDD-HHMMSS.sql.gz | \
  docker compose exec -T postgres sh -c \
  'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d ingatin_restore_test'

# 3) Verifikasi isi
docker compose exec postgres sh -c \
  'psql -U "$POSTGRES_USER" -d ingatin_restore_test \
   -c "SELECT count(*) FROM work_items" \
   -c "SELECT count(*) FROM information_schema.tables WHERE table_schema=\$\$public\$\$"'

# 4) Bersihkan
docker compose exec postgres sh -c \
  'psql -U "$POSTGRES_USER" -d postgres -c "DROP DATABASE ingatin_restore_test"'
```

> Mode A (host): gunakan `sudo -u postgres createdb/dropdb` dan `psql` host
> seperti sebelumnya.

Catat tanggal uji terakhir di checklist §7.

---

## 5. Verifikasi integritas

Verifikasi checksum kapan pun tanpa restore:

```bash
cd backups
sha256sum -c ingatin-db-20260101-020000.sql.gz.sha256
```

Verifikasi isi arsip (apakah dump valid & berisi tabel):

```bash
# Cek gzip utuh
gzip -t ingatin-db-20260101-020000.sql.gz && echo OK

# Lihat daftar tabel di dalam dump tanpa membukanya penuh
gunzip -c ingatin-db-20260101-020000.sql.gz | grep -E '^CREATE TABLE' | head
```

---

## 6. Troubleshooting

| Gejala | Penyebab | Tindakan |
|---|---|---|
| `pg_dump: command not found` (Mode A) | client tidak ada di host | `apt-get install postgresql-client`, atau pakai Mode B |
| `pg_dump: server version mismatch` | `pg_dump` host lebih lama dari server | Pakai `./scripts/backup.sh` (otomatis memakai container di Mode B) |
| `container PostgreSQL tidak ditemukan` | container `postgres` tidak berjalan / nama beda | `docker compose up -d postgres`; atau set `INGATIN_PG_CONTAINER` |
| `INGATIN_DB_URL tidak diset` | `.env` hilang/kosong | periksa `.env` di root proyek |
| `connection refused` ke `127.0.0.1:5432` (Mode A) | PostgreSQL mati / bukan di host | `systemctl status postgresql`; cek mode A vs B |
| `hasil backup kosong` | DB kosong / URL salah DB | cek `INGATIN_DB_URL` & hak akses role |
| `checksum tidak cocok` | file backup korup | ambil backup lain; jangan restore file ini |
| `permission denied for schema public` saat restore | role bukan pemilik DB | `sudo -u postgres psql -c 'ALTER DATABASE ingatin OWNER TO ingatin'` |
| restore gagal di tengah | error SQL / DB inkonsisten | pulihkan dari `backups/pre-restore-*.sql.gz` |
| backup tak terjadwal | cron tidak aktif | `crontab -l`, cek log `/var/log/ingatin-backup.log` |

**Rollback restore yang gagal:** restore.sh selalu menyimpan backup pengaman
sebelum menimpa. Pulihkan dengan:

```bash
docker compose stop api worker
./scripts/restore.sh backups/pre-restore-YYYYMMDD-HHMMSS.sql.gz --yes
docker compose start api worker
```

---

## 7. Checklist operator

- [ ] `pg_dump`/`psql` terpasang di host, versi ≥ versi server
- [ ] `INGATIN_DB_URL` benar di `.env`
- [ ] Cron backup harian aktif (`crontab -l`)
- [ ] Retensi (`KEEP_DAYS`) sesuai kebijakan
- [ ] Backup disalin ke luar server (offsite) secara berkala
- [ ] Uji restore ke `ingatin_restore_test` pernah berhasil (per kuartal)
- [ ] Backup dijalankan **sebelum** upgrade/uninstall (`./scripts/backup.sh`)
- [ ] Log backup dicek (`/var/log/ingatin-backup.log`)

---

## 8. Lampiran — Perbedaan dari MySQL

Bila Anda terbiasa dengan MySQL, tabel padanan berikut memudahkan transisi.
Ingat.in **memakai PostgreSQL**, jadi kolom "Ingat.in (PostgreSQL)" yang berlaku.

| Kebutuhan | MySQL | Ingat.in (PostgreSQL) |
|---|---|---|
| Dump database | `mysqldump db > dump.sql` | `pg_dump --no-owner --no-acl <url>` |
| Restore | `mysql db < dump.sql` | `psql < dump.sql` |
| Client | `mysql` | `psql` |
| Password env | `MYSQL_PWD` | `PGPASSWORD` |
| URL | `mysql://user:pass@host:3306/db` | `postgres://user:pass@host:5432/db?sslmode=disable` |
| Tipe host | MySQL 8.x | PostgreSQL 14+/16 |
| Port default | 3306 | 5432 |
| Cek tabel | `information_schema.tables` (sama) | `information_schema.tables` (sama) |

**Kesetaraan perintah penting:**

```bash
# MySQL              →  PostgreSQL (Ingat.in)
mysqldump db | gzip  →  pg_dump <url> | gzip -9
mysql db < dump      →  psql <url> < dump
sha256sum file       →  sha256sum file   # sama
```

**Hal yang TIDAK berlaku di PostgreSQL** (jangan dipakai): `mysqldump`,
`SHOW DATABASES`, `USE db`, `ENGINE=InnoDB`, `AUTO_INCREMENT`. Padanan
PostgreSQL: `\l` (daftar DB), `\c db` (ganti DB), `GENERATED ... AS IDENTITY`
(pengganti AUTO_INCREMENT).

---

## 9. Referensi terkait

| Dokumen | Bagian |
|---|---|
| `DEPLOYMENT.md` | §10 Backup & Restore, §5 mode DB (host vs container) |
| `OPERATIONS.md` | §6 Backup & Restore (runbook harian) |
| `scripts/backup.sh` | Implementasi backup + rotasi |
| `scripts/restore.sh` | Implementasi restore + backup pengaman |
| `backend/Dockerfile` | Runtime `postgresql-client` + `gzip` |
