//go:build integration

// Uji integrasi (memerlukan database PostgreSQL).
//
// Jalankan dengan:
//
//	export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
//	go run ./cmd/ingatin -mode=migrate
//	go test -tags=integration ./internal/repository/... -v
//
// Uji ini TIDAK boleh diarahkan ke database produksi.
package repository_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/db"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/workitems"
)

func setupStore(t *testing.T) (*repository.Store, context.Context) {
	t.Helper()

	url := os.Getenv("INGATIN_DB_URL")
	if url == "" {
		t.Skip("INGATIN_DB_URL tidak diset; uji integrasi dilewati")
	}
	if !strings.Contains(url, "test") {
		t.Fatalf("penolakan keamanan: INGATIN_DB_URL harus menunjuk database uji "+
			"(nama harus memuat \"test\"), got %s", url)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("koneksi database: %v", err)
	}
	t.Cleanup(pool.Close)
	return repository.New(pool), ctx
}

// TestNumberingIsSequential memverifikasi penomoran referensi berurutan dan
// tidak ada duplikat walau dipanggil berulang.
func TestNumberingIsSequential(t *testing.T) {
	store, ctx := setupStore(t)

	now := time.Now()
	seen := map[string]bool{}

	for i := 0; i < 5; i++ {
		var refNo string
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			r, err := workitems.NextRefNo(ctx, tx, models.ItemIncident, now)
			if err != nil {
				return err
			}
			refNo = r
			return nil
		})
		if err != nil {
			t.Fatalf("alokasi nomor ke-%d: %v", i, err)
		}
		if seen[refNo] {
			t.Fatalf("nomor referensi duplikat: %s", refNo)
		}
		seen[refNo] = true
		if !strings.HasPrefix(refNo, "INC-") {
			t.Errorf("prefiks salah untuk incident: %s", refNo)
		}
	}
}

// TestNumberingSeparatePerPrefix memverifikasi counter terpisah untuk setiap tipe.
func TestNumberingSeparatePerPrefix(t *testing.T) {
	store, ctx := setupStore(t)
	now := time.Now()

	types := map[string]string{
		models.ItemTask:     "TSK-",
		models.ItemReminder: "REM-",
		models.ItemRFS:      "RFS-",
		models.ItemRequest:  "REQ-",
		models.ItemChange:   "CHG-",
	}

	for it, prefix := range types {
		var refNo string
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			r, err := workitems.NextRefNo(ctx, tx, it, now)
			if err != nil {
				return err
			}
			refNo = r
			return nil
		})
		if err != nil {
			t.Fatalf("alokasi nomor untuk %s: %v", it, err)
		}
		if !strings.HasPrefix(refNo, prefix) {
			t.Errorf("item_type %s mendapat nomor %s, ingin prefiks %s", it, refNo, prefix)
		}
	}
}

