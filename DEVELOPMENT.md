# Ingat.in — Development Guide

Panduan untuk pengembang: setup lokal, konvensi kode, dan cara menambah fitur.

> Baca `PLAN.md` untuk keputusan arsitektur dan konvensi kerja Level A.

---

## 1. Prasyarat

| Tool | Versi | Keterangan |
|---|---|---|
| Go | **1.25+** | `go version` |
| Node.js | 20+ | Untuk frontend |
| PostgreSQL | 14+ | Bisa pakai host atau container |
| Docker + Compose | 24+ / v2 | Untuk jalankan stack |
| `psql` | 15+ | Diagnostik DB |

### Catatan Go di server ini

Go bawaan sistem adalah **1.19.8** (terlalu lama untuk `go 1.25`). Toolchain baru
dipasang di `/usr/local/go1.25`:

```bash
/usr/local/go1.25/bin/go version      # go1.25.1

# Tambahkan ke PATH (per shell)
export PATH=/usr/local/go1.25/bin:$PATH
```

Build di Docker tidak terpengaruh (`golang:1.25-alpine` di stage builder).

---

## 2. Setup Development

### 2.1 Jalankan penuh dengan Docker (paling cepat)

```bash
cd /path/ke/ingat.in
cp .env.example .env      # sesuaikan secret
./scripts/provision-db.sh # bila memakai PG host
docker compose up -d --build
docker compose logs -f api worker
```

### 2.2 Jalankan backend langsung (untuk iterasi cepat)

```bash
export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin?sslmode=disable'
export INGATIN_SECRET_KEY="$(openssl rand -hex 32)"
export INGATIN_CREDENTIAL_KEY="$(openssl rand -base64 24 | cut -c1-32)"
export INGATIN_ADDR='127.0.0.1:8081'
export INGATIN_TIMEZONE='Asia/Jakarta'
export PATH=/usr/local/go1.25/bin:$PATH

cd backend
go run ./cmd/ingatin -mode=migrate   # sekali
go run ./cmd/ingatin -mode=api       # di terminal 1
go run ./cmd/ingatin -mode=worker    # di terminal 2
```

### 2.3 Frontend dev (hot reload)

```bash
cd frontend
npm install
npm run dev            # Vite, proxy /api → 127.0.0.1:8081
```

---

## 3. Struktur Backend

```
backend/
├── cmd/ingatin/main.go            # entrypoint; flag: -mode api|worker|migrate, -config
└── internal/
    ├── config/                    # pembacaan env INGATIN_*; fail-fast pada secret tidak valid
    ├── crypto/                     # AES-256-GCM (Enkripsi API key provider)
    ├── db/                         # pgx pool + goose (embed migrations/)
    │   └── migrations/            # 000NN_nama.sql — JANGAN edit yang sudah dijalankan
    ├── models/                     # struct domain
    ├── repository/                 # Store{a *pgxpool.Pool}; satu file per entitas
    ├── auth/                       # JWT + refresh token (F2)
    ├── api/                        # chi router; Server struct + method handler
    ├── workitems/                  # service domain: numbering, events, workflows
    ├── notify/                     # outbox, fanout, templating, escalation, sender
    ├── providers/                  # interface Provider + telegram/waha/fonnte/wablas/...
    ├── sla/                        # clock, policy, tick (F11)
    ├── backup/                     # pg_dump + gzip + sha256 + retensi
    └── worker/                     # robfig/cron: fanout, sender, digest, retention, sla
```

---

## 4. Konvensi Kode (diadopsi dari `mcnvpn`)

### 4.1 Umum

- **Tidak ada DI framework.** Semua dependency diinjeksi lewat constructor:
  `api.New(cfg, store, authMgr, ...)`.
- **Tidak ada ORM.** SQL mentah dengan placeholder posisional `$1..$n`.
- **Error handling eksplisit:** bungkus dengan `fmt.Errorf("...: %w", err)`.
- **Logging:** `log` standar (`log.Printf`) dengan prefiks modul.
- **Konteks:** setiap operasi I/O menerima `context.Context`.

