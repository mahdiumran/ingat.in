#!/usr/bin/env bash
#
# backfill-item-team.sh — isi team_id untuk task/daily_task lama yang kosong.
#
# LATAR: sebelum perbaikan, admin/super user yang membuat task tanpa memilih
# tim menghasilkan item dengan team_id NULL ("Tanpa tim"). Item seperti itu
# hanya terlihat oleh admin (lihat backend/internal/api/team_scope.go).
#
# Skrip ini mengisi team_id dari TIM PEMBUAT (users.team_id berdasarkan
# work_items.created_by). Baris yang pembuatnya tidak punya tim, atau yang
# memang bukan task/daily_task, tidak disentuh.
#
# IDEMPOTEN: hanya menyentuh baris dengan team_id IS NULL pada task/daily_task.
#
# PEMAKAIAN:
#   ./scripts/backfill-item-team.sh --dry-run     # lihat dampak tanpa mengubah
#   ./scripts/backfill-item-team.sh               # terapkan
#
# Setelah dijalankan, jalankan ulang migrasi/relasi aplikasi tidak diperlukan;
# cukup refresh panel. Disarankan backup dulu: ./scripts/backup.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) printf 'Argumen tidak dikenal: %s\n' "$arg" >&2; exit 1 ;;
  esac
done