// TestCreateWorkItemWithEvents memverifikasi pembuatan work item + extension
// + pencatatan event dalam satu transaksi.
func TestCreateWorkItemWithEvents(t *testing.T) {
	store, ctx := setupStore(t)

	var itemID string
	var refNo string

	err := store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, models.ItemTask, time.Now())
		if err != nil {
			return err
		}
		refNo = ref

		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:         ref,
			ItemType:      models.ItemTask,
			Title:         "Uji integrasi: cek BGP flap",
			Description:   "Dibuat oleh integration test",
			Priority:      "high",
			Status:        "open",
			OwnerUsername: "noc-test",
			Source:        "test",
			CreatedBy:     "integration-test",
		})
		if err != nil {
			return err
		}
		itemID = wi.ID.String()

		// Extension untuk task.
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_details (work_item_id, checklist_json, progress_pct)
			VALUES ($1, $2, 0)`, wi.ID, []byte(`[{"text":"cek peer","done":false}]`)); err != nil {
			return err
		}

		// Event created (append-only).
		return workitems.AppendEvent(ctx, tx, workitems.EventInput{
			WorkItemID: itemID,
			EventType:  workitems.EventCreated,
			Actor:      "integration-test",
			ToValue:    "open",
			Detail:     map[string]any{"via": "integration_test"},
		})
	})
	if err != nil {
		t.Fatalf("transaksi create: %v", err)
	}

	// Verifikasi item dapat dibaca kembali beserta extension-nya.
	item, err := store.GetWorkItem(ctx, mustUUID(t, itemID))
	if err != nil {
		t.Fatalf("baca work item: %v", err)
	}
	if item.RefNo != refNo {
		t.Errorf("ref_no tidak cocok: got %s want %s", item.RefNo, refNo)
	}
	if item.Task == nil {
		t.Error("extension task_details tidak dimuat")
	} else if item.Task.ProgressPct != 0 {
		t.Errorf("progress_pct = %d, ingin 0", item.Task.ProgressPct)
	}

	// Verifikasi event tercatat.
	var eventCount int
	if err := store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM work_item_events WHERE work_item_id=$1`, itemID).Scan(&eventCount); err != nil {
		t.Fatalf("hitung event: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("jumlah event = %d, ingin 1", eventCount)
	}

	// Bersihkan.
	if _, err := store.SoftDeleteWorkItem(ctx, mustUUID(t, itemID)); err != nil {
		t.Fatalf("hapus work item: %v", err)
	}
}

// TestStatusTransitionRecorded memverifikasi perubahan status tervalidasi
// dan tercatat sebagai event.
func TestStatusTransitionRecorded(t *testing.T) {
	store, ctx := setupStore(t)

	wf := workitems.WorkflowFor(models.ItemTask)
	if wf == nil {
		t.Fatal("workflow task tidak ditemukan")
	}

	var itemID string
	err := store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, models.ItemTask, time.Now())
		if err != nil {
			return err
		}
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo: ref, ItemType: models.ItemTask,
			Title: "Uji transisi status", Status: wf.InitialState,
			Priority: "normal", Source: "test", CreatedBy: "integration-test",
		})
		if err != nil {
			return err
		}
		itemID = wi.ID.String()

		// Transisi sah sesuai workflow task saat ini: accepted -> on_progress.
		res := wf.EvaluateTransitions("accepted", "on_progress")
		if !res.Allowed {
			return errTransition
		}
		if err := store.UpdateWorkItemFields(ctx, tx, wi.ID, map[string]any{"status": "on_progress"}); err != nil {
			return err
		}
		return workitems.AppendEvent(ctx, tx, workitems.EventInput{
			WorkItemID: itemID, EventType: workitems.EventStatusChanged,
			Actor: "integration-test", FromValue: "accepted", ToValue: "on_progress",
		})
	})
	if err != nil {
		t.Fatalf("transaksi transisi: %v", err)
	}

	item, err := store.GetWorkItem(ctx, mustUUID(t, itemID))
	if err != nil {
		t.Fatalf("baca work item: %v", err)
	}
	if item.Status != "on_progress" {
		t.Errorf("status = %q, ingin on_progress", item.Status)
	}

	// Transisi tidak sah harus ditolak oleh workflow.
	if wf.CanTransition("on_progress", "accepted") {
		t.Error("transisi on_progress -> accepted seharusnya DITOLAK")
	}

	if _, err := store.SoftDeleteWorkItem(ctx, mustUUID(t, itemID)); err != nil {
		t.Fatalf("hapus work item: %v", err)
	}
}

