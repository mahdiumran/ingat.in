#!/usr/bin/env bash
#
# install.sh — pemasangan Ingat.in dengan satu perintah.
#
# Langkah:
#   1. periksa Docker & Docker Compose
#   2. buat .env berisi secret acak (bila belum ada)
#   3. siapkan role & database PostgreSQL
#   4. build image dan jalankan seluruh service
#   5. tampilkan URL akses + kredensial admin
#
# Jalankan ulang aman (idempoten): .env yang ada tidak akan ditimpa.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(cat VERSION 2>/dev/null || echo dev)"

log()  { printf '\033[1;36m[install]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[install]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[install] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. Prasyarat
# ---------------------------------------------------------------------------
log "memeriksa prasyarat"

command -v docker >/dev/null 2>&1 || fail "Docker tidak ditemukan. Install Docker Engine terlebih dahulu."

if docker compose version >/dev/null 2>&1; then
  DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  DC="docker-compose"
else
  fail "Docker Compose tidak ditemukan (butuh plugin 'docker compose' v2)."
fi
log "menggunakan: $DC"

command -v openssl >/dev/null 2>&1 || fail "openssl tidak ditemukan (dibutuhkan untuk membuat secret)."

# ---------------------------------------------------------------------------
# 2. File .env
# ---------------------------------------------------------------------------
ENV_CREATED=0
ADMIN_PASSWORD=""
WAHA_API_KEY=""
WAHA_DASHBOARD_PASSWORD=""

if [[ -f .env ]]; then
  log ".env sudah ada — tidak ditimpa"
  # Baca nilai yang sudah ada agar dapat ditampilkan di akhir.
  ADMIN_PASSWORD="$(grep -E '^INGATIN_ADMIN_PASSWORD=' .env | head -1 | cut -d= -f2- || true)"
  WAHA_API_KEY="$(grep -E '^WAHA_API_KEY=' .env | head -1 | cut -d= -f2- || true)"
  WAHA_DASHBOARD_PASSWORD="$(grep -E '^WAHA_DASHBOARD_PASSWORD=' .env | head -1 | cut -d= -f2- || true)"
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

  cat > .env <<EOF
# Dibuat otomatis oleh install.sh pada $(date -u '+%Y-%m-%d %H:%M:%SZ')
# JANGAN commit file ini. Simpan salinan di tempat aman.

# --- Umum ---
INGATIN_APP_NAME=Ingat.in
INGATIN_APP_VERSION=${VERSION}
INGATIN_MODE=api
INGATIN_ADDR=127.0.0.1:8081
INGATIN_WEB_PORT=8091
INGATIN_LOG_LEVEL=info
INGATIN_TIMEZONE=Asia/Jakarta
INGATIN_DATA_DIR=/app/data
INGATIN_FRONTEND_ORIGIN=http://localhost:8091
INGATIN_TRUSTED_PROXIES=

# --- Keamanan ---
INGATIN_SECRET_KEY=${SECRET_KEY}
INGATIN_CREDENTIAL_KEY=${CREDENTIAL_KEY}

# --- Database (Mode A: PostgreSQL host) ---
INGATIN_DB_URL=postgres://ingatin:${DB_PASSWORD}@127.0.0.1:5432/ingatin?sslmode=disable
INGATIN_DB_NAME=ingatin
INGATIN_DB_USER=ingatin
INGATIN_DB_PASSWORD=${DB_PASSWORD}
INGATIN_PG_HOST=127.0.0.1
INGATIN_PG_PORT=5432
INGATIN_PG_SUPERUSER=postgres

# --- Sesi (JWT 72 jam + refresh 30 hari) ---
INGATIN_ACCESS_TOKEN_MINUTES=4320
INGATIN_REFRESH_TOKEN_DAYS=30
INGATIN_SESSION_IDLE_TIMEOUT_MINUTES=0

# --- Admin awal ---
INGATIN_ADMIN_USERNAME=admin
INGATIN_ADMIN_EMAIL=
INGATIN_ADMIN_PASSWORD=${ADMIN_PASSWORD}

# --- WAHA (WhatsApp) ---
INGATIN_WAHA_BASE_URL=http://127.0.0.1:8082
INGATIN_WAHA_API_KEY=${WAHA_API_KEY}
WAHA_ENGINE=WEBJS
WAHA_API_KEY=${WAHA_API_KEY}
WAHA_DASHBOARD_USERNAME=noc
WAHA_DASHBOARD_PASSWORD=${WAHA_DASHBOARD_PASSWORD}

# --- Notifikasi ---
INGATIN_OUTBOX_BATCH_SIZE=50
INGATIN_OUTBOX_MAX_ATTEMPTS=5
INGATIN_OUTBOX_SENDER_INTERVAL_SECONDS=15
INGATIN_FANOUT_INTERVAL_SECONDS=60

# --- Retensi ---
INGATIN_AUDIT_RETENTION_DAYS=90
INGATIN_OUTBOX_RETENTION_DAYS=90
INGATIN_BACKUP_KEEP_DAYS=14
INGATIN_RETENTION_INTERVAL_HOURS=24
EOF

  chmod 600 .env
  ENV_CREATED=1
  log ".env dibuat (mode 600)"
fi

# ---------------------------------------------------------------------------
# 3. Database
# ---------------------------------------------------------------------------
log "menyiapkan database PostgreSQL"
if [[ -x ./scripts/provision-db.sh ]]; then
  ./scripts/provision-db.sh
else
  warn "scripts/provision-db.sh tidak ditemukan — lewati provisioning database"
fi

# ---------------------------------------------------------------------------
# 4. Build & jalankan
# ---------------------------------------------------------------------------
log "membangun image (mungkin perlu beberapa menit pada build pertama)"
$DC build --pull

log "menjalankan service"
$DC up -d

log "menunggu migrasi dan API siap"
for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:8081/api/health >/dev/null 2>&1; then
    log "API siap"
    break
  fi
  if [[ $i -eq 60 ]]; then
    warn "API belum merespons setelah 60 detik; periksa: $DC logs api migrate"
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# 4b. Situs nginx untuk dashboard QR WAHA
#
# WAHA hanya listen di 127.0.0.1:8082 sehingga tidak dapat diakses dari LAN.
# Situs ini mem-proxy-nya pada port 8010 dengan basic auth. Penting: berkas
# htpasswd DIREGENERASI setiap kali install dijalankan agar selalu sinkron
# dengan WAHA_DASHBOARD_PASSWORD di .env (temuan: password di .env diganti
# tetapi htpasswd lama membuat akses 401).
# ---------------------------------------------------------------------------
if command -v nginx >/dev/null 2>&1 && [[ $EUID -eq 0 ]]; then
  WAHA_PORT="$(grep -E '^INGATIN_WEB_PORT=' .env | head -1 | cut -d= -f2- || true)"
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
        proxy_pass http://127.0.0.1:8082;
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
    else
      warn "konfigurasi nginx tidak valid — situs QR tidak diaktifkan"
    fi
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
echo " Web UI     : http://${HOST_IP}:8091"
echo " API (lokal): http://127.0.0.1:8081/api/health"
echo " WAHA (lokal): http://127.0.0.1:8082"
echo
if [[ "$ENV_CREATED" == "1" ]]; then
  echo " Kredensial admin awal (simpan di tempat aman):"
  echo "   Username : admin"
  echo "   Password : ${ADMIN_PASSWORD}"
  echo
  echo " Kredensial WAHA:"
  echo "   API key           : ${WAHA_API_KEY}"
  echo "   Dashboard user    : noc"
  echo "   Dashboard password: ${WAHA_DASHBOARD_PASSWORD}"
  echo
  echo " Semua nilai juga tersimpan di .env (mode 600)."
else
  echo " .env sudah ada sebelumnya — kredensial tidak ditampilkan."
  echo " Lihat .env untuk INGATIN_ADMIN_PASSWORD dan WAHA_API_KEY."
fi
echo
echo " Langkah berikutnya:"
echo "   1. Login ke Web UI dan GANTI password admin."
echo "   2. Buka dashboard WAHA untuk scan QR (lihat DEPLOYMENT.md §8)."
echo "   3. Isi token Telegram & target notifikasi dari panel (F3/F4)."
echo
echo " Perintah berguna:"
echo "   $DC ps"
echo "   $DC logs -f api worker"
echo "   ./scripts/smoke.sh"
echo "   ./scripts/backup.sh"
echo
echo " Dokumentasi: README.md · PLAN.md · DEPLOYMENT.md · OPERATIONS.md"
echo "==========================================================================="
