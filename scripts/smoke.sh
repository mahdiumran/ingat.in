#!/usr/bin/env bash
#
# smoke.sh — pemeriksaan cepat kesehatan Ingat.in.
#
# Memverifikasi: container, endpoint API, tabel & seed database, serta proxy web.
# Keluar dengan kode non-nol bila ada pemeriksaan yang gagal.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

API_BASE="${API_BASE:-http://127.0.0.1:8081}"
WEB_BASE="${WEB_BASE:-http://127.0.0.1:8091}"

PASS=0
FAIL=0

ok()   { printf '  \033[1;32mOK\033[0m   %s\n' "$*"; PASS=$((PASS + 1)); }
bad()  { printf '  \033[1;31mGAGAL\033[0m %s\n' "$*"; FAIL=$((FAIL + 1)); }
skip() { printf '  \033[1;33mLEWAT\033[0m %s\n' "$*"; }
head() { printf '\n\033[1;36m%s\033[0m\n' "$*"; }

# Muat .env bila ada.
if [[ -f "$PROJECT_DIR/.env" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key="$(echo "$key" | tr -d '[:space:]')"
    [[ -z "${!key:-}" ]] && export "$key=$value"
  done < <(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$PROJECT_DIR/.env" || true)
fi

# ---------------------------------------------------------------------------
head "1. Container"
# ---------------------------------------------------------------------------
if command -v docker >/dev/null 2>&1; then
  RUNNING="$(docker ps --format '{{.Names}}' 2>/dev/null | grep -c '^ingatin-' || true)"
  if [[ "$RUNNING" -gt 0 ]]; then
    ok "$RUNNING container ingatin-* berjalan"
    docker ps --format '  {{.Names}}\t{{.Status}}' 2>/dev/null | grep '^  ingatin-' || true
  else
    bad "tidak ada container ingatin-* yang berjalan"
  fi
else
  skip "docker tidak tersedia"
fi

# ---------------------------------------------------------------------------
head "2. API"
# ---------------------------------------------------------------------------
# http_code_from mengembalikan kode HTTP, atau "000" bila tidak dapat dihubungi.
http_code_from() {
  local code
  code="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' "$1" 2>/dev/null)" || code="000"
  [[ -z "$code" ]] && code="000"
  printf '%s' "$code"
}

HEALTH="$(curl -fsS --max-time 5 "$API_BASE/api/health" 2>/dev/null || true)"
if [[ -z "$HEALTH" ]]; then
  bad "GET $API_BASE/api/health tidak merespons"
else
  if echo "$HEALTH" | grep -q '"status":"ok"'; then
    ok "health: ok"
  else
    bad "health tidak ok: $HEALTH"
  fi
  if echo "$HEALTH" | grep -q '"database":"ok"'; then
    ok "database: ok"
  else
    bad "database tidak ok: $HEALTH"
  fi
fi

VERSION="$(curl -fsS --max-time 5 "$API_BASE/api/version" 2>/dev/null || true)"
if [[ -z "$VERSION" ]]; then
  bad "GET $API_BASE/api/version tidak merespons"
else
  ok "version: $(echo "$VERSION" | tr -d '\n' | cut -c1-110)"
  SCHEMA="$(echo "$VERSION" | grep -o '"schema_version":[0-9]*' | cut -d: -f2)"
  if [[ -n "$SCHEMA" && "$SCHEMA" -ge 2 ]]; then
    ok "versi skema: $SCHEMA"
  else
    bad "versi skema tidak sesuai (dapat: ${SCHEMA:-kosong}, ingin >= 2)"
  fi
fi

# Endpoint tidak dikenal harus 404 berformat JSON.
NOTFOUND="$(http_code_from "$API_BASE/api/tidak-ada")"
if [[ "$NOTFOUND" == "404" ]]; then
  ok "endpoint tak dikenal mengembalikan 404"
else
  bad "endpoint tak dikenal mengembalikan $NOTFOUND (ingin 404)"
fi

# F18: endpoint sinkronisasi spreadsheet harus dilindungi autentikasi.
SHEET_NOAUTH="$(http_code_from "$API_BASE/api/sheet-sync")"
if [[ "$SHEET_NOAUTH" == "401" ]]; then
  ok "GET /api/sheet-sync tanpa token mengembalikan 401"
else
  bad "GET /api/sheet-sync tanpa token mengembalikan $SHEET_NOAUTH (ingin 401)"
fi

# F20: endpoint KPI harus dilindungi autentikasi.
KPI_NOAUTH="$(http_code_from "$API_BASE/api/kpi/sla")"
if [[ "$KPI_NOAUTH" == "401" ]]; then
  ok "GET /api/kpi/sla tanpa token mengembalikan 401"
else
  bad "GET /api/kpi/sla tanpa token mengembalikan $KPI_NOAUTH (ingin 401)"
fi

# ---------------------------------------------------------------------------
head "3. Database"
# ---------------------------------------------------------------------------
# Pada Mode B (PostgreSQL container), INGATIN_DB_URL menunjuk ke host 'postgres'
# yang hanya dapat di-resolve dari dalam network Compose. Untuk pemeriksaan dari
# host, ganti host:port container (postgres:5432) menjadi loopback + port host.
DB_CHECK_URL="${INGATIN_DB_URL:-}"
DB_CHECK_URL="${DB_CHECK_URL/@postgres:5432\//@127.0.0.1:${INGATIN_PG_PORT:-5432}\/}"
if command -v psql >/dev/null 2>&1; then
  if [[ "$DB_CHECK_URL" =~ ^postgres(ql)?://([^:]+):([^@]+)@(.+)$ ]]; then
    DB_USER="${BASH_REMATCH[2]}"
    DB_PASS="${BASH_REMATCH[3]}"
    DB_REST="${BASH_REMATCH[4]}"
    SANITIZED="postgresql://${DB_USER}@${DB_REST}"
    export PGPASSWORD="$DB_PASS"

    q() { psql -tAc "$1" "$SANITIZED" 2>/dev/null || true; }

    TABLES="$(q "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
    if [[ -n "$TABLES" && "$TABLES" -ge 30 ]]; then
      ok "tabel pada schema public: $TABLES"
    else
      bad "jumlah tabel = ${TABLES:-?} (ingin >= 30)"
    fi

    ORG="$(q 'SELECT count(*) FROM organizations')"
    TEAMS="$(q 'SELECT count(*) FROM teams')"
    WF="$(q 'SELECT count(*) FROM workflow_definitions')"
    POL="$(q 'SELECT count(*) FROM escalation_policies')"
    TGT="$(q 'SELECT count(*) FROM notification_targets')"
    TPL="$(q 'SELECT count(*) FROM notification_templates')"

    [[ "${ORG:-0}" -ge 1 ]]   && ok "seed organisasi: $ORG"     || bad "seed organisasi kosong"
    [[ "${TEAMS:-0}" -ge 3 ]] && ok "seed team: $TEAMS"          || bad "seed team = ${TEAMS:-0} (ingin >= 3)"
    [[ "${WF:-0}" -ge 7 ]]    && ok "seed workflow: $WF"         || bad "seed workflow = ${WF:-0} (ingin >= 7)"
    [[ "${POL:-0}" -ge 2 ]]   && ok "seed policy eskalasi: $POL" || bad "seed policy = ${POL:-0} (ingin >= 2)"
    [[ "${TGT:-0}" -ge 1 ]]   && ok "seed target notifikasi: $TGT" || bad "seed target kosong"
    [[ "${TPL:-0}" -ge 10 ]]  && ok "seed template notifikasi: $TPL" || bad "seed template = ${TPL:-0} (ingin >= 10)"

    # Keunikan template harus ditegakkan untuk baris ber-channel maupun tanpa channel.
    # Tanpa indeks unik parsial, ON CONFLICT tidak akan mendeteksi baris channel NULL
    # sehingga seed yang dijalankan ulang menambah duplikat (temuan A11).
    HAS_PARTIAL_IDX="$(q "SELECT count(*) FROM pg_indexes WHERE tablename='notification_templates' AND indexname='ux_notification_templates_key_no_channel'")"
    if [[ "${HAS_PARTIAL_IDX:-0}" -ge 1 ]]; then
      ok "indeks unik parsial template (channel NULL) tersedia"
    else
      bad "indeks ux_notification_templates_key_no_channel tidak ditemukan (migrasi 00003 belum diterapkan?)"
    fi

    DUP_TPL="$(q "SELECT count(*) FROM (SELECT key, COALESCE(channel,'') AS ch FROM notification_templates GROUP BY key, COALESCE(channel,'') HAVING count(*) > 1) x")"
    if [[ "${DUP_TPL:-1}" -eq 0 ]]; then
      ok "tidak ada template duplikat per (key, channel)"
    else
      bad "ditemukan ${DUP_TPL} pasangan (key, channel) template yang duplikat"
    fi

    # Nomor referensi untuk trial 3 hari harus berisi H-0 dan LATE.
    TRIAL="$(q "SELECT offsets_json::text FROM escalation_policies WHERE name='TRIAL-3D'")"
    if echo "$TRIAL" | grep -q 'H-0' && echo "$TRIAL" | grep -q 'LATE-1H'; then
      ok "policy TRIAL-3D memuat H-0 dan LATE-1H"
    else
      bad "policy TRIAL-3D tidak lengkap: ${TRIAL:-kosong}"
    fi

    RFS_POL="$(q "SELECT offsets_json::text FROM escalation_policies WHERE name='RFS-DEFAULT'")"
    if echo "$RFS_POL" | grep -q 'H-7' && echo "$RFS_POL" | grep -q 'LATE-4H'; then
      ok "policy RFS-DEFAULT memuat H-7 dan LATE-4H"
    else
      bad "policy RFS-DEFAULT tidak lengkap: ${RFS_POL:-kosong}"
    fi

    # F12: master data & matriks izin.
    MD_TABLES="$(q "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('master_data','master_data_kinds','role_permissions')")"
    if [[ "${MD_TABLES:-0}" -eq 3 ]]; then
      ok "tabel master data & role_permissions tersedia"
    else
      bad "tabel master data tidak lengkap (ditemukan ${MD_TABLES:-0}/3; migrasi 00005 belum diterapkan?)"
    fi

    MD_KINDS="$(q "SELECT count(*) FROM master_data_kinds")"
    [[ "${MD_KINDS:-0}" -ge 7 ]] && ok "kelompok master data terdaftar: $MD_KINDS" \
      || bad "kelompok master data = ${MD_KINDS:-0} (ingin >= 7)"

    # Kelompok customer wajib ada (dipakai fitur pelanggan).
    CUST_KIND="$(q "SELECT count(*) FROM master_data_kinds WHERE kind='customer'")"
    [[ "${CUST_KIND:-0}" -ge 1 ]] && ok "kelompok 'customer' terdaftar" \
      || bad "kelompok 'customer' tidak ada (migrasi 00006 belum diterapkan?)"

    MD_ENTRIES="$(q "SELECT count(*) FROM master_data")"
    [[ "${MD_ENTRIES:-0}" -ge 8 ]] && ok "entri master data awal: $MD_ENTRIES" \
      || bad "entri master data = ${MD_ENTRIES:-0} (ingin >= 8)"

    # Admin harus selalu punya seluruh izin.
    ADMIN_PERMS="$(q "SELECT count(*) FROM role_permissions WHERE role='admin' AND allowed")"
    [[ "${ADMIN_PERMS:-0}" -ge 4 ]] && ok "izin admin lengkap: $ADMIN_PERMS" \
      || bad "izin admin = ${ADMIN_PERMS:-0} (ingin >= 4)"

    # F13+: status Todo memakai diksi baru (migrasi 00007).
    TASK_STATES="$(q "SELECT states_json::text FROM workflow_definitions WHERE item_type='task'")"
    if echo "$TASK_STATES" | grep -q 'accepted' && echo "$TASK_STATES" | grep -q 'on_progress' \
       && echo "$TASK_STATES" | grep -q 'closed'; then
      ok "status Todo memakai accepted/on_progress/expired/canceled/closed"
    else
      bad "status Todo belum diperbarui: ${TASK_STATES:-kosong} (migrasi 00007?)"
    fi

    # Tidak boleh ada sisa status Todo lama pada data.
    OLD_TASK_STATUS="$(q "SELECT count(*) FROM work_items WHERE item_type='task' AND status IN ('open','in_progress','blocked','done','cancelled')")"
    if [[ "${OLD_TASK_STATUS:-1}" -eq 0 ]]; then
      ok "tidak ada Todo dengan status lama"
    else
      bad "ditemukan ${OLD_TASK_STATUS} Todo berstatus lama"
    fi

    # Template notifikasi Telegram & WA tersedia untuk editor Master Data.
    TPL_TG="$(q "SELECT count(*) FROM notification_templates WHERE channel='telegram'")"
    TPL_WA="$(q "SELECT count(*) FROM notification_templates WHERE channel='whatsapp'")"
    [[ "${TPL_TG:-0}" -ge 5 ]] && ok "template Telegram: $TPL_TG" || bad "template Telegram = ${TPL_TG:-0} (ingin >= 5)"
    [[ "${TPL_WA:-0}" -ge 5 ]] && ok "template WhatsApp: $TPL_WA" || bad "template WhatsApp = ${TPL_WA:-0} (ingin >= 5)"

    # F15: jenis gangguan, warna tag, subkategori tiket (migrasi 00008).
    INC_KIND="$(q "SELECT count(*) FROM master_data WHERE kind='incident_type'")"
    [[ "${INC_KIND:-0}" -ge 5 ]] && ok "master jenis gangguan: $INC_KIND" \
      || bad "jenis gangguan = ${INC_KIND:-0} (ingin >= 5; migrasi 00008?)"

    TAG_COLORS="$(q "SELECT count(*) FROM master_data WHERE kind='tag_color'")"
    [[ "${TAG_COLORS:-0}" -ge 7 ]] && ok "warna tag: $TAG_COLORS" \
      || bad "warna tag = ${TAG_COLORS:-0} (ingin >= 7)"

    SUBKAT="$(q "SELECT count(*) FROM master_data WHERE kind='ticket_category' AND parent_id IS NOT NULL")"
    [[ "${SUBKAT:-0}" -ge 3 ]] && ok "subkategori tiket (hierarki): $SUBKAT" \
      || bad "subkategori tiket = ${SUBKAT:-0} (ingin >= 3)"

    INC_COL="$(q "SELECT count(*) FROM information_schema.columns WHERE table_name='ticket_details' AND column_name='incident_type'")"
    [[ "${INC_COL:-0}" -eq 1 ]] && ok "kolom ticket_details.incident_type tersedia" \
      || bad "kolom incident_type tidak ada (migrasi 00008 belum diterapkan?)"

    # F16: deskripsi dilampirkan pada notifikasi (migrasi 00009).
    DESC_TPL="$(q "SELECT count(*) FROM notification_templates WHERE body_tpl LIKE '%Deskripsi :%' AND key IN ('TODO_CREATED','REMINDER_OFFSET','RFS_UPCOMING')")"
    [[ "${DESC_TPL:-0}" -ge 3 ]] && ok "deskripsi dilampirkan pada template notifikasi: $DESC_TPL" \
      || bad "template notifikasi tanpa deskripsi = ${DESC_TPL:-0} (ingin >= 3; migrasi 00009?)"

    # F17: Daily Task (item_type=daily_task; migrasi 00010).
    DAILY_WF="$(q "SELECT count(*) FROM workflow_definitions WHERE item_type='daily_task'")"
    [[ "${DAILY_WF:-0}" -ge 1 ]] && ok "workflow Daily Task terdaftar" \
      || bad "workflow daily_task tidak ada (migrasi 00010 belum diterapkan?)"

    DAILY_CHECK="$(q "SELECT count(*) FROM pg_constraint WHERE conname='work_items_item_type_check' AND pg_get_constraintdef(oid) LIKE '%daily_task%'")"
    [[ "${DAILY_CHECK:-0}" -ge 1 ]] && ok "CHECK item_type menerima daily_task" \
      || bad "constraint item_type belum memuat daily_task (migrasi 00010?)"

    DAILY_IDX="$(q "SELECT count(*) FROM pg_indexes WHERE indexname='ix_work_items_daily'")"
    [[ "${DAILY_IDX:-0}" -ge 1 ]] && ok "indeks ix_work_items_daily tersedia" \
      || bad "indeks ix_work_items_daily tidak ditemukan (migrasi 00010?)"

    # F18: sinkronisasi Google Spreadsheet (migrasi 00011).
    SHEET_TABLES="$(q "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('sheet_sync_config','sheet_sync_queue')")"
    [[ "${SHEET_TABLES:-0}" -eq 2 ]] && ok "tabel sinkronisasi spreadsheet tersedia" \
      || bad "tabel sheet_sync tidak lengkap (ditemukan ${SHEET_TABLES:-0}/2; migrasi 00011?)"

    SHEET_CFG="$(q "SELECT count(*) FROM sheet_sync_config")"
    [[ "${SHEET_CFG:-0}" -eq 1 ]] && ok "baris konfigurasi sheet_sync_config ada (tunggal)" \
      || bad "baris sheet_sync_config = ${SHEET_CFG:-0} (ingin tepat 1)"

    # F19: jejak pengubah + keterangan penyelesaian (migrasi 00012).
    UPD_COL="$(q "SELECT count(*) FROM information_schema.columns WHERE table_name='work_items' AND column_name='updated_by_username'")"
    [[ "${UPD_COL:-0}" -eq 1 ]] && ok "kolom work_items.updated_by_username tersedia" \
      || bad "kolom updated_by_username tidak ada (migrasi 00012?)"

    NOTE_COL="$(q "SELECT count(*) FROM information_schema.columns WHERE table_name='task_details' AND column_name='completion_note'")"
    [[ "${NOTE_COL:-0}" -eq 1 ]] && ok "kolom task_details.completion_note tersedia" \
      || bad "kolom completion_note tidak ada (migrasi 00012?)"

    # F20/F21: siklus SLA, collaborator, catatan penanganan.
    TKT_TABLES="$(q "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('ticket_sla_cycles','ticket_collaborators')")"
    [[ "${TKT_TABLES:-0}" -eq 2 ]] && ok "tabel ticket_sla_cycles & ticket_collaborators tersedia" \
      || bad "tabel F20 tidak lengkap (${TKT_TABLES:-0}/2; migrasi 00014?)"

    HANDLE_COLS="$(q "SELECT count(*) FROM information_schema.columns WHERE table_name='ticket_details' AND column_name IN ('issue_found','troubleshooting','action_solution')")"
    [[ "${HANDLE_COLS:-0}" -eq 3 ]] && ok "kolom catatan penanganan tiket tersedia" \
      || bad "kolom penanganan tiket = ${HANDLE_COLS:-0} (ingin 3; migrasi 00014?)"

    SLA_POL="$(q "SELECT count(*) FROM sla_policies WHERE name LIKE 'SLA-INCIDENT-%'")"
    [[ "${SLA_POL:-0}" -ge 4 ]] && ok "SLA policy per prioritas (insiden): $SLA_POL" \
      || bad "SLA policy per prioritas = ${SLA_POL:-0} (ingin >= 4; migrasi 00014?)"

    # Provider WAHA dibuat otomatis dari konfigurasi environment.
    WAHA_PROV="$(q "SELECT count(*) FROM notification_providers WHERE kind='waha' AND is_active")"
    if [[ "${WAHA_PROV:-0}" -ge 1 ]]; then
      ok "provider WAHA tersedia (dibuat dari env)"
    else
      skip "provider WAHA belum ada — periksa INGATIN_WAHA_API_KEY"
    fi

    unset PGPASSWORD
  else
    skip "INGATIN_DB_URL tidak dapat diurai — pemeriksaan database dilewati"
  fi
else
  skip "psql tidak tersedia — pemeriksaan database dilewati"
fi

# ---------------------------------------------------------------------------
head "4. Web & proxy"
# ---------------------------------------------------------------------------
WEB_CODE="$(http_code_from "$WEB_BASE/")"
if [[ "$WEB_CODE" == "200" ]]; then
  ok "halaman web dapat diakses ($WEB_BASE)"
else
  bad "halaman web mengembalikan $WEB_CODE"
fi

PROXY="$(curl -fsS --max-time 5 "$WEB_BASE/api/health" 2>/dev/null || true)"
if echo "$PROXY" | grep -q '"status":"ok"'; then
  ok "proxy /api dari web berfungsi"
else
  bad "proxy /api dari web gagal"
fi

# ---------------------------------------------------------------------------
head "5. WAHA (opsional)"
# ---------------------------------------------------------------------------
# Mode B: INGATIN_WAHA_BASE_URL memakai host 'waha' (DNS internal Compose) yang
# tidak dapat di-resolve dari host. Untuk pemeriksaan dari host, arahkan ke
# port yang dipetakan (INGATIN_WAHA_PORT, default 8082).
WAHA_URL="${INGATIN_WAHA_BASE_URL:-http://127.0.0.1:8082}"
WAHA_URL="${WAHA_URL/http:\/\/waha:3000/http:\/\/127.0.0.1:${INGATIN_WAHA_PORT:-8082}}"
WAHA_KEY="${WAHA_API_KEY:-${INGATIN_WAHA_API_KEY:-}}"

if [[ -n "$WAHA_KEY" ]]; then
  # Uji jalur autentikasi yang sesungguhnya: kunci yang benar harus 200.
  WAHA_AUTH="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
    -H "X-Api-Key: ${WAHA_KEY}" "${WAHA_URL}/api/sessions" 2>/dev/null || echo 000)"
  case "$WAHA_AUTH" in
    200) ok "WAHA menerima API key (HTTP 200) di $WAHA_URL" ;;
    401|403) bad "WAHA menolak API key — samakan WAHA_API_KEY dengan INGATIN_WAHA_API_KEY di .env" ;;
    000) skip "WAHA belum berjalan di $WAHA_URL" ;;
    *)   bad "WAHA mengembalikan HTTP $WAHA_AUTH" ;;
  esac

  # Sesi 'default' wajib ada agar QR dapat dipindai; dibuat otomatis saat pertama
  # kali panel diakses. Bila belum ada, ini bukan kegagalan.
  SESS="$(curl -s --max-time 5 -H "X-Api-Key: ${WAHA_KEY}" "${WAHA_URL}/api/sessions" 2>/dev/null || true)"
  if echo "$SESS" | grep -q '"name":"default"'; then
    if echo "$SESS" | grep -q '"status":"WORKING"'; then
      ok "sesi WAHA 'default' aktif (WORKING)"
    else
      skip "sesi WAHA 'default' ada tetapi belum WORKING — buka dashboard QR untuk memindai"
    fi
  else
    skip "sesi WAHA 'default' belum dibuat — mulai dari panel Providers"
  fi
else
  WAHA_CODE="$(http_code_from "${WAHA_URL}/api/sessions")"
  case "$WAHA_CODE" in
    200|401|403) ok "WAHA merespons (HTTP $WAHA_CODE) di $WAHA_URL" ;;
    000)         skip "WAHA belum berjalan di $WAHA_URL" ;;
    *)           bad "WAHA mengembalikan HTTP $WAHA_CODE" ;;
  esac
fi

# ---------------------------------------------------------------------------
printf '\n\033[1mRingkasan:\033[0m %d lulus, %d gagal\n\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
exit 0