// TestOutboxEventKeyUnique memverifikasi idempotensi outbox pada tingkat DB.
func TestOutboxEventKeyUnique(t *testing.T) {
	store, ctx := setupStore(t)

	key := "test:integration:" + time.Now().Format("20060102150405.000000000")

	insert := func() error {
		_, err := store.Pool().Exec(ctx, `
			INSERT INTO notification_outbox
				(event_key, source_type, channel, destination, payload_json)
			VALUES ($1, 'test', 'telegram', '12345', '{}'::jsonb)
			ON CONFLICT (event_key) DO NOTHING`, key)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("insert pertama: %v", err)
	}
	if err := insert(); err != nil {
		t.Fatalf("insert kedua (harus no-op): %v", err)
	}

	var n int
	if err := store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM notification_outbox WHERE event_key=$1`, key).Scan(&n); err != nil {
		t.Fatalf("hitung outbox: %v", err)
	}
	if n != 1 {
		t.Errorf("baris outbox dengan event_key sama = %d, ingin 1 (idempotent)", n)
	}

	if _, err := store.Pool().Exec(ctx, `DELETE FROM notification_outbox WHERE event_key=$1`, key); err != nil {
		t.Fatalf("bersihkan outbox: %v", err)
	}
}

// TestTagsJSONBRoundTrip memverifikasi []string di-encode ke JSONB dan
// dibaca kembali tanpa kehilangan data (findings A10 dari review F1).
func TestTagsJSONBRoundTrip(t *testing.T) {
	store, ctx := setupStore(t)

	tags := []string{"noc", "trial", "rfs", "urgent"}

	var itemID string
	err := store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, models.ItemTask, time.Now())
		if err != nil {
			return err
		}
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo: ref, ItemType: models.ItemTask, Title: "uji tags jsonb",
			Priority: "normal", Status: "open", Source: "test",
			Tags: tags, CreatedBy: "integration-test",
		})
		if err != nil {
			return err
		}
		itemID = wi.ID.String()
		return nil
	})
	if err != nil {
		t.Fatalf("buat work item bertags: %v", err)
	}
	t.Cleanup(func() { _, _ = store.SoftDeleteWorkItem(ctx, mustUUID(t, itemID)) })

	got, err := store.GetWorkItem(ctx, mustUUID(t, itemID))
	if err != nil {
		t.Fatalf("baca work item: %v", err)
	}

	if len(got.Tags) != len(tags) {
		t.Fatalf("round-trip tags gagal: ingin %d (%v), dapat %d (%v)",
			len(tags), tags, len(got.Tags), got.Tags)
	}
	for i := range tags {
		if got.Tags[i] != tags[i] {
			t.Errorf("tags[%d] = %q, ingin %q", i, got.Tags[i], tags[i])
		}
	}
}

// TestSeedTemplateNullChannelIdempotent memverifikasi temuan A11: baris
// notification_templates dengan channel NULL tidak boleh terduplikasi.
//
// Indeks unik parsial `ux_notification_templates_key_no_channel` menegakkan
// keunikan pada (key) WHERE channel IS NULL. Karena itu idiom yang benar
// adalah `ON CONFLICT (key) WHERE channel IS NULL` — bukan
// `ON CONFLICT (key, channel)` yang tidak mendeteksi NULL.
func TestSeedTemplateNullChannelIdempotent(t *testing.T) {
	store, ctx := setupStore(t)

	const key = "ZZ_IDEMPOTENCY_TEST"

	cleanup := func() {
		_, _ = store.Pool().Exec(ctx, `DELETE FROM notification_templates WHERE key=$1`, key)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Insert pertama: harus berhasil.
	if _, err := store.Pool().Exec(ctx, `
		INSERT INTO notification_templates (key, item_type, channel, body_tpl)
		VALUES ($1, NULL, NULL, 'uji idempotensi')
		ON CONFLICT (key) WHERE channel IS NULL DO NOTHING`, key); err != nil {
		t.Fatalf("insert pertama gagal: %v", err)
	}

	// Insert kedua: harus no-op (inilah yang gagal sebelum perbaikan).
	if _, err := store.Pool().Exec(ctx, `
		INSERT INTO notification_templates (key, item_type, channel, body_tpl)
		VALUES ($1, NULL, NULL, 'uji idempotensi')
		ON CONFLICT (key) WHERE channel IS NULL DO NOTHING`, key); err != nil {
		t.Fatalf("insert kedua (harus no-op) gagal: %v", err)
	}

	var n int
	if err := store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM notification_templates WHERE key=$1 AND channel IS NULL`, key).Scan(&n); err != nil {
		t.Fatalf("hitung template: %v", err)
	}
	if n != 1 {
		t.Errorf("baris template channel NULL = %d, ingin 1 (ON CONFLICT belum idempoten)", n)
	}
}

