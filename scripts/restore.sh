#!/usr/bin/env bash
#
# restore.sh — memulihkan database Ingat.in dari file backup.
#
# PEMAKAIAN:
#   docker compose stop api worker
#   ./scripts/restore.sh backups/ingatin-db-20260101-020000.sql.gz
#   docker compose start api worker
#
# Script akan:
#   1. memverifikasi checksum (bila file .sha256 tersedia)
#   2. membuat backup pengaman sebelum menimpa
#   3. memuat ulang skema dari file dump
#
# Opsi:
#   --yes      lewati konfirmasi interaktif

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

log()  { printf '[restore] %s\n' "$*"; }
fail() { printf '[restore] ERROR: %s\n' "$*" >&2; exit 1; }

DUMP_FILE="${1:-}"
SKIP_CONFIRM=0
[[ "${2:-}" == "--yes" || "${1:-}" == "--yes" ]] && SKIP_CONFIRM=1
[[ "${1:-}" == "--yes" ]] && DUMP_FILE="${2:-}"

[[ -z "$DUMP_FILE" ]] && fail "pemakaian: ./scripts/restore.sh <file-backup.sql.gz> [--yes]"
[[ -f "$DUMP_FILE" ]] || fail "file tidak ditemukan: $DUMP_FILE"

# Muat .env.
if [[ -f "$PROJECT_DIR/.env" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key="$(echo "$key" | tr -d '[:space:]')"
    [[ -z "${!key:-}" ]] && export "$key=$value"
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$PROJECT_DIR/.env" || true)
fi

DB_URL="${INGATIN_DB_URL:-}"
[[ -z "$DB_URL" ]] && fail "INGATIN_DB_URL tidak diset (periksa .env)"

command -v psql >/dev/null 2>&1 || fail "psql tidak ditemukan. Install postgresql-client."
command -v gzip >/dev/null 2>&1 || fail "gzip tidak ditemukan."

# ---------------------------------------------------------------------------
# 1. Verifikasi checksum
# ---------------------------------------------------------------------------
SHA_FILE="${DUMP_FILE}.sha256"
if [[ -f "$SHA_FILE" ]]; then
  log "memverifikasi checksum"
  if command -v sha256sum >/dev/null 2>&1; then
    if ! ( cd "$(dirname "$DUMP_FILE")" && sha256sum -c "$(basename "$SHA_FILE")" >/dev/null ); then
      fail "checksum tidak cocok — file mungkin rusak. Dibatalkan."
    fi
  else
    EXPECTED="$(cut -d' ' -f1 < "$SHA_FILE")"
    ACTUAL="$(shasum -a 256 "$DUMP_FILE" | cut -d' ' -f1)"
    [[ "$EXPECTED" == "$ACTUAL" ]] || fail "checksum tidak cocok. Dibatalkan."
  fi
  log "checksum OK"
else
  log "file checksum tidak ditemukan — verifikasi dilewati"
fi

# Verifikasi arsip gzip dapat dibaca.
gzip -t "$DUMP_FILE" 2>/dev/null || fail "arsip gzip rusak"

# ---------------------------------------------------------------------------
# 2. Pisahkan kredensial
# ---------------------------------------------------------------------------
if [[ "$DB_URL" =~ ^postgres(ql)?://([^:]+):([^@]+)@(.+)$ ]]; then
  DB_USER="${BASH_REMATCH[2]}"
  DB_PASS="${BASH_REMATCH[3]}"
  DB_REST="${BASH_REMATCH[4]}"
  SANITIZED_URL="postgresql://${DB_USER}@${DB_REST}"
  DB_NAME="${DB_REST%%\?*}"  # host:port/db
  DB_NAME="${DB_NAME##*/}"
else
  SANITIZED_URL="$DB_URL"
  DB_PASS=""
  DB_NAME="(tidak diketahui)"
fi

# ---------------------------------------------------------------------------
# 3. Konfirmasi (restore bersifat destruktif)
# ---------------------------------------------------------------------------
log "PERINGATAN: operasi ini akan MENIMPA seluruh data pada database '$DB_NAME'."
log "file sumber: $DUMP_FILE"

if [[ "$SKIP_CONFIRM" == "0" ]]; then
  read -r -p "Ketik 'RESTORE' untuk melanjutkan: " answer
  [[ "$answer" == "RESTORE" ]] || fail "dibatalkan oleh pengguna"
fi

# ---------------------------------------------------------------------------
# 4. Backup pengaman
# ---------------------------------------------------------------------------
SAFETY="$PROJECT_DIR/backups/pre-restore-$(date -u '+%Y%m%d-%H%M%S').sql.gz"
mkdir -p "$(dirname "$SAFETY")"
log "membuat backup pengaman -> $(basename "$SAFETY")"

if [[ -n "$DB_PASS" ]]; then
  PGPASSWORD="$DB_PASS" pg_dump --no-owner --no-acl "$SANITIZED_URL" | gzip -9 > "$SAFETY"
else
  pg_dump --no-owner --no-acl "$SANITIZED_URL" | gzip -9 > "$SAFETY"
fi

[[ -s "$SAFETY" ]] || fail "gagal membuat backup pengaman; restore dibatalkan"
log "backup pengaman OK: $(du -h "$SAFETY" | cut -f1)"

# ---------------------------------------------------------------------------
# 5. Restore
# ---------------------------------------------------------------------------
log "memulihkan database..."

restore_cmd() {
  if [[ -n "$DB_PASS" ]]; then
    PGPASSWORD="$DB_PASS" psql -v ON_ERROR_STOP=1 --no-owner --no-acl -q "$SANITIZED_URL"
  else
    psql -v ON_ERROR_STOP=1 --no-owner --no-acl -q "$SANITIZED_URL"
  fi
}

# Hentikan pada error agar tidak merusak sebagian.
if ! gunzip -c "$DUMP_FILE" | restore_cmd; then
  fail "restore gagal. Backup pengaman tersedia di: $SAFETY"
fi

# ---------------------------------------------------------------------------
# 6. Verifikasi
# ---------------------------------------------------------------------------
log "verifikasi hasil restore"
if [[ -n "$DB_PASS" ]]; then
  PGPASSWORD="$DB_PASS" psql -tAc "SELECT count(*) FROM work_items" "$SANITIZED_URL" >/dev/null 2>&1 \
    && log "tabel work_items dapat dibaca" \
    || log "peringatan: tidak dapat membaca work_items (periksa manual)"
else
  psql -tAc "SELECT count(*) FROM work_items" "$SANITIZED_URL" >/dev/null 2>&1 \
    && log "tabel work_items dapat dibaca" \
    || log "peringatan: tidak dapat membaca work_items (periksa manual)"
fi

log "restore selesai."
log "langkah berikutnya: docker compose start api worker"
