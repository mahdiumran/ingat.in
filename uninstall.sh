#!/usr/bin/env bash
#
# uninstall.sh — menghentikan dan menghapus Ingat.in.
#
# PEMAKAIAN:
#   ./uninstall.sh                  hentikan & hapus container (data tetap)
#   ./uninstall.sh --purge          hapus juga volume (DESTRUKTIF: data hilang)
#   ./uninstall.sh --purge --drop-db hapus juga role & database PostgreSQL (DESTRUKTIF)
#
# SELALU jalankan ./scripts/backup.sh sebelum uninstall.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PURGE=0
DROP_DB=0
for arg in "$@"; do
  case "$arg" in
    --purge)   PURGE=1 ;;
    --drop-db) DROP_DB=1 ;;
    -h|--help)
      sed -n '2,12p' "$0"
      exit 0
      ;;
    *) printf 'Argumen tidak dikenal: %s\n' "$arg" >&2; exit 1 ;;
  esac
done

log()  { printf '[uninstall] %s\n' "$*"; }
warn() { printf '\033[1;33m[uninstall]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[uninstall] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

if docker compose version >/dev/null 2>&1; then
  DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  DC="docker-compose"
else
  fail "Docker Compose tidak ditemukan"
fi

# ---------------------------------------------------------------------------
# Peringatan
# ---------------------------------------------------------------------------
warn "Operasi ini akan menghentikan seluruh service Ingat.in."
if [[ "$PURGE" == "1" ]]; then
  warn "MODE --purge: VOLUME AKAN DIHAPUS. Seluruh data (database aplikasi, sesi WhatsApp) HILANG PERMANEN."
fi
if [[ "$DROP_DB" == "1" ]]; then
  warn "MODE --drop-db: role & database PostgreSQL akan DIHAPUS."
fi

if [[ -d "$SCRIPT_DIR/backups" ]] && compgen -G "$SCRIPT_DIR/backups/ingatin-db-*.sql.gz" >/dev/null; then
  log "backup yang tersedia:"
  ls -1 "$SCRIPT_DIR"/backups/ingatin-db-*.sql.gz 2>/dev/null | tail -5 | sed 's/^/  /'
else
  warn "tidak ditemukan file backup di backups/"
  warn "sangat disarankan menjalankan ./scripts/backup.sh terlebih dahulu."
  read -r -p "Lanjutkan tanpa backup? (ketik 'lanjut'): " answer
  [[ "$answer" == "lanjut" ]] || fail "dibatalkan"
fi

# ---------------------------------------------------------------------------
# Hentikan service
# ---------------------------------------------------------------------------
log "menghentikan service"
if [[ "$PURGE" == "1" ]]; then
  $DC down -v --remove-orphans || true
else
  $DC down --remove-orphans || true
fi

# ---------------------------------------------------------------------------
# Volume (safety net bila masih tersisa)
# ---------------------------------------------------------------------------
if [[ "$PURGE" == "1" ]]; then
  for vol in ingatin_data waha_sessions; do
    if docker volume inspect "$vol" >/dev/null 2>&1; then
      log "menghapus volume $vol"
      docker volume rm "$vol" >/dev/null 2>&1 || warn "gagal menghapus volume $vol"
    fi
  done
fi

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------
if [[ "$DROP_DB" == "1" ]]; then
  # Muat .env untuk nama database.
  if [[ -f .env ]]; then
    while IFS='=' read -r key value; do
      [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
      key="$(echo "$key" | tr -d '[:space:]')"
      [[ -z "${!key:-}" ]] && export "$key=$value"
    done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' .env || true)
  fi

  DB_NAME="${INGATIN_DB_NAME:-ingatin}"
  DB_USER="${INGATIN_DB_USER:-ingatin}"
  PG_SUPERUSER="${INGATIN_PG_SUPERUSER:-postgres}"

  warn "menghapus database '$DB_NAME' dan role '$DB_USER'"
  read -r -p "Ketik 'HAPUS' untuk melanjutkan: " confirm
  if [[ "$confirm" == "HAPUS" ]]; then
    sudo -u "$PG_SUPERUSER" psql -q -c "DROP DATABASE IF EXISTS \"$DB_NAME\" WITH (FORCE)" 2>/dev/null \
      || sudo -u "$PG_SUPERUSER" psql -q -c "DROP DATABASE IF EXISTS \"$DB_NAME\"" 2>/dev/null \
      || warn "gagal menghapus database $DB_NAME"
    sudo -u "$PG_SUPERUSER" psql -q -c "DROP ROLE IF EXISTS \"$DB_USER\"" 2>/dev/null \
      || warn "gagal menghapus role $DB_USER"
    log "database & role dihapus"
  else
    log "penghapusan database dibatalkan (hanya database)"
  fi
fi

# ---------------------------------------------------------------------------
# Ringkasan
# ---------------------------------------------------------------------------
echo
log "selesai."
if [[ "$PURGE" == "0" ]]; then
  log "volume dipertahankan. Menjalankan ulang 'docker compose up -d' akan mengembalikan sistem dengan data yang sama."
fi
log "file .env, backups/, dan kode sumber TIDAK dihapus. Hapus manual bila diinginkan."