log()  { printf '[backfill-team] %s\n' "$*"; }
warn() { printf '\033[1;33m[backfill-team]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[backfill-team] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# Muat .env (tanpa menimpa variabel yang sudah diset).
if [[ -f "$PROJECT_DIR/.env" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key="$(echo "$key" | tr -d '[:space:]')"
    [[ -z "${!key:-}" ]] && export "$key=$value"
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$PROJECT_DIR/.env" || true)
fi

DB_URL="${INGATIN_DB_URL:-}"
[[ -z "$DB_URL" ]] && fail "INGATIN_DB_URL tidak diset (periksa .env)"

# Mode B: jalankan psql di dalam container PostgreSQL.
MODE_B=0
PG_CONTAINER="${INGATIN_PG_CONTAINER:-}"
if [[ -n "$PG_CONTAINER" ]]; then
  MODE_B=1
elif [[ "$DB_URL" == *"@postgres:5432/"* ]]; then
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx 'ingatin-postgres'; then
    PG_CONTAINER="ingatin-postgres"; MODE_B=1
  else
    PG_CONTAINER="$(docker ps --filter 'label=com.docker.compose.service=postgres' \
      --format '{{.Names}}' 2>/dev/null | head -1)"
    [[ -n "$PG_CONTAINER" ]] && MODE_B=1 || fail "container PostgreSQL tidak ditemukan; set INGATIN_PG_CONTAINER"
  fi
fi

# Pisahkan kredensial.
if [[ "$DB_URL" =~ ^postgres(ql)?://([^:]+):([^@]+)@(.+)$ ]]; then
  DB_USER="${BASH_REMATCH[2]}"; DB_PASS="${BASH_REMATCH[3]}"; DB_REST="${BASH_REMATCH[4]}"
  SANITIZED_URL="postgresql://${DB_USER}@${DB_REST}"
else
  SANITIZED_URL="$DB_URL"; DB_PASS=""
fi
DB_NAME="${DB_REST%%\?*}"; DB_NAME="${DB_NAME##*/}"; DB_NAME="${DB_NAME:-ingatin}"

if [[ "$MODE_B" == "0" ]]; then
  DB_URL="${DB_URL/@postgres:5432\//@127.0.0.1:${INGATIN_PG_PORT:-5432}\/}"
  SANITIZED_URL="${SANITIZED_URL/@postgres:5432\//@127.0.0.1:${INGATIN_PG_PORT:-5432}\/}"
  command -v psql >/dev/null 2>&1 || fail "psql tidak ditemukan (Mode A). Install postgresql-client."
fi

# q <sql> — jalankan SQL terseleksi, kembalikan stdout.
q() {
  if [[ "$MODE_B" == "1" ]]; then
    docker exec -e PGPASSWORD="$DB_PASS" "$PG_CONTAINER" \
      psql -tAc "$1" -U "$DB_USER" -d "$DB_NAME"
  elif [[ -n "$DB_PASS" ]]; then
    PGPASSWORD="$DB_PASS" psql -tAc "$1" "$SANITIZED_URL"
  else
    psql -tAc "$1" "$SANITIZED_URL"
  fi
}

# x <sql> — jalankan SQL non-select (menampilkan baris terdampak).
x() {
  if [[ "$MODE_B" == "1" ]]; then
    docker exec -e PGPASSWORD="$DB_PASS" "$PG_CONTAINER" \
      psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" -c "$1"
  elif [[ -n "$DB_PASS" ]]; then
    PGPASSWORD="$DB_PASS" psql -v ON_ERROR_STOP=1 "$SANITIZED_URL" -c "$1"
  else
    psql -v ON_ERROR_STOP=1 "$SANITIZED_URL" -c "$1"
  fi
}

# ---------------------------------------------------------------------------
# Ringkasan sebelum
# ---------------------------------------------------------------------------
log "menganalisis item tanpa tim"
TOTAL_NULL="$(q "SELECT count(*) FROM work_items WHERE NOT is_deleted AND team_id IS NULL AND item_type IN ('task','daily_task')")"
FIXABLE="$(q "SELECT count(*) FROM work_items wi JOIN users u ON lower(u.username)=lower(wi.created_by) WHERE NOT wi.is_deleted AND wi.team_id IS NULL AND wi.item_type IN ('task','daily_task') AND u.team_id IS NOT NULL")"
ORPHAN="$(q "SELECT count(*) FROM work_items wi LEFT JOIN users u ON lower(u.username)=lower(wi.created_by) WHERE NOT wi.is_deleted AND wi.team_id IS NULL AND wi.item_type IN ('task','daily_task') AND (u.team_id IS NULL OR u.id IS NULL)")"

log "task/daily_task tanpa tim         : ${TOTAL_NULL:-0}"
log "dapat diperbaiki (pembuat bertim)  : ${FIXABLE:-0}"
log "tetap kosong (pembuat tanpa tim)   : ${ORPHAN:-0}"

if [[ "${FIXABLE:-0}" -eq 0 ]]; then
  log "tidak ada yang perlu diperbaiki."
  exit 0
fi

if [[ "$DRY_RUN" == "1" ]]; then
  warn "MODE --dry-run: tidak ada perubahan yang ditulis."
  q "SELECT wi.ref_no, wi.item_type, wi.created_by, u.team_id AS akan_diset_ke
     FROM work_items wi JOIN users u ON lower(u.username)=lower(wi.created_by)
     WHERE NOT wi.is_deleted AND wi.team_id IS NULL AND wi.item_type IN ('task','daily_task')
       AND u.team_id IS NOT NULL
     ORDER BY wi.created_at LIMIT 50"
  exit 0
fi

# ---------------------------------------------------------------------------
# Terapkan
# ---------------------------------------------------------------------------
log "mengisi team_id dari tim pembuat..."
x "UPDATE work_items wi
      SET team_id = u.team_id,
          updated_at = now()
     FROM users u
    WHERE NOT wi.is_deleted
      AND wi.team_id IS NULL
      AND wi.item_type IN ('task','daily_task')
      AND lower(u.username) = lower(wi.created_by)
      AND u.team_id IS NOT NULL"

AFTER="$(q "SELECT count(*) FROM work_items WHERE NOT is_deleted AND team_id IS NULL AND item_type IN ('task','daily_task')")"
log "selesai. sisa task/daily_task tanpa tim: ${AFTER:-0}"

if [[ "${AFTER:-0}" -gt 0 ]]; then
  warn "sisa ${AFTER} item tetap tanpa tim (pembuatnya tidak punya tim)."
  warn "Perbaiki lewat panel (edit item → pilih Tim) atau set tim pembuat lalu jalankan ulang."
fi
