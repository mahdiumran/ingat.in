//go:build integration

// Uji integrasi alur status Aktivasi/EWO (RFS):
//
//	new → Mulai Kerjakan → in_progress → Close Ticket → activated
//
// Regresi yang dijaga:
//
//  1. "Close Ticket" dulu hanya menyimpan tahap instalasi lalu gagal diam-diam
//     memindahkan status; RFS yang masih di status awal tetap di status awal
//     walau sudah diaktivasi (UI menampilkan Closed/Completed dari tahap).
//
//  2. Ikut menangani RFS yang masih di status awal tidak memajukan status.
//
//  3. Jadwal (expire_at) BUKAN penghalang: pengerjaan lebih awal diizinkan
//     asalkan alasan diisi. Tanpa alasan → 400, dengan alasan → 200 dan event
//     mencatat `early_start`.
//
//     export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
//     go test -tags=integration ./internal/api/... -run TestRFSClose -v
package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
)

func TestRFSCloseAndJoinStatus(t *testing.T) {
	store, ctx := setupTestStore(t)
	cfg := &config.Config{Timezone: "Asia/Jakarta", OutboxMaxAttempts: 3}
	s := &Server{cfg: cfg, store: store, notifier: notify.NewResolver(cfg, store)}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	teamA, err := store.CreateTeam(ctx, "IT-CLOSE-A "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim: %v", err)
	}
	newUser := func(name, role string) *models.User {
		t.Helper()
		u, err := store.CreateUser(ctx, repository.CreateUserParams{
			Username: name, Email: name + "@example.test", FullName: name,
			PasswordHash: "x", Role: role, TeamID: &teamA.ID,
		})
		if err != nil {
			t.Fatalf("buat user %s: %v", name, err)
		}
		return u
	}
	sales := newUser("it_cs_"+suffix, models.RoleSales)
	noc1 := newUser("it_n1_"+suffix, models.RoleNOC)
	noc2 := newUser("it_n2_"+suffix, models.RoleNOC)

	created := make([]uuid.UUID, 0, 4)
	t.Cleanup(func() {
		p := store.Pool()
		for _, id := range created {
			_, _ = p.Exec(ctx, "DELETE FROM work_items WHERE id=$1", id)
		}
		for _, u := range []*models.User{sales, noc1, noc2} {
			_, _ = p.Exec(ctx, "DELETE FROM users WHERE id=$1", u.ID)
		}
		_, _ = p.Exec(ctx, "DELETE FROM teams WHERE id=$1", teamA.ID)
	})

	seq := 0
	// newRFS membuat RFS dengan status dan jadwal (expire_at) tertentu.
	// expireAt nil berarti RFS tanpa jadwal.
	newRFS := func(owner, status string, expireAt *time.Time) *models.WorkItem {
		t.Helper()
		seq++
		var wi *models.WorkItem
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			wi, err = store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
				RefNo:         fmt.Sprintf("RFS-CL-%s-%d", suffix, seq),
				ItemType:      models.ItemRFS,
				Title:         "Aktivasi tutup " + suffix,
				Priority:      "normal",
				Status:        status,
				OwnerUsername: owner,
				ExpireAt:      expireAt,
				CreatedBy:     sales.Username,
			})
			if err != nil {
				return err
			}
			return store.CreateRFSDetails(ctx, tx, wi.ID, repository.CreateRFSDetailsParams{
				CustomerName: "Pelanggan " + suffix,
				InstallStage: models.RFSStagePlanned,
			})
		})
		if err != nil {
			t.Fatalf("buat RFS: %v", err)
		}
		created = append(created, wi.ID)
		return wi
	}

	closeRFS := func(u *models.User, id uuid.UUID) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/rfs-close",
			strings.NewReader(`{"reason":"selesai uji"}`))
		return callHandler(t, r, u, id, s.handleRFSClose)
	}
	startRFS := func(u *models.User, id uuid.UUID, body string) int {
		t.Helper()
		if body == "" {
			body = `{"note":"mulai uji"}`
		}
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/rfs-start",
			strings.NewReader(body))
		return callHandler(t, r, u, id, s.handleRFSStart)
	}
	joinRFS := func(u *models.User, id uuid.UUID) int {
		t.Helper()
		body := strings.NewReader(`{"username":"` + u.Username + `"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/collaborators", body)
		return callHandler(t, r, u, id, s.handleAddCollaborator)
	}
	statusOf := func(id uuid.UUID) string {
		t.Helper()
		got, err := store.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatalf("baca item: %v", err)
		}
		return got.Status
	}
	// lastStatusEvent mengambil event perubahan status terbaru (untuk memeriksa
	// penanda early_start pada pengecualian jadwal).
	lastStatusEvent := func(id uuid.UUID) *models.WorkItemEvent {
		t.Helper()
		events, err := store.ListEvents(ctx, id, 50)
		if err != nil {
			t.Fatalf("baca event: %v", err)
		}
		for i := range events {
			if events[i].EventType == "status_changed" {
				return &events[i]
			}
		}
		return nil
	}

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(72 * time.Hour)

	// --- Kasus 1: alur normal new → in_progress → activated. ---
	open := newRFS(sales.Username, "new", &past)
	if code := startRFS(noc1, open.ID, ""); code != http.StatusOK {
		t.Fatalf("mulai kerjakan RFS: got %d, want 200", code)
	}
	if got := statusOf(open.ID); got != "in_progress" {
		t.Fatalf("status setelah Mulai Kerjakan = %q, want in_progress", got)
	}
	// Mulai ulang idempoten.
	if code := startRFS(noc1, open.ID, ""); code != http.StatusOK {
		t.Fatalf("mulai ulang RFS: got %d, want 200", code)
	}
	if code := closeRFS(noc1, open.ID); code != http.StatusOK {
		t.Fatalf("tutup RFS: got %d, want 200", code)
	}
	closed, err := store.GetWorkItem(ctx, open.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if closed.Status != "activated" {
		t.Fatalf("status setelah close = %q, want activated", closed.Status)
	}
	if closed.RFS == nil || closed.RFS.InstallStage != models.RFSStageActivated {
		t.Fatalf("install_stage setelah close = %+v, want activated", closed.RFS)
	}
	if closed.ClosedAt == nil {
		t.Error("closed_at tidak diisi saat RFS ditutup")
	}
	// Close ulang idempoten (200, tanpa duplikasi masalah).
	if code := closeRFS(noc1, open.ID); code != http.StatusOK {
		t.Fatalf("tutup ulang RFS: got %d, want 200", code)
	}

	// --- Kasus 2: jadwal belum tiba — boleh, tetapi alasan wajib. ---
	early := newRFS(sales.Username, "new", &future)
	// Tanpa alasan → ditolak 400 dengan pesan yang menjelaskan pilihan.
	if code := startRFS(noc1, early.ID, `{"note":"mulai uji"}`); code != http.StatusBadRequest {
		t.Fatalf("mulai sebelum jadwal tanpa alasan: got %d, want 400", code)
	}
	if got := statusOf(early.ID); got != "new" {
		t.Fatalf("status setelah mulai ditolak = %q, want tetap new", got)
	}
	// Jalur generik /status juga menuntut alasan (note) sebelum jadwal.
	r := httptest.NewRequest(http.MethodPost, "/api/items/"+early.ID.String()+"/status",
		strings.NewReader(`{"status":"in_progress"}`))
	if code := callHandler(t, r, noc1, early.ID, s.handleChangeStatus); code != http.StatusBadRequest {
		t.Fatalf("status generik sebelum jadwal tanpa alasan: got %d, want 400", code)
	}
	if got := statusOf(early.ID); got != "new" {
		t.Fatalf("status setelah /status ditolak = %q, want tetap new", got)
	}
	// Ikut menangani sebelum jadwal hanya mencatat penangan (tanpa alasan).
	if code := joinRFS(noc2, early.ID); code != http.StatusOK {
		t.Fatalf("ikut menangani RFS: got %d, want 200", code)
	}
	if got := statusOf(early.ID); got != "new" {
		t.Fatalf("status setelah ikut menangani sebelum jadwal = %q, want tetap new", got)
	}
	// Dengan alasan → 200, status maju, dan pengecualian tercatat.
	if code := startRFS(noc1, early.ID,
		`{"note":"mulai uji","reason":"pelanggan minta aktivasi lebih cepat"}`); code != http.StatusOK {
		t.Fatalf("mulai sebelum jadwal dengan alasan: got %d, want 200", code)
	}
	if got := statusOf(early.ID); got != "in_progress" {
		t.Fatalf("status setelah mulai lebih awal = %q, want in_progress", got)
	}
	if ev := lastStatusEvent(early.ID); ev == nil {
		t.Error("event perubahan status tidak ditemukan")
	} else {
		if earlyFlag, _ := ev.Detail["early_start"].(bool); !earlyFlag {
			t.Errorf("event harus menandai early_start, dapat %+v", ev.Detail)
		}
		if got, _ := ev.Detail["reason"].(string); !strings.Contains(got, "lebih cepat") {
			t.Errorf("event harus memuat alasan, dapat %+v", ev.Detail)
		}
		if _, ok := ev.Detail["schedule"]; !ok {
			t.Errorf("event harus memuat jadwal asal, dapat %+v", ev.Detail)
		}
	}
	// Jalur generik /status juga boleh lebih awal asalkan ada catatan alasan.
	earlyGeneric := newRFS(sales.Username, "new", &future)
	r2 := httptest.NewRequest(http.MethodPost, "/api/items/"+earlyGeneric.ID.String()+"/status",
		strings.NewReader(`{"status":"in_progress","note":"pelanggan minta lebih cepat"}`))
	if code := callHandler(t, r2, noc1, earlyGeneric.ID, s.handleChangeStatus); code != http.StatusOK {
		t.Fatalf("status generik sebelum jadwal dengan alasan: got %d, want 200", code)
	}
	if got := statusOf(earlyGeneric.ID); got != "in_progress" {
		t.Fatalf("status generik lebih awal = %q, want in_progress", got)
	}

	// --- Kasus 3: ikut menangani setelah jadwal tiba memajukan status. ---
	ready := newRFS(sales.Username, "new", &past)
	if code := joinRFS(noc2, ready.ID); code != http.StatusOK {
		t.Fatalf("ikut menangani RFS: got %d, want 200", code)
	}
	if got := statusOf(ready.ID); got != "in_progress" {
		t.Fatalf("status setelah ikut menangani = %q, want in_progress", got)
	}

	// RFS yang sudah ditutup tidak dibuka kembali oleh self-join.
	if code := joinRFS(noc2, open.ID); code != http.StatusOK {
		t.Fatalf("ikut menangani RFS tertutup: got %d, want 200", code)
	}
	if got := statusOf(open.ID); got != "activated" {
		t.Fatalf("status RFS tertutup berubah menjadi %q, want tetap activated", got)
	}

	// --- Kasus 4: RFS tanpa jadwal boleh langsung dimulai (tidak diblokir). ---
	noSchedule := newRFS(sales.Username, "new", nil)
	if code := startRFS(noc1, noSchedule.ID, ""); code != http.StatusOK {
		t.Fatalf("mulai RFS tanpa jadwal: got %d, want 200", code)
	}
	if got := statusOf(noSchedule.ID); got != "in_progress" {
		t.Fatalf("status RFS tanpa jadwal = %q, want in_progress", got)
	}
}