### 4.2 Database

- Pool: `pgxpool.New(ctx, url)` lalu `Ping`.
- Baris tunggal: `pool.QueryRow(ctx, `SQL`, args...).Scan(...)`;
  `errors.Is(err, pgx.ErrNoRows)` → `ErrNotFound`.
- Banyak baris: `pool.Query(...)` + `defer rows.Close()` + helper
  `scanXxx(rows pgx.Rows) ([]T, error)` yang memeriksa `rows.Err()`.
- Tulis: `_, err := pool.Exec(...)`.
- Transaksi: `store.Tx(ctx, func(tx pgx.Tx) error { ... })`.
- Waktu: simpan UTC (`store.Now()`), tampilkan WIB.

#### Soft delete — kebijakan wajib (temuan A18 review F1)

`work_items` memakai `is_deleted`, **tetapi tabel anak dan extension tidak**:
`task_details`, `reminder_details`, `rfs_details`, `ticket_details`,
`comments`, `attachments`, `watchers`, `work_item_events`, `notification_outbox`.

Aturannya:

1. **Setiap query yang membaca tabel anak WAJIB melalui parent yang sudah
   difilter `NOT is_deleted`.** Contoh yang benar:

   ```sql
   SELECT c.* FROM comments c
   JOIN work_items w ON w.id = c.work_item_id AND NOT w.is_deleted
   WHERE c.work_item_id = $1
   ```

2. `loadExtensions` aman karena hanya dipanggil setelah parent diverifikasi
   hidup. Jangan panggil extension secara langsung tanpa penjagaan itu.

3. `work_item_events` **sengaja tidak pernah dihapus** walau parent
   soft-deleted: tabel ini adalah sumber audit dan perhitungan SLA historis.
   Baris event untuk item yang dihapus tetap tersimpan.

4. `SoftDeleteWorkItem` hanya menandai parent. Bila nanti dibutuhkan
   penghapusan berantai, tambahkan eksplisit — jangan mengandalkan FK, karena
   FK `ON DELETE CASCADE` tidak berlaku pada soft delete.

### 4.2.1 Extension work_items

Setiap `item_type` punya tabel extension 1:1. **Extension harus dibuat di dalam
transaksi yang sama** dengan `work_items`, jika tidak `GetWorkItem` akan
mengembalikan extension `nil`.

Gunakan helper di `internal/repository/work_item_extensions.go`:

| item_type | Helper | Parameter |
|---|---|---|
| `task` | `CreateTaskDetails` | checklist, estimate_minutes |
| `reminder` | `CreateReminderDetails` | category, subject_name, subject_type, escalation_policy_id, recurrence_rule |
| `rfs` | `CreateRFSDetails` | struct `CreateRFSDetailsParams` |
| `incident`/`request`/`change` | `CreateTicketDetails` | struct `CreateTicketDetailsParams` |

Contoh pola yang benar (lihat `integration_test.go` `TestExtensionWritePath`):

```go
err := store.Tx(ctx, func(tx pgx.Tx) error {
    ref, err := workitems.NextRefNo(ctx, tx, models.ItemTask, time.Now())
    if err != nil { return err }
    wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
        RefNo: ref, ItemType: models.ItemTask, Title: "…",
        Status: "open", Priority: "normal", CreatedBy: user,
    })
    if err != nil { return err }
    if err := store.CreateTaskDetails(ctx, tx, wi.ID, checklist, nil); err != nil {
        return err
    }
    return workitems.AppendEvent(ctx, tx, workitems.EventInput{
        WorkItemID: wi.ID.String(), EventType: workitems.EventCreated, ToValue: "open",
    })
})
```

> Jangan lupa `AppendEvent` untuk setiap perubahan status: itu sumber timeline,
> audit, dan SLA.

