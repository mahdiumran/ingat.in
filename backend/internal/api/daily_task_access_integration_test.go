//go:build integration

// Uji integrasi untuk penjaga tulis work item (F31).
//
// Memerlukan database uji PostgreSQL:
//
//	export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
//	go run ./cmd/ingatin -mode=migrate
//	go test -tags=integration ./internal/api/... -run TestCanModifyItemDailyTask -v
//
// Uji ini TIDAK boleh diarahkan ke database produksi.
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/db"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

func setupTestStore(t *testing.T) (*repository.Store, context.Context) {
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

// TestCanModifyItemDailyTask memverifikasi aturan: daily_task dapat diubah/
// diselesaikan oleh anggota tim yang SAMA, bukan hanya pembuat/owner/admin.
//
// Regresi yang dijaga: handleChangeStatus/handleUpdateWorkItem dulu memakai
// canJoinItemTeam yang TIDAK mengenal aturan tim untuk daily_task, sehingga
// rekan setim selalu ditolak ("NOC lain tidak bisa update/complete task").
func TestCanModifyItemDailyTask(t *testing.T) {
	store, _ := setupTestStore(t)
	s := &Server{cfg: &config.Config{Timezone: "Asia/Jakarta"}, store: store}
	r := httptest.NewRequest("POST", "/api/items/x/status", nil)

	teamA := uuid.New()
	teamB := uuid.New()

	item := &models.WorkItem{
		ItemType:      models.ItemDailyTask,
		CreatedBy:     "alice",
		OwnerUsername: "alice",
		TeamID:        &teamA,
	}

	sameTeam := &models.User{Username: "bob", Role: models.RoleNOC, TeamID: &teamA}
	otherTeam := &models.User{Username: "carol", Role: models.RoleNOC, TeamID: &teamB}
	noTeam := &models.User{Username: "dave", Role: models.RoleNOC}
	creator := &models.User{Username: "alice", Role: models.RoleNOC, TeamID: &teamA}
	admin := &models.User{Username: "root", Role: models.RoleAdmin}

	cases := []struct {
		name string
		user *models.User
		want bool
	}{
		{"anggota tim sama boleh (bug lama: ditolak)", sameTeam, true},
		{"anggota tim lain ditolak", otherTeam, false},
		{"tanpa tim bukan pemilik ditolak", noTeam, false},
		{"pembuat/owner boleh", creator, true},
		{"admin selalu boleh", admin, true},
	}

	for _, tc := range cases {
		got := s.canModifyItem(r, tc.user, item)
		if got != tc.want {
			t.Errorf("canModifyItem %s: got %v, want %v", tc.name, got, tc.want)
		}
	}

	// Dokumentasikan perilaku penjaga lama: canJoinItemTeam TIDAK memberi akses
	// rekan setim pada daily_task — inilah akar masalahnya.
	if s.canJoinItemTeam(r, sameTeam, item) {
		t.Error("canJoinItemTeam sengaja tidak mengenal tim untuk daily_task; " +
			"bila ini berubah, penjaga tulis harus ditinjau ulang")
	}
}

// TestSameTeamDailyTaskHandlers adalah uji end-to-end: anggota tim yang SAMA
// benar-benar dapat MENYELESAIKAN dan MENGUBAH daily_task milik rekannya lewat
// HTTP handler (bukan hanya helper izin), sementara anggota tim lain ditolak.
//
// Regresi yang dijaga: "NOC lain tidak bisa update/complete daily task" —
// handler dulu memakai canJoinItemTeam sehingga hanya pembuat/owner/admin lolos.
func TestSameTeamDailyTaskHandlers(t *testing.T) {
	store, ctx := setupTestStore(t)
	s := &Server{cfg: &config.Config{Timezone: "Asia/Jakarta"}, store: store}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	ownerUser := "it_owner_" + suffix
	mateUser := "it_mate_" + suffix
	otherUser := "it_other_" + suffix

	teamA, err := store.CreateTeam(ctx, "IT-A "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim A: %v", err)
	}
	teamB, err := store.CreateTeam(ctx, "IT-B "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim B: %v", err)
	}

	newUser := func(name string, teamID *uuid.UUID) *models.User {
		u, err := store.CreateUser(ctx, repository.CreateUserParams{
			Username:     name,
			Email:        name + "@example.test",
			FullName:     name,
			PasswordHash: "x",
			Role:         models.RoleNOC,
			TeamID:       teamID,
		})
		if err != nil {
			t.Fatalf("buat user %s: %v", name, err)
		}
		return u
	}
	owner := newUser(ownerUser, &teamA.ID)
	mate := newUser(mateUser, &teamA.ID)
	other := newUser(otherUser, &teamB.ID)

	// Daily task milik owner di tim A.
	var itemID uuid.UUID
	err = store.Tx(ctx, func(tx pgx.Tx) error {
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:         "DT-IT-" + suffix,
			ItemType:      models.ItemDailyTask,
			Title:         "Piket pagi",
			Priority:      "normal",
			Status:        "pending",
			OwnerUsername: ownerUser,
			TeamID:        &teamA.ID,
			CreatedBy:     ownerUser,
		})
		if err != nil {
			return err
		}
		itemID = wi.ID
		return store.CreateTaskDetails(ctx, tx, wi.ID, nil, nil, "")
	})
	if err != nil {
		t.Fatalf("buat daily task: %v", err)
	}
	t.Cleanup(func() {
		p := store.Pool()
		_, _ = p.Exec(ctx, "DELETE FROM work_items WHERE id=$1", itemID)
		for _, u := range []uuid.UUID{owner.ID, mate.ID, other.ID} {
			_, _ = p.Exec(ctx, "DELETE FROM users WHERE id=$1", u)
		}
		_, _ = p.Exec(ctx, "DELETE FROM teams WHERE id IN ($1,$2)", teamA.ID, teamB.ID)
	})

	postDone := func(u *models.User) int {
		t.Helper()
		body := strings.NewReader(`{"status":"done","note":"selesai piket","result_status":"normal"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+itemID.String()+"/status", body)
		return callHandler(t, r, u, itemID, s.handleChangeStatus)
	}

	// Rekan setim: harus 200 dan status benar-benar berubah jadi done.
	if code := postDone(mate); code != http.StatusOK {
		t.Fatalf("rekan setim menyelesaikan daily task: got %d, want 200", code)
	}
	got, err := store.GetWorkItem(ctx, itemID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("status setelah diselesaikan rekan setim = %q, want done", got.Status)
	}

	// Kembalikan ke pending agar transisi berikutnya sah.
	if err := store.Tx(ctx, func(tx pgx.Tx) error {
		return store.UpdateWorkItemFields(ctx, tx, itemID, map[string]any{"status": "pending"})
	}); err != nil {
		t.Fatalf("reset status: %v", err)
	}

	// Anggota tim lain: harus 403 dan status tidak berubah.
	if code := postDone(other); code != http.StatusForbidden {
		t.Fatalf("anggota tim lain: got %d, want 403", code)
	}
	got, err = store.GetWorkItem(ctx, itemID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Status != "pending" {
		t.Fatalf("status setelah percobaan tim lain = %q, want tetap pending", got.Status)
	}

	// Bagian "update": perubahan isi (deskripsi) oleh rekan setim harus 200,
	// sedangkan anggota tim lain tetap 403.
	patchDesc := func(u *models.User, desc string) int {
		t.Helper()
		body := strings.NewReader(`{"description":"` + desc + `"}`)
		r := httptest.NewRequest(http.MethodPatch, "/api/items/"+itemID.String(), body)
		return callHandler(t, r, u, itemID, s.handleUpdateWorkItem)
	}
	if code := patchDesc(other, "diubah tim lain"); code != http.StatusForbidden {
		t.Fatalf("anggota tim lain mengubah isi: got %d, want 403", code)
	}
	if code := patchDesc(mate, "diubah rekan setim"); code != http.StatusOK {
		t.Fatalf("rekan setim mengubah isi: got %d, want 200", code)
	}
	got, err = store.GetWorkItem(ctx, itemID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if got.Description != "diubah rekan setim" {
		t.Fatalf("deskripsi = %q, want diubah rekan setim", got.Description)
	}
}

// callHandler menjalankan handler item dengan path param {id} dan user terautentikasi.
func callHandler(t *testing.T, r *http.Request, u *models.User, itemID uuid.UUID, h http.HandlerFunc) int {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itemID.String())
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxUserKey, u)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h(w, r)
	return w.Code
}
