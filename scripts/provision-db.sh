#!/usr/bin/env bash
#
# provision-db.sh — membuat role & database Ingat.in di PostgreSQL host.
#
# Idempoten: aman dijalankan berulang.
#   - role   : dibuat bila belum ada, password selalu disinkronkan ke INGATIN_DB_PASSWORD
#   - database: dibuat bila belum ada, owner diarahkan ke role ingatin
#
# Cara pakai:
#   ./scripts/provision-db.sh
#
# Variabel (dibaca dari .env atau environment):
#   INGATIN_DB_NAME      (default: ingatin)
#   INGATIN_DB_USER      (default: ingatin)
#   INGATIN_DB_PASSWORD  (wajib, atau dibuat acak)
#   INGATIN_PG_SUPERUSER (default: postgres) — dijalankan via sudo -u
#   INGATIN_PG_HOST      (default: 127.0.0.1)
#   INGATIN_PG_PORT      (default: 5432)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Muat .env bila ada (tanpa menimpa variabel yang sudah diset).
if [[ -f "$PROJECT_DIR/.env" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key="$(echo "$key" | tr -d '[:space:]')"
    [[ -z "${!key:-}" ]] && export "$key=$value"
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$PROJECT_DIR/.env" || true)
fi

DB_NAME="${INGATIN_DB_NAME:-ingatin}"
DB_USER="${INGATIN_DB_USER:-ingatin}"
DB_PASS="${INGATIN_DB_PASSWORD:-}"
PG_SUPERUSER="${INGATIN_PG_SUPERUSER:-postgres}"
PG_HOST="${INGATIN_PG_HOST:-127.0.0.1}"
PG_PORT="${INGATIN_PG_PORT:-5432}"

log()  { printf '[provision-db] %s\n' "$*"; }
fail() { printf '[provision-db] ERROR: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Validasi
# ---------------------------------------------------------------------------
command -v psql >/dev/null 2>&1 || fail "psql tidak ditemukan. Install postgresql-client."

if [[ -z "$DB_PASS" ]]; then
  log "INGATIN_DB_PASSWORD kosong — membuat password acak"
  DB_PASS="$(openssl rand -hex 20)"
  log "Password dibuat. Tambahkan ke .env:"
  log "  INGATIN_DB_PASSWORD=$DB_PASS"
  log "  INGATIN_DB_URL=postgres://$DB_USER:$DB_PASS@$PG_HOST:$PG_PORT/$DB_NAME?sslmode=disable"
fi

# Nama identifier tidak boleh mengandung kutip; validasi sederhana.
for ident in "$DB_NAME" "$DB_USER"; do
  [[ "$ident" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || fail "nama identifier tidak valid: $ident"
done

# Cek akses superuser (peer auth lewat sudo -u postgres).
if ! sudo -n -u "$PG_SUPERUSER" psql -tAc 'SELECT 1' >/dev/null 2>&1; then
  if sudo -u "$PG_SUPERUSER" psql -tAc 'SELECT 1' >/dev/null 2>&1; then
    :
  else
    fail "tidak dapat terhubung sebagai superuser '$PG_SUPERUSER' (butuh sudo)."
  fi
fi

log "PostgreSQL terdeteksi; menyiapkan role '$DB_USER' dan database '$DB_NAME'"

# ---------------------------------------------------------------------------
# Role
# ---------------------------------------------------------------------------
ROLE_EXISTS="$(sudo -u "$PG_SUPERUSER" psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'")"
if [[ "$ROLE_EXISTS" == "1" ]]; then
  log "role '$DB_USER' sudah ada — menyinkronkan password"
  sudo -u "$PG_SUPERUSER" psql -q -c "ALTER ROLE \"$DB_USER\" WITH LOGIN PASSWORD '$DB_PASS'"
else
  log "membuat role '$DB_USER'"
  sudo -u "$PG_SUPERUSER" psql -q -c "CREATE ROLE \"$DB_USER\" WITH LOGIN PASSWORD '$DB_PASS'"
fi

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------
DB_EXISTS="$(sudo -u "$PG_SUPERUSER" psql -tAc "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'")"
if [[ "$DB_EXISTS" == "1" ]]; then
  log "database '$DB_NAME' sudah ada"
else
  log "membuat database '$DB_NAME' (owner: $DB_USER)"
  sudo -u "$PG_SUPERUSER" psql -q -c "CREATE DATABASE \"$DB_NAME\" OWNER \"$DB_USER\" ENCODING 'UTF8'"
fi

# Pastikan owner benar (idempoten).
sudo -u "$PG_SUPERUSER" psql -q -c "ALTER DATABASE \"$DB_NAME\" OWNER TO \"$DB_USER\"" >/dev/null

# Beri hak pada schema public (PG 15 mencabut CREATE dari public secara default).
sudo -u "$PG_SUPERUSER" psql -q -d "$DB_NAME" -c "GRANT ALL ON SCHEMA public TO \"$DB_USER\"" >/dev/null

# ---------------------------------------------------------------------------
# Verifikasi koneksi sebagai user aplikasi
# ---------------------------------------------------------------------------
log "memverifikasi koneksi sebagai '$DB_USER'"

# PG hanya listen di localhost; koneksi via TCP 127.0.0.1 diuji bila memungkinkan.
# pg_hba untuk 127.0.0.1/32 bisa 'trust' sehingga password tidak selalu diuji,
# tetapi kita tetap menyertakan PGPASSWORD agar perilakunya konsisten.
if PGPASSWORD="$DB_PASS" psql -h "$PG_HOST" -p "$PG_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc 'SELECT current_database(), current_user' >/dev/null 2>&1; then
  log "koneksi TCP OK: $DB_USER@$PG_HOST:$PG_PORT/$DB_NAME"
else
  log "koneksi TCP gagal; mencoba lewat socket unix (peer/local)"
  if sudo -u "$PG_SUPERUSER" psql -d "$DB_NAME" -tAc 'SELECT 1' >/dev/null 2>&1; then
    log "database dapat diakses via socket; periksa pg_hba bila container gagal konek"
  else
    fail "database tidak dapat diakses"
  fi
fi

log "selesai. Gunakan URL berikut di .env:"
log "  INGATIN_DB_URL=postgres://$DB_USER:$DB_PASS@$PG_HOST:$PG_PORT/$DB_NAME?sslmode=disable"
log ""
log "Catatan: bila service api/worker berjalan dengan network_mode: host,"
log "         host '$PG_HOST' sudah benar. Bila memakai container Postgres"
log "         (Mode B), ganti host menjadi 'postgres'."