### 4.3 HTTP

- Handler = method pada `*Server`, bukan closure.
- Middleware berurutan: `RequestID → Recoverer → Logger → securityHeaders → cors`.
- Grup route: `r.Group(func(gr chi.Router) { gr.Use(s.requireAuth); ... })`.
- Respons error: `writeErr(w, status, "pesan")` → `{"error":"pesan"}`.
- Param path: `chi.URLParam(r, "id")`.

### 4.4 Penamaan

| Item | Konvensi | Contoh |
|---|---|---|
| Tabel | snake_case plural | `work_items`, `notification_outbox` |
| Kolom | snake_case | `expire_at`, `sla_state` |
| Struct | PascalCase | `WorkItem`, `NotificationOutbox` |
| Ref number | `PREFIX-YYYY-NNNN` | `TSK-2026-0001`, `INC-2026-0042` |
| Migrasi | `000NN_nama.sql` | `00003_add_targets.sql` |
| Env | `INGATIN_*` | `INGATIN_DB_URL` |

---

## 5. Cara Menambah Migrasi

1. Buat file baru dengan nomor urut berikutnya:

```bash
ls backend/internal/db/migrations/     # lihat nomor terakhir
# buat 00003_contoh.sql
```

2. Tulis dengan anotasi goose:

```sql
-- +goose Up
ALTER TABLE work_items ADD COLUMN new_field TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE work_items DROP COLUMN new_field;
```

3. Jalankan:

```bash
docker compose run --rm migrate -mode=migrate
# atau lokal: go run ./cmd/ingatin -mode=migrate
```

4. Verifikasi:

```bash
psql -h 127.0.0.1 -U ingatin -d ingatin -c \
  'SELECT version_id, is_applied FROM goose_db_version ORDER BY id DESC LIMIT 3'
```

**Aturan:** migrasi yang sudah diterapkan di lingkungan mana pun **tidak boleh diedit**.
Kesalahan diperbaiki dengan migrasi baru. Selalu sediakan `Down` yang benar.

---

## 6. Cara Menambah Provider Notifikasi

1. Buat file `backend/internal/providers/<nama>.go`:

```go
package providers

type Fonnte struct {
    baseURL string
    apiKey  string
    client  *http.Client
}

func NewFonnte(baseURL, apiKey string) *Fonnte {
    return &Fonnte{baseURL: baseURL, apiKey: apiKey, client: &http.Client{Timeout: 15 * time.Second}}
}

// Send mengikuti kontrak Provider.
func (f *Fonnte) Send(ctx context.Context, msg Message) (Result, error) {
    // ...
}
```

2. Implementasikan interface `Provider` (lihat `providers/base.go`):
   - `Kind() Kind`
   - `Send(ctx, Message) (Result, error)`
   - `TestConnection(ctx) error`

3. Daftarkan di `providers/registry.go` agar bisa dipilih dari panel.

4. Tambahkan unit test `providers/<nama>_test.go` dengan `httptest.Server`.

5. **Verifikasi Level A:** minta `reviewer` memeriksa SSRF (URL yang bisa dikonfigurasi
   user) dan penanganan API key, serta `tester` memeriksa retry/backoff.

---

## 7. Cara Menambah Template Notifikasi

Template disimpan di tabel `notification_templates` (`key`, `item_type`, `channel`,
`subject_tpl`, `body_tpl`) dan dirender dengan `text/template`.

Placeholder yang tersedia antara lain:

```
{{.RefNo}} {{.Title}} {{.Priority}} {{.Status}}
{{.Owner}} {{.Requester}} {{.Team}}
{{.DueAt}} {{.ExpireAt}} {{.Remaining}}
{{.OffsetLabel}} {{.Severity}}
{{.CustomerName}} {{.ServicePackage}} {{.Bandwidth}}
{{.PicNoc}} {{.PicSales}} {{.DeviceRef}} {{.Site}}
```