// TestSeedTemplateNullChannelIndexExists memverifikasi indeks unik parsial ada,
// karena tanpa indeks itu ON CONFLICT (key) WHERE channel IS NULL tidak sah.
func TestSeedTemplateNullChannelIndexExists(t *testing.T) {
	store, ctx := setupStore(t)

	var exists bool
	err := store.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename='notification_templates'
			  AND indexname='ux_notification_templates_key_no_channel'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("periksa indeks: %v", err)
	}
	if !exists {
		t.Fatal("indeks unik parsial untuk channel NULL tidak ditemukan " +
			"(migrasi 00003 belum diterapkan?)")
	}
}

// TestExtensionWritePath memverifikasi temuan A1: extension dibuat bersama
// work_items dalam satu transaksi sehingga GetWorkItem memuatnya.
func TestExtensionWritePath(t *testing.T) {
	store, ctx := setupStore(t)

	cases := []struct {
		itemType string
		create   func(tx pgx.Tx, id uuid.UUID) error
		verify   func(t *testing.T, wi *models.WorkItem)
	}{
		{
			itemType: models.ItemTask,
			create: func(tx pgx.Tx, id uuid.UUID) error {
				return store.CreateTaskDetails(ctx, tx, id,
					[]models.ChecklistItem{{Text: "cek peer", Done: false}}, nil)
			},
			verify: func(t *testing.T, wi *models.WorkItem) {
				if wi.Task == nil {
					t.Fatal("extension task tidak dimuat")
				}
				if len(wi.Task.Checklist) != 1 || wi.Task.Checklist[0].Text != "cek peer" {
					t.Errorf("checklist tidak benar: %#v", wi.Task.Checklist)
				}
			},
		},
		{
			itemType: models.ItemReminder,
			create: func(tx pgx.Tx, id uuid.UUID) error {
				return store.CreateReminderDetails(ctx, tx, id,
					models.ReminderCategoryTrial, "PT Uji Trial", models.SubjectTypeCustomer, nil, nil)
			},
			verify: func(t *testing.T, wi *models.WorkItem) {
				if wi.Reminder == nil {
					t.Fatal("extension reminder tidak dimuat")
				}
				if wi.Reminder.Category != models.ReminderCategoryTrial {
					t.Errorf("category = %q, ingin trial", wi.Reminder.Category)
				}
				if wi.Reminder.SubjectName != "PT Uji Trial" {
					t.Errorf("subject_name = %q", wi.Reminder.SubjectName)
				}
			},
		},
		{
			itemType: models.ItemRFS,
			create: func(tx pgx.Tx, id uuid.UUID) error {
				return store.CreateRFSDetails(ctx, tx, id, repository.CreateRFSDetailsParams{
					CustomerName: "PT RFS Uji", ServicePackage: "Dedicated", Bandwidth: "100 Mbps",
					PicNOC: "Budi", PicSales: "Sari", Site: "CGK-1",
				})
			},
			verify: func(t *testing.T, wi *models.WorkItem) {
				if wi.RFS == nil {
					t.Fatal("extension rfs tidak dimuat")
				}
				if wi.RFS.CustomerName != "PT RFS Uji" {
					t.Errorf("customer_name = %q", wi.RFS.CustomerName)
				}
				if wi.RFS.InstallStage != models.RFSStagePlanned {
					t.Errorf("install_stage default = %q, ingin planned", wi.RFS.InstallStage)
				}
			},
		},
		{
			itemType: models.ItemIncident,
			create: func(tx pgx.Tx, id uuid.UUID) error {
				return store.CreateTicketDetails(ctx, tx, id, repository.CreateTicketDetailsParams{
					Category: "jaringan", Subcategory: "bgp",
				})
			},
			verify: func(t *testing.T, wi *models.WorkItem) {
				if wi.Ticket == nil {
					t.Fatal("extension ticket tidak dimuat")
				}
				if wi.Ticket.Category != "jaringan" {
					t.Errorf("category = %q", wi.Ticket.Category)
				}
				if wi.Ticket.Impact != models.LevelMedium || wi.Ticket.Urgency != models.LevelMedium {
					t.Errorf("impact/urgency default salah: %q/%q", wi.Ticket.Impact, wi.Ticket.Urgency)
				}
				if wi.Ticket.EscalationLevel != 1 {
					t.Errorf("escalation_level default = %d, ingin 1", wi.Ticket.EscalationLevel)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.itemType, func(t *testing.T) {
			var itemID string
			err := store.Tx(ctx, func(tx pgx.Tx) error {
				ref, err := workitems.NextRefNo(ctx, tx, tc.itemType, time.Now())
				if err != nil {
					return err
				}
				// Status awal harus state awal workflow agar konsisten.
				wf := workitems.WorkflowFor(tc.itemType)
				initial := "open"
				if wf != nil {
					initial = wf.InitialState
				}
				wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
					RefNo: ref, ItemType: tc.itemType, Title: "uji extension " + tc.itemType,
					Priority: "normal", Status: initial, Source: "test",
					CreatedBy: "integration-test",
				})
				if err != nil {
					return err
				}
				itemID = wi.ID.String()
				if err := tc.create(tx, wi.ID); err != nil {
					return err
				}
				return workitems.AppendEvent(ctx, tx, workitems.EventInput{
					WorkItemID: itemID, EventType: workitems.EventCreated,
					Actor: "integration-test", ToValue: initial,
				})
			})
			if err != nil {
				t.Fatalf("transaksi: %v", err)
			}
			t.Cleanup(func() { _, _ = store.SoftDeleteWorkItem(ctx, mustUUID(t, itemID)) })

			wi, err := store.GetWorkItem(ctx, mustUUID(t, itemID))
			if err != nil {
				t.Fatalf("baca: %v", err)
			}
			tc.verify(t, wi)
		})
	}
}

