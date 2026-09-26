#!/usr/bin/env bash
#
# backup.sh — backup database Ingat.in.
#
# Menghasilkan: backups/ingatin-db-YYYYMMDD-HHMMSS.sql.gz
#               backups/ingatin-db-YYYYMMDD-HHMMSS.sql.gz.sha256
#
# Variabel:
#   KEEP_DAYS   retensi backup dalam hari (default: INGATIN_BACKUP_KEEP_DAYS atau 14)
#
# Pemasangan cron harian 02:00 WIB:
#   0 2 * * * cd /path/ke/ingat.in && ./scripts/backup.sh >> /var/log/ingatin-backup.log 2>&1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$PROJECT_DIR/backups}"

log()  { printf '[backup] %s\n' "$*"; }
fail() { printf '[backup] ERROR: %s\n' "$*" >&2; exit 1; }

# Muat .env bila ada.
if [[ -f "$PROJECT_DIR/.env" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key="$(echo "$key" | tr -d '[:space:]')"
    [[ -z "${!key:-}" ]] && export "$key=$value"
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$PROJECT_DIR/.env" || true)
fi

DB_URL="${INGATIN_DB_URL:-}"
[[ -z "$DB_URL" ]] && fail "INGATIN_DB_URL tidak diset (periksa .env)"

KEEP_DAYS="${KEEP_DAYS:-${INGATIN_BACKUP_KEEP_DAYS:-14}}"

command -v gzip >/dev/null 2>&1 || fail "gzip tidak ditemukan."

mkdir -p "$BACKUP_DIR"

# ---------------------------------------------------------------------------
# Tentukan mode koneksi.
#
# Mode B (default): PostgreSQL adalah container `ingatin-postgres`. pg_dump
#   dijalankan DI DALAM container sehingga versi klien selalu cocok dengan
#   server (menghindari "server version mismatch" ketika postgresql-client host
#   lebih lama). pg_dump 16 di dalam image postgres:16 sudah tersedia.
# Mode A (lama): PostgreSQL host. pg_dump dijalankan di host dengan host:port
#   di-remap ke loopback.
# ---------------------------------------------------------------------------
MODE_B=0
PG_CONTAINER="${INGATIN_PG_CONTAINER:-}"
if [[ -n "$PG_CONTAINER" ]]; then
  MODE_B=1
elif [[ "$DB_URL" == *"@postgres:5432/"* ]]; then
  # Cari container postgres milik proyek ini.
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx 'ingatin-postgres'; then
    PG_CONTAINER="ingatin-postgres"
    MODE_B=1
  else
    # Nama container dapat berbeda bila COMPOSE_PROJECT_NAME diubah.
    PG_CONTAINER="$(docker ps --filter 'label=com.docker.compose.service=postgres' \
      --format '{{.Names}}' 2>/dev/null | head -1)"
    if [[ -n "$PG_CONTAINER" ]]; then
      MODE_B=1
    else
      fail "container PostgreSQL tidak ditemukan. Jalankan 'docker compose up -d postgres' terlebih dahulu, atau set INGATIN_PG_CONTAINER."
    fi
  fi
fi

# Pisahkan kredensial dari URL agar password tidak muncul di daftar proses.
# Format: postgres://user:pass@host:port/db?params
if [[ "$DB_URL" =~ ^postgres(ql)?://([^:]+):([^@]+)@(.+)$ ]]; then
  DB_USER="${BASH_REMATCH[2]}"
  DB_PASS="${BASH_REMATCH[3]}"
  DB_REST="${BASH_REMATCH[4]}"
  SANITIZED_URL="postgresql://${DB_USER}@${DB_REST}"
else
  # Tanpa password di URL.
  SANITIZED_URL="$DB_URL"
  DB_PASS=""
fi

# Nama database untuk pg_dump di dalam container.
DB_NAME_FROM_URL="${DB_REST%%\?*}"
DB_NAME_FROM_URL="${DB_NAME_FROM_URL##*/}"
DB_NAME_FROM_URL="${DB_NAME_FROM_URL:-${INGATIN_DB_NAME:-ingatin}}"

if [[ "$MODE_B" == "0" ]]; then
  # Mode A: pg_dump dari host. Remap host:port ke loopback.
  DB_URL="${DB_URL/@postgres:5432\//@127.0.0.1:${INGATIN_PG_PORT:-5432}\/}"
  SANITIZED_URL="${SANITIZED_URL/@postgres:5432\//@127.0.0.1:${INGATIN_PG_PORT:-5432}\/}"
  command -v pg_dump >/dev/null 2>&1 || fail "pg_dump tidak ditemukan. Install postgresql-client atau gunakan Mode B."
fi

STAMP="$(date -u '+%Y%m%d-%H%M%S')"
OUT_BASE="ingatin-db-${STAMP}.sql.gz"
OUT_PATH="$BACKUP_DIR/$OUT_BASE"
TMP_PATH="${OUT_PATH}.part"

log "memulai backup -> $OUT_BASE"

cleanup() {
  if [[ -f "$TMP_PATH" ]]; then
    rm -f "$TMP_PATH"
    log "file parsial dihapus"
  fi
}
trap cleanup EXIT

# Pipe pg_dump -> gzip. Password dikirim lewat PGPASSWORD agar tidak terlihat
# di daftar proses.
if [[ "$MODE_B" == "1" ]]; then
  # Mode B: jalankan pg_dump di dalam container PostgreSQL (versi klien cocok).
  log "menggunakan container PostgreSQL: $PG_CONTAINER"
  docker exec -e PGPASSWORD="$DB_PASS" "$PG_CONTAINER" \
    pg_dump --no-owner --no-acl -U "$DB_USER" -d "$DB_NAME_FROM_URL" \
    | gzip -9 > "$TMP_PATH"
elif [[ -n "$DB_PASS" ]]; then
  PGPASSWORD="$DB_PASS" pg_dump --no-owner --no-acl "$SANITIZED_URL" | gzip -9 > "$TMP_PATH"
else
  pg_dump --no-owner --no-acl "$SANITIZED_URL" | gzip -9 > "$TMP_PATH"
fi

# Verifikasi hasil tidak kosong.
if [[ ! -s "$TMP_PATH" ]]; then
  fail "hasil backup kosong"
fi

# Verifikasi gzip dapat dibaca.
if ! gzip -t "$TMP_PATH" 2>/dev/null; then
  fail "arsip gzip rusak"
fi

mv "$TMP_PATH" "$OUT_PATH"

# Checksum.
if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$BACKUP_DIR" && sha256sum "$OUT_BASE" > "${OUT_BASE}.sha256" )
else
  ( cd "$BACKUP_DIR" && shasum -a 256 "$OUT_BASE" > "${OUT_BASE}.sha256" )
fi

SIZE="$(du -h "$OUT_PATH" | cut -f1)"
SHA="$(cut -d' ' -f1 < "${OUT_PATH}.sha256")"
log "selesai: $OUT_BASE ($SIZE)"
log "sha256: $SHA"

# ---------------------------------------------------------------------------
# Rotasi
# ---------------------------------------------------------------------------
log "menghapus backup lebih tua dari $KEEP_DAYS hari"
DELETED=0
while IFS= read -r old; do
  [[ -z "$old" ]] && continue
  rm -f "$old" "${old}.sha256" 2>/dev/null || true
  log "  dihapus: $(basename "$old")"
  DELETED=$((DELETED + 1))
done < <(find "$BACKUP_DIR" -maxdepth 1 -name 'ingatin-db-*.sql.gz' -type f -mtime "+$KEEP_DAYS" 2>/dev/null || true)

log "rotasi selesai ($DELETED file dihapus)"
log "total backup tersimpan: $(find "$BACKUP_DIR" -maxdepth 1 -name 'ingatin-db-*.sql.gz' -type f | wc -l)"
