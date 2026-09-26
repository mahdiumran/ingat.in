#!/usr/bin/env bash
#
# install.sh — pemasangan Ingat.in dengan satu perintah (Mode B: PostgreSQL container).
#
# Langkah:
#   1. periksa prasyarat (Docker, Compose, tool lain, port, disk, RAM)
#   2. buat .env berisi secret acak & port default (bila belum ada)
#   3. build image dan jalankan seluruh service
#   4. tunggu PostgreSQL, migrasi, dan API siap
#   5. (opsional) siapkan situs nginx untuk dashboard QR WAHA
#   6. tampilkan URL akses + kredensial admin
#
# Jalankan ulang aman (idempoten): .env yang ada tidak akan ditimpa.
#
# Opsi:
#   --no-build     lewati `docker compose build` (pakai image yang ada)
#   --no-qr-site   jangan sentuh konfigurasi nginx host
#   --reset-db     hapus volume pgdata lalu buat ulang database dari .env
#                  (DESTRUKTIF untuk data PostgreSQL container; pakai bila
#                  muncul error 'password authentication failed' 28P01)
#   -h | --help    tampilkan bantuan ini

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(cat VERSION 2>/dev/null || echo dev)"

NO_BUILD=0
NO_QR_SITE=0
RESET_DB=0
for arg in "$@"; do
  case "$arg" in
    --no-build)  NO_BUILD=1 ;;
    --no-qr-site) NO_QR_SITE=1 ;;
    --reset-db)  RESET_DB=1 ;;
    -h|--help)   sed -n '2,22p' "$0"; exit 0 ;;
    *) printf 'Argumen tidak dikenal: %s\n' "$arg" >&2; exit 1 ;;
  esac
done