Menambah template baru: masukkan baris seed di migrasi baru, atau lewat panel (F4).
Jika menambah **placeholder baru**, perbarui juga struct payload di `notify/templating.go`.

---

## 8. Cara Menambah Workflow (State Machine)

`workflow_definitions.states_json` + `transitions_json` mendefinisikan status dan
transisi yang sah per `item_type`. Contoh:

```json
{
  "states": ["new", "assigned", "in_progress", "resolved", "closed"],
  "transitions": {
    "new": ["assigned", "in_progress", "closed"],
    "assigned": ["in_progress", "closed"],
    "in_progress": ["resolved", "closed"],
    "resolved": ["closed", "in_progress"],
    "closed": []
  }
}
```

Validasi transisi dilakukan di `workitems/workflows.go`. **Semua perubahan status wajib
mencatat `work_item_events`** (append-only) — ini sumber perhitungan SLA dan timeline UI.

---

## 9. Test

```bash
export PATH=/usr/local/go1.25/bin:$PATH
cd backend

go build ./...              # wajib lulus
go vet ./...                # wajib bersih
gofmt -l .                  # harus tidak ada output
go test ./...               # unit test
go test ./internal/crypto/... -v
go test -run TestNumbering ./internal/workitems/... -v
```

Konvensi: table-driven test, gunakan `httptest` untuk HTTP, dan untuk DB gunakan
database uji terpisah (`ingatin_test`) — jangan pernah menunjuk ke produksi.

```bash
# Siapkan DB uji (sekali)
sudo -u postgres psql -c "CREATE DATABASE ingatin_test OWNER ingatin"
export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
go run ./cmd/ingatin -mode=migrate
go test ./... -tags=integration
```

---

## 10. Frontend

```
frontend/src/
├── design/tokens.ts       # token warna/type/spacing dari m2c — SATU-SATUNYA sumber warna
├── api.ts                 # client fetch + penanganan token
├── App.tsx                # shell + routing
├── components/            # AppShell, DataTable, StatusBadge, Timeline, ...
├── pages/                 # Login, Dashboard, Todos, Reminders, Rfs, WorkItemDetail, ...
└── assets/                # canvas network animation (adaptasi m2c/js/canvas.js)
```

Aturan:

- **Jangan hard-code warna** di komponen; gunakan token dari `design/tokens.ts`
  atau kelas Tailwind dari preset.
- Font: **Inter** (judul), **Space Grotesk** (body), **JetBrains Mono** (ID/timestamp/kode).
- Ikon: **Material Symbols**.
- Setiap halaman wajib punya state: *loading*, *success*, *empty*, *error*, *unauthorized*.
- Mobile: bottom-sheet nav (pola m2c), bukan sidebar yang dikecilkan.

```bash
cd frontend
npm run build     # harus lulus sebelum commit
npm run dev
```

---

## 11. Konvensi Git & Commit

- Commit kecil dan fokus; satu commit satu maksud.
- Format pesan: `<area>: <ringkasan imperatif>` — contoh:
  `workitems: tambah numbering transaksional`, `api: tambah endpoint health`.
- Jangan commit `.env`, kredensial, atau data produksi.
- Jalankan `go build ./... && go vet ./... && gofmt -l .` sebelum commit.

---

## 12. Checklist Sebelum Menandai Fase Selesai (Level A)

- [ ] `go build ./...` lulus
- [ ] `go vet ./...` bersih
- [ ] `gofmt -l .` tidak ada output
- [ ] `go test ./...` lulus
- [ ] Perubahan skema punya migrasi `Up` + `Down`
- [ ] Endpoint baru terverifikasi dengan `curl` nyata (output dicatat)
- [ ] Verifikator (`reviewer`/`tester`) melaporkan **lulus**
- [ ] `PLAN.md` §8 diperbarui sesuai status
- [ ] Insight disimpan ke agentmemory