var errTransition = &transitionError{}

type transitionError struct{}

func (e *transitionError) Error() string { return "transisi tidak diizinkan" }

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("uuid tidak valid %q: %v", s, err)
	}
	return parsed
}

// TestSheetSyncRequeueAfterSent memverifikasi temuan penting: setelah sebuah
// item pernah tersinkron (status sent, op update), perubahan berikutnya WAJIB
// mengembalikan entri ke pending agar ikut terkirim. Sebelum perbaikan, klausa
// WHERE pada ON CONFLICT membuat perubahan setelah sent tidak pernah terkirim.
func TestSheetSyncRequeueAfterSent(t *testing.T) {
	store, ctx := setupStore(t)

	// Buat work item nyata (antrean punya FK ke work_items).
	var itemID uuid.UUID
	if err := store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, models.ItemTask, time.Now())
		if err != nil {
			return err
		}
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:     ref,
			ItemType:  models.ItemTask,
			Title:     "Uji sinkronisasi spreadsheet",
			Priority:  "normal",
			Status:    "accepted",
			Source:    "test",
			CreatedBy: "integration-test",
		})
		if err != nil {
			return err
		}
		itemID = wi.ID
		return nil
	}); err != nil {
		t.Fatalf("buat work item: %v", err)
	}
	t.Cleanup(func() { _, _ = store.SoftDeleteWorkItem(ctx, itemID) })
	eventKey := "sheet:" + itemID.String()

	base := repository.EnqueueSheetSyncParams{
		WorkItemID: itemID,
		EventKey:   eventKey,
		RefNo:      "TSK-TEST-REQUEUE",
		Op:         models.SheetOpAppend,
		Action:     "create",
		Payload:    map[string]any{"ref_no": "TSK-TEST-REQUEUE", "status": "accepted"},
	}

	// 1) Enqueue awal.
	if _, err := store.EnqueueSheetSync(ctx, base); err != nil {
		t.Fatalf("enqueue awal: %v", err)
	}
	rows, err := store.ClaimSheetSync(ctx, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	var target *models.SheetSyncQueueRow
	for i := range rows {
		if rows[i].EventKey == eventKey {
			target = &rows[i]
		}
	}
	if target == nil {
		t.Fatal("entri tidak ditemukan setelah enqueue")
	}
	if err := store.MarkSheetSyncSent(ctx, target.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	// 2) Perubahan setelah sent -> harus kembali pending dengan payload baru.
	upd := base
	upd.Op = models.SheetOpUpdate
	upd.Action = "status_changed"
	upd.Payload = map[string]any{"ref_no": "TSK-TEST-REQUEUE", "status": "closed"}
	if _, err := store.EnqueueSheetSync(ctx, upd); err != nil {
		t.Fatalf("enqueue setelah sent: %v", err)
	}

	var status, action, payloadStatus string
	if err := store.Pool().QueryRow(ctx, `
		SELECT status, action, payload_json->>'status'
		FROM sheet_sync_queue WHERE event_key=$1`, eventKey,
	).Scan(&status, &action, &payloadStatus); err != nil {
		t.Fatalf("baca entri: %v", err)
	}
	if status != models.SheetSyncPending {
		t.Errorf("status = %q, want pending (perubahan setelah sent tidak diantre ulang)", status)
	}
	if action != "status_changed" {
		t.Errorf("action = %q, want status_changed", action)
	}
	if payloadStatus != "closed" {
		t.Errorf("payload status = %q, want closed", payloadStatus)
	}
}

// TestUpdatedByAndCompletionNote memverifikasi jejak pengubah terakhir dan
// keterangan penyelesaian Daily Task tersimpan serta terbaca kembali.
func TestUpdatedByAndCompletionNote(t *testing.T) {
	store, ctx := setupStore(t)

	var itemID uuid.UUID
	if err := store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, models.ItemDailyTask, time.Now())
		if err != nil {
			return err
		}
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:     ref,
			ItemType:  models.ItemDailyTask,
			Title:     "Uji daily task",
			Priority:  "normal",
			Status:    "pending",
			Source:    "test",
			CreatedBy: "creatorA",
		})
		if err != nil {
			return err
		}
		itemID = wi.ID
		// Simpan keterangan penyelesaian.
		if err := store.SetTaskCompletionNote(ctx, tx, wi.ID, "Selesai, clear"); err != nil {
			return err
		}
		return store.UpdateWorkItemFields(ctx, tx, wi.ID, map[string]any{
			"status":              "done",
			"updated_by_username": "operatorB",
		})
	}); err != nil {
		t.Fatalf("transaksi: %v", err)
	}
	t.Cleanup(func() { _, _ = store.SoftDeleteWorkItem(ctx, itemID) })

	got, err := store.GetWorkItem(ctx, itemID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.UpdatedByUsername != "operatorB" {
		t.Errorf("updated_by = %q, want operatorB", got.UpdatedByUsername)
	}
	if got.Task == nil {
		t.Fatal("task details tidak dimuat untuk daily_task")
	}
	if got.Task.CompletionNote != "Selesai, clear" {
		t.Errorf("completion_note = %q, want 'Selesai, clear'", got.Task.CompletionNote)
	}
}