log()  { printf '\033[1;36m[install]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[install]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[install] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. Prasyarat
# ---------------------------------------------------------------------------
log "memeriksa prasyarat"

command -v docker >/dev/null 2>&1 || fail "Docker tidak ditemukan. Install Docker Engine terlebih dahulu (https://docs.docker.com/engine/install/)."
docker info >/dev/null 2>&1 || fail "Docker daemon tidak berjalan atau tidak dapat diakses. Jalankan 'systemctl start docker' atau periksa izin user."

if docker compose version >/dev/null 2>&1; then
  DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  DC="docker-compose"
else
  fail "Docker Compose tidak ditemukan (butuh plugin 'docker compose' v2)."
fi
log "menggunakan: $DC"

command -v openssl >/dev/null 2>&1 || fail "openssl tidak ditemukan (dibutuhkan untuk membuat secret)."
command -v curl    >/dev/null 2>&1 || fail "curl tidak ditemukan (dibutuhkan untuk health check)."

# ---------------------------------------------------------------------------
# 1b. Sumber daya & port
# ---------------------------------------------------------------------------
# Disk minimal ~5 GB bebas untuk image, data, dan backup.
DISK_AVAIL_KB="$(df -Pk "$SCRIPT_DIR" 2>/dev/null | awk 'NR==2 {print $4}')"
if [[ -n "${DISK_AVAIL_KB:-}" ]]; then
  DISK_AVAIL_GB=$((DISK_AVAIL_KB / 1024 / 1024))
  if [[ "$DISK_AVAIL_GB" -lt 3 ]]; then
    warn "ruang disk hanya ${DISK_AVAIL_GB} GB bebas; disarankan >= 5 GB"
  else
    log "ruang disk bebas: ${DISK_AVAIL_GB} GB"
  fi
fi

# RAM: WAHA/Chromium butuh ~400–800 MB, plus PostgreSQL & API.
if [[ -r /proc/meminfo ]]; then
  MEM_MB="$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo)"
  if [[ "${MEM_MB:-0}" -lt 1500 ]]; then
    warn "RAM total ${MEM_MB} MB; disarankan >= 1500 MB (WAHA/Chromium)"
  else
    log "RAM total: ${MEM_MB} MB"
  fi
fi

# Port yang harus bebas di host (dapat diubah lewat .env).
check_port() {
  local port="$1" name="$2"
  if command -v ss >/dev/null 2>&1; then
    if ss -ltn "sport = :$port" 2>/dev/null | grep -q LISTEN; then
      warn "port $port ($name) sedang dipakai — ubah lewat .env atau hentikan service terkait"
      PORT_CONFLICT=1
    fi
  elif command -v netstat >/dev/null 2>&1; then
    if netstat -ltn 2>/dev/null | awk '{print $4}' | grep -q ":$port\$"; then
      warn "port $port ($name) sedang dipakai — ubah lewat .env atau hentikan service terkait"
      PORT_CONFLICT=1
    fi
  fi
}

PORT_CONFLICT=0
check_port 8091 "web"
check_port 8081 "api"
check_port 8082 "waha"
check_port 5432 "postgres"
if [[ "$PORT_CONFLICT" == "1" ]]; then
  if [[ -f .env ]]; then
    warn "melanjutkan dengan port dari .env yang sudah ada"
  else
    warn "port di atas dipakai proses lain. Anda dapat mengubahnya di .env setelah dibuat, lalu jalankan ulang ./install.sh"
  fi
fi

# ---------------------------------------------------------------------------
# 2. File .env
# ---------------------------------------------------------------------------
ENV_CREATED=0
ADMIN_PASSWORD=""
WAHA_API_KEY=""
WAHA_DASHBOARD_PASSWORD=""

# Nilai default port (dapat diubah manual setelah .env dibuat).
WEB_PORT=8091
API_PORT=8081
WAHA_PORT=8082
PG_PORT=5432

if [[ -f .env ]]; then
  log ".env sudah ada — tidak ditimpa"
  # Baca nilai yang sudah ada agar dapat ditampilkan di akhir.
  ADMIN_PASSWORD="$(grep -E '^INGATIN_ADMIN_PASSWORD=' .env | head -1 | cut -d= -f2- || true)"
  WAHA_API_KEY="$(grep -E '^WAHA_API_KEY=' .env | head -1 | cut -d= -f2- || true)"
  WAHA_DASHBOARD_PASSWORD="$(grep -E '^WAHA_DASHBOARD_PASSWORD=' .env | head -1 | cut -d= -f2- || true)"
  WEB_PORT="$(grep -E '^INGATIN_WEB_PORT=' .env | head -1 | cut -d= -f2- || echo 8091)"
  WAHA_PORT="$(grep -E '^INGATIN_WAHA_PORT=' .env | head -1 | cut -d= -f2- || echo 8082)"
else
  log "membuat .env dengan secret acak"

  SECRET_KEY="$(openssl rand -hex 32)"
  CREDENTIAL_KEY="$(openssl rand -base64 24 | tr -d '\n' | cut -c1-32)"
  DB_PASSWORD="$(openssl rand -hex 20)"
  ADMIN_PASSWORD="$(openssl rand -base64 12 | tr -d '\n' | tr '/+' 'Aa')"
  WAHA_API_KEY="$(openssl rand -hex 20)"
  WAHA_DASHBOARD_PASSWORD="$(openssl rand -base64 12 | tr -d '\n' | tr '/+' 'Aa')"

  # Pastikan credential key tepat 32 byte (persyaratan AES-256-GCM).
  if [[ ${#CREDENTIAL_KEY} -ne 32 ]]; then
    fail "gagal membuat INGATIN_CREDENTIAL_KEY tepat 32 byte (dapat ${#CREDENTIAL_KEY})"
  fi

  # Alamat QR dashboard: bila nginx host tersedia, pakai port 8010 pada IP server.
  HOST_IP_TMP="$(hostname -I 2>/dev/null | awk '{print $1}')"
  QR_URL=""
  if [[ -n "$HOST_IP_TMP" ]]; then
    QR_URL="http://${HOST_IP_TMP}:8010"
  fi

  cat > .env <<EOF
# Dibuat otomatis oleh install.sh pada $(date -u '+%Y-%m-%d %H:%M:%SZ')
# JANGAN commit file ini. Simpan salinan di tempat aman.

# --- Umum ---
INGATIN_APP_NAME=Ingat.in
INGATIN_APP_VERSION=${VERSION}
INGATIN_MODE=api
INGATIN_ADDR=0.0.0.0:8081
INGATIN_LOG_LEVEL=info
INGATIN_TIMEZONE=Asia/Jakarta
INGATIN_DATA_DIR=/app/data
INGATIN_FRONTEND_ORIGIN=http://localhost:${WEB_PORT}
INGATIN_TRUSTED_PROXIES=

# --- Port host (ubah bila bentrok) ---
INGATIN_WEB_PORT=${WEB_PORT}
INGATIN_API_PORT=${API_PORT}
INGATIN_WAHA_PORT=${WAHA_PORT}
INGATIN_PG_PORT=${PG_PORT}

# --- Keamanan ---
INGATIN_SECRET_KEY=${SECRET_KEY}
INGATIN_CREDENTIAL_KEY=${CREDENTIAL_KEY}

# --- Database (Mode B: PostgreSQL container; host = nama service 'postgres') ---
INGATIN_DB_URL=postgres://ingatin:${DB_PASSWORD}@postgres:5432/ingatin?sslmode=disable
INGATIN_DB_NAME=ingatin
INGATIN_DB_USER=ingatin
INGATIN_DB_PASSWORD=${DB_PASSWORD}

# --- Sesi (JWT 72 jam + refresh 30 hari) ---
INGATIN_ACCESS_TOKEN_MINUTES=4320
INGATIN_REFRESH_TOKEN_DAYS=30
INGATIN_SESSION_IDLE_TIMEOUT_MINUTES=0

# --- Admin awal ---
INGATIN_ADMIN_USERNAME=admin
INGATIN_ADMIN_EMAIL=
INGATIN_ADMIN_PASSWORD=${ADMIN_PASSWORD}

# --- WAHA (WhatsApp) ---
INGATIN_WAHA_BASE_URL=http://waha:3000
INGATIN_WAHA_API_KEY=${WAHA_API_KEY}
INGATIN_WAHA_QR_URL=${QR_URL}
WAHA_ENGINE=WEBJS
WAHA_API_KEY=${WAHA_API_KEY}
WAHA_DASHBOARD_USERNAME=noc
WAHA_DASHBOARD_PASSWORD=${WAHA_DASHBOARD_PASSWORD}

# --- Notifikasi ---
INGATIN_OUTBOX_BATCH_SIZE=50
INGATIN_OUTBOX_MAX_ATTEMPTS=5
INGATIN_OUTBOX_SENDER_INTERVAL_SECONDS=15
INGATIN_FANOUT_INTERVAL_SECONDS=60

# --- Telegram bot (inbound; aktif bila secret diisi) ---
INGATIN_TELEGRAM_WEBHOOK_SECRET=

# --- Retensi ---
INGATIN_AUDIT_RETENTION_DAYS=90
INGATIN_OUTBOX_RETENTION_DAYS=90
INGATIN_BACKUP_KEEP_DAYS=14
INGATIN_RETENTION_INTERVAL_HOURS=24

# --- Cadangan (F34) ---
INGATIN_BACKUP_DIR=
INGATIN_BACKUP_AUTO_ENABLED=false
INGATIN_BACKUP_SCHEDULE=0 2 * * *

# --- Sinkronisasi Google Spreadsheet (F18) ---
INGATIN_SHEET_SYNC_INTERVAL_SECONDS=30

# --- Lampiran (F21) ---
INGATIN_ATTACHMENTS_MAX_MB=50
INGATIN_ATTACHMENTS_ALLOWED_TYPES=image/png,image/jpeg,image/jpg,image/gif,image/webp,application/pdf,text/plain,text/csv,application/zip,application/gzip
EOF

  chmod 600 .env
  ENV_CREATED=1
  log ".env dibuat (mode 600)"
fi

# ---------------------------------------------------------------------------
# 2b. Deteksi volume PostgreSQL yang tidak sinkron (penyebab umum 28P01)
#
# POSTGRES_PASSWORD hanya diterapkan saat volume pgdata PERTAMA kali dibuat.
# Bila pgdata sudah ada dari percobaan sebelumnya dengan password berbeda,
# migrate akan gagal: 'password authentication failed for user "ingatin"'.
# Kita deteksi volume itu dan (a) tawarkan reset, atau (b) tandai --reset-db.
# ---------------------------------------------------------------------------
PROJECT_NAME="$($DC config --format json 2>/dev/null \
  | grep -oE '"name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 \
  | sed -E 's/.*"([^"]*)"$/\1/')"
PROJECT_NAME="${PROJECT_NAME:-$(basename "$SCRIPT_DIR")}"
PG_VOLUME="${PROJECT_NAME}_pgdata"

if docker volume inspect "$PG_VOLUME" >/dev/null 2>&1; then
  if [[ "$RESET_DB" == "1" ]]; then
    warn "--reset-db: menghapus volume PostgreSQL '$PG_VOLUME' (data DB container hilang)"
    $DC down --remove-orphans >/dev/null 2>&1 || true
    docker volume rm "$PG_VOLUME" >/dev/null 2>&1 || warn "gagal menghapus volume $PG_VOLUME"
    log "volume '$PG_VOLUME' dihapus; database akan dibuat ulang dari .env"
  else
    warn "volume PostgreSQL '$PG_VOLUME' sudah ada."
    warn "Jika muncul error 'password authentication failed' (28P01), volume ini"
    warn "kemungkinan dibuat dengan password lama. Perbaikannya:"
    warn "    docker compose down && docker volume rm $PG_VOLUME && ./install.sh"
    warn "atau jalankan ulang:  ./install.sh --reset-db"
  fi
fi

# ---------------------------------------------------------------------------
# 3. Build & jalankan
# ---------------------------------------------------------------------------
log "memeriksa konfigurasi compose"
$DC config >/dev/null || fail "konfigurasi docker-compose.yml tidak valid"

if [[ "$NO_BUILD" == "0" ]]; then
  log "membangun image (mungkin perlu beberapa menit pada build pertama)"
  $DC build --pull
fi

log "menjalankan service"
$DC up -d

# ---------------------------------------------------------------------------
# 3b. Tunggu PostgreSQL, migrasi, dan API siap
#
# Nama container mengikuti pola Compose (<project>-<service>-<n>) atau dapat
# diambil lewat `docker compose ps`. Helper di bawah mengabstraksi perbedaan ini.
# ---------------------------------------------------------------------------
container_id_for() {
  # $1 = nama service (postgres, migrate, api, ...)
  $DC ps -aq "$1" 2>/dev/null | head -1
}

log "menunggu PostgreSQL siap"
for i in $(seq 1 60); do
  PG_CID="$(container_id_for postgres)"
  if [[ -n "$PG_CID" ]] && \
     docker inspect --format '{{.State.Health.Status}}' "$PG_CID" 2>/dev/null | grep -q healthy; then
    log "PostgreSQL siap"
    break
  fi
  if [[ $i -eq 60 ]]; then
    warn "PostgreSQL belum sehat setelah 60 detik; periksa: $DC logs postgres"
  fi
  sleep 2
done

log "menunggu migrasi selesai"
for i in $(seq 1 120); do
  MIG_CID="$(container_id_for migrate)"
  MIGRATE_STATE="$(docker inspect --format '{{.State.Status}}' "$MIG_CID" 2>/dev/null || echo unknown)"
  if [[ -n "$MIG_CID" && "$MIGRATE_STATE" == "exited" ]]; then
    MIGRATE_EXIT="$(docker inspect --format '{{.State.ExitCode}}' "$MIG_CID" 2>/dev/null || echo 1)"
    if [[ "$MIGRATE_EXIT" == "0" ]]; then
      log "migrasi selesai"
    else
      warn "migrasi keluar dengan kode $MIGRATE_EXIT; periksa: $DC logs migrate"
    fi
    break
  fi
  if [[ $i -eq 120 ]]; then
    warn "migrasi belum selesai setelah 240 detik; periksa: $DC logs migrate"
  fi
  sleep 2
done

API_PORT_VAL="$(grep -E '^INGATIN_API_PORT=' .env | head -1 | cut -d= -f2- || true)"
API_PORT_VAL="${API_PORT_VAL:-8081}"
log "menunggu API siap di 127.0.0.1:${API_PORT_VAL}"
API_READY=0
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${API_PORT_VAL}/api/health" >/dev/null 2>&1; then
    log "API siap"
    API_READY=1
    break
  fi
  if [[ $i -eq 60 ]]; then
    warn "API belum merespons setelah 120 detik; periksa: $DC logs api migrate"
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# 4. Situs nginx untuk dashboard QR WAHA (opsional)
#
# WAHA hanya listen di 127.0.0.1:<port> sehingga tidak dapat diakses dari LAN.
# Situs ini mem-proxy-nya pada port 8010 dengan basic auth. Penting: berkas
# htpasswd DIREGENERASI setiap kali install dijalankan agar selalu sinkron
# dengan WAHA_DASHBOARD_PASSWORD di .env (temuan: password di .env diganti
# tetapi htpasswd lama membuat akses 401).
# ---------------------------------------------------------------------------
QR_SITE_ACTIVE=0
if [[ "$NO_QR_SITE" == "0" ]] && command -v nginx >/dev/null 2>&1 && [[ $EUID -eq 0 ]]; then
  # Port QR tidak memakai INGATIN_WEB_PORT (itu untuk web UI). Tetap 8010.
  QR_PORT=8010

  if [[ -n "$WAHA_DASHBOARD_PASSWORD" ]]; then
    log "menyiapkan situs nginx QR WAHA pada port ${QR_PORT}"
    if command -v htpasswd >/dev/null 2>&1; then
      htpasswd -cB -b /etc/nginx/.htpasswd-ingatin "${WAHA_DASHBOARD_USERNAME:-noc}" "$WAHA_DASHBOARD_PASSWORD" >/dev/null 2>&1
    else
      printf '%s:%s\n' "${WAHA_DASHBOARD_USERNAME:-noc}" "$(openssl passwd -apr1 "$WAHA_DASHBOARD_PASSWORD")" \
        > /etc/nginx/.htpasswd-ingatin
    fi
    chown root:www-data /etc/nginx/.htpasswd-ingatin 2>/dev/null || true
    chmod 640 /etc/nginx/.htpasswd-ingatin

    cat > /etc/nginx/sites-available/ingatin-waha <<NGINXEOF
# Ingat.in — dashboard QR WAHA (dibuat oleh install.sh).
server {
    listen ${QR_PORT};
    listen [::]:${QR_PORT};
    server_name _;

    auth_basic           "Ingat.in WAHA";
    auth_basic_user_file /etc/nginx/.htpasswd-ingatin;

    location / {
        proxy_pass http://127.0.0.1:${WAHA_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host              \$host;
        proxy_set_header X-Real-IP         \$remote_addr;
        proxy_set_header X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade    \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }
}
NGINXEOF

    ln -sf /etc/nginx/sites-available/ingatin-waha /etc/nginx/sites-enabled/ingatin-waha
    if nginx -t >/dev/null 2>&1; then
      systemctl reload nginx 2>/dev/null || nginx -s reload 2>/dev/null || true
      log "situs QR WAHA aktif di :${QR_PORT}"
      QR_SITE_ACTIVE=1
    else
      warn "konfigurasi nginx tidak valid — situs QR tidak diaktifkan"
      rm -f /etc/nginx/sites-enabled/ingatin-waha
    fi
  fi
else
  if [[ "$NO_QR_SITE" == "0" ]]; then
    warn "nginx host tidak tersedia atau bukan root — situs QR WAHA dilewati (dashboard tetap dapat diakses via ssh -L ${WAHA_PORT}:127.0.0.1:${WAHA_PORT})"
  fi
fi

# ---------------------------------------------------------------------------
# 5. Ringkasan
# ---------------------------------------------------------------------------
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[[ -z "$HOST_IP" ]] && HOST_IP="<IP-SERVER>"

echo
echo "==========================================================================="
echo " Ingat.in ${VERSION} — pemasangan selesai"
echo "==========================================================================="
echo
echo " Web UI     : http://${HOST_IP}:${WEB_PORT}"
echo " API (lokal): http://127.0.0.1:${API_PORT_VAL}/api/health"
echo " WAHA (lokal): http://127.0.0.1:${WAHA_PORT}"
if [[ "$QR_SITE_ACTIVE" == "1" ]]; then
  echo " Dashboard QR: http://${HOST_IP}:8010  (basic auth: ${WAHA_DASHBOARD_USERNAME:-noc})"
fi
echo
if [[ "$ENV_CREATED" == "1" ]]; then
  echo " Kredensial admin awal (simpan di tempat aman):"
  echo "   Username : admin"
  echo "   Password : ${ADMIN_PASSWORD}"
  echo
  echo " Kredensial WAHA:"
  echo "   API key           : ${WAHA_API_KEY}"
  echo "   Dashboard user    : ${WAHA_DASHBOARD_USERNAME:-noc}"
  echo "   Dashboard password: ${WAHA_DASHBOARD_PASSWORD}"
  echo
  echo " Semua nilai juga tersimpan di .env (mode 600)."
else
  echo " .env sudah ada sebelumnya — kredensial tidak ditampilkan."
  echo " Lihat .env untuk INGATIN_ADMIN_PASSWORD dan WAHA_API_KEY."
fi
echo
if [[ "$API_READY" == "0" ]]; then
  warn "API belum terverifikasi sehat. Periksa: $DC logs api migrate"
  echo
fi
echo " Langkah berikutnya:"
echo "   1. Login ke Web UI dan GANTI password admin."
echo "   2. Buka dashboard WAHA untuk scan QR (lihat DEPLOYMENT.md §8)."
echo "   3. Isi token Telegram & target notifikasi dari panel (F3/F4)."
echo "   4. Bila diakses dari LAN, set INGATIN_FRONTEND_ORIGIN=http://${HOST_IP}:${WEB_PORT} di .env"
echo "      lalu jalankan: $DC up -d api worker"
echo
echo " Perintah berguna:"
echo "   $DC ps"
echo "   $DC logs -f api worker"
echo "   ./scripts/smoke.sh"
echo "   ./scripts/backup.sh"
echo
echo " Dokumentasi: README.md · PLAN.md · DEPLOYMENT.md · OPERATIONS.md"
echo "==========================================================================="
