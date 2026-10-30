//go:build integration

// Uji integrasi penutupan Aktivasi/EWO (RFS) dan kemajuan status otomatis saat
// seorang penangan mulai mengerjakan.
//
// Regresi yang dijaga:
//
//  1. "Close Ticket" dulu hanya menyimpan tahap instalasi lalu gagal diam-diam
//     memindahkan status; RFS yang masih `planned` tetap `planned` walau sudah
//     diaktivasi (UI menampilkan Closed/Completed dari tahap, backend planned).
//
//  2. Ikut menangani tiket/RFS yang masih di state awal tidak memajukan status,
//     sehingga pekerjaan berjalan tanpa status "in progress".
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

	created := make([]uuid.UUID, 0, 3)
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
	newRFS := func(owner, status string) *models.WorkItem {
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
	joinRFS := func(u *models.User, id uuid.UUID) int {
		t.Helper()
		body := strings.NewReader(`{"username":"` + u.Username + `"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/collaborators", body)
		return callHandler(t, r, u, id, s.handleAddCollaborator)
	}

	// --- Kasus 1: close dari planned harus menjadi activated. ---
	planned := newRFS(sales.Username, "planned")
	if code := closeRFS(noc1, planned.ID); code != http.StatusOK {
		t.Fatalf("tutup RFS dari planned: got %d, want 200", code)
	}
	got, err := store.GetWorkItem(ctx, planned.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Status != "activated" {
		t.Fatalf("status setelah close = %q, want activated (dulu tertinggal planned)", got.Status)
	}
	if got.RFS == nil || got.RFS.InstallStage != models.RFSStageActivated {
		t.Fatalf("install_stage setelah close = %+v, want activated", got.RFS)
	}
	if got.ClosedAt == nil {
		t.Error("closed_at tidak diisi saat RFS ditutup")
	}

	// Close ulang idempoten (200, tanpa duplikasi masalah).
	if code := closeRFS(noc1, planned.ID); code != http.StatusOK {
		t.Fatalf("tutup ulang RFS: got %d, want 200", code)
	}

	// --- Kasus 2: ikut menangani memajukan status planned → in_progress. ---
	open := newRFS(sales.Username, "planned")
	if code := joinRFS(noc2, open.ID); code != http.StatusOK {
		t.Fatalf("ikut menangani RFS: got %d, want 200", code)
	}
	got, err = store.GetWorkItem(ctx, open.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("status setelah ikut menangani = %q, want in_progress", got.Status)
	}

	// RFS yang sudah ditutup tidak dibuka kembali oleh self-join.
	if code := joinRFS(noc2, planned.ID); code != http.StatusOK {
		t.Fatalf("ikut menangani RFS tertutup: got %d, want 200", code)
	}
	got, err = store.GetWorkItem(ctx, planned.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Status != "activated" {
		t.Fatalf("status RFS tertutup berubah menjadi %q, want tetap activated", got.Status)
	}
}
