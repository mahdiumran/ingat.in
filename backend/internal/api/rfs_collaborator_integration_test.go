//go:build integration

// Uji integrasi "ikut menangani" pada Aktivasi/EWO (RFS).
//
// Regresi yang dijaga:
//
//  1. Penangan RFS tidak muncul pada detail item karena loadExtensions hanya
//     memuat collaborator untuk IsTicket() → UI selalu "Ikut Menangani (0)".
//
//  2. Akibat (1), isCollaboratorOf selalu false sehingga penangan lintas tim
//     ditolak 403 saat mengubah deskripsi/detail RFS dan saat menutup RFS
//     ("hanya pembuat item atau admin yang dapat mengubah deskripsi/detail").
//
//  3. "Team ikut menangani": seluruh anggota tim penekan tombol Ikut/Kerjakan
//     ikut tercatat sebagai penangan.
//
//     export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
//     go test -tags=integration ./internal/api/... -run TestRFSCollaborator -v
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

// TestRFSCollaboratorHandling mencakup self-join, ekspansi tim, tampilan pada
// detail item, izin update isi, dan izin menutup RFS.
func TestRFSCollaboratorHandling(t *testing.T) {
	store, ctx := setupTestStore(t)
	cfg := &config.Config{Timezone: "Asia/Jakarta", OutboxMaxAttempts: 3}
	s := &Server{cfg: cfg, store: store, notifier: notify.NewResolver(cfg, store)}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	teamA, err := store.CreateTeam(ctx, "IT-RFS-A "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim A: %v", err)
	}
	teamB, err := store.CreateTeam(ctx, "IT-RFS-B "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim B: %v", err)
	}
	teamC, err := store.CreateTeam(ctx, "IT-RFS-C "+suffix, "", "other", nil)
	if err != nil {
		t.Fatalf("buat tim C: %v", err)
	}

	newUser := func(name string, role string, teamID *uuid.UUID) *models.User {
		t.Helper()
		u, err := store.CreateUser(ctx, repository.CreateUserParams{
			Username:     name,
			Email:        name + "@example.test",
			FullName:     name,
			PasswordHash: "x",
			Role:         role,
			TeamID:       teamID,
		})
		if err != nil {
			t.Fatalf("buat user %s: %v", name, err)
		}
		return u
	}

	sales := newUser("it_sales_"+suffix, models.RoleSales, &teamA.ID)
	joiner := newUser("it_join_"+suffix, models.RoleNOC, &teamB.ID)
	mate := newUser("it_mate_"+suffix, models.RoleNOC, &teamB.ID)
	outsider := newUser("it_out_"+suffix, models.RoleNOC, &teamC.ID)
	viewer := newUser("it_view_"+suffix, models.RoleViewer, &teamB.ID)

	created := make([]uuid.UUID, 0, 2)
	t.Cleanup(func() {
		p := store.Pool()
		for _, id := range created {
			_, _ = p.Exec(ctx, "DELETE FROM work_items WHERE id=$1", id)
		}
		for _, u := range []*models.User{sales, joiner, mate, outsider, viewer} {
			_, _ = p.Exec(ctx, "DELETE FROM users WHERE id=$1", u.ID)
		}
		_, _ = p.Exec(ctx, "DELETE FROM teams WHERE id IN ($1,$2,$3)", teamA.ID, teamB.ID, teamC.ID)
	})

	seq := 0
	newRFS := func(owner string) *models.WorkItem {
		t.Helper()
		seq++
		var wi *models.WorkItem
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			wi, err = store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
				RefNo:         fmt.Sprintf("RFS-IT-%s-%d", suffix, seq),
				ItemType:      models.ItemRFS,
				Title:         "Aktivasi uji " + suffix,
				Priority:      "normal",
				Status:        "planned",
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

	join := func(u *models.User, id uuid.UUID, target string) int {
		t.Helper()
		body := strings.NewReader(`{"username":"` + target + `"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/collaborators", body)
		return callHandler(t, r, u, id, s.handleAddCollaborator)
	}
	patch := func(u *models.User, id uuid.UUID, body string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPatch, "/api/items/"+id.String(), strings.NewReader(body))
		return callHandler(t, r, u, id, s.handleUpdateWorkItem)
	}
	changeStatus := func(u *models.User, id uuid.UUID, status string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/status",
			strings.NewReader(`{"status":"`+status+`"}`))
		return callHandler(t, r, u, id, s.handleChangeStatus)
	}
	collaboratorNames := func(id uuid.UUID) []string {
		t.Helper()
		it, err := store.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatalf("baca item: %v", err)
		}
		out := make([]string, 0, len(it.Collaborators))
		for _, c := range it.Collaborators {
			out = append(out, strings.ToLower(c.Username))
		}
		return out
	}
	hasCollab := func(names []string, want string) bool {
		for _, n := range names {
			if n == strings.ToLower(want) {
				return true
			}
		}
		return false
	}

	// --- Kasus 1: RFS milik Sales (tim A), NOC tim B menekan "Ikut RFS". ---
	rfs := newRFS(sales.Username)

	if code := join(joiner, rfs.ID, joiner.Username); code != http.StatusOK {
		t.Fatalf("NOC ikut RFS: got %d, want 200", code)
	}

	// Bug 1: collaborator harus muncul pada detail item (GetWorkItem).
	names := collaboratorNames(rfs.ID)
	if !hasCollab(names, joiner.Username) {
		t.Fatalf("penangan %s tidak muncul pada detail RFS: %v", joiner.Username, names)
	}
	// "Team ikut menangani": rekan setim otomatis tercatat.
	if !hasCollab(names, mate.Username) {
		t.Fatalf("rekan tim %s tidak otomatis tercatat sebagai penangan: %v", mate.Username, names)
	}
	// Viewer tidak boleh menjadi penangan (tidak dapat menulis).
	if hasCollab(names, viewer.Username) {
		t.Fatalf("viewer %s tidak boleh ikut menjadi penangan: %v", viewer.Username, names)
	}
	// Anggota tim lain tidak ikut tercatat.
	if hasCollab(names, outsider.Username) {
		t.Fatalf("anggota tim lain %s tidak boleh tercatat: %v", outsider.Username, names)
	}

	// Bug 2a: penangan dapat mengubah isi RFS (deskripsi + catatan penanganan).
	if code := patch(joiner, rfs.ID, `{"description":"diperbarui penangan"}`); code != http.StatusOK {
		t.Fatalf("penangan mengubah deskripsi RFS: got %d, want 200", code)
	}
	if code := patch(joiner, rfs.ID,
		`{"rfs":{"issue_found":"kabel longgar","troubleshooting":"cek ODP","action_solution":"rapikan"}}`); code != http.StatusOK {
		t.Fatalf("penangan menyimpan catatan penanganan RFS: got %d, want 200", code)
	}
	// Rekan setim (otomatis penangan) juga boleh mengubah.
	if code := patch(mate, rfs.ID, `{"rfs":{"issue_found":"dicek rekan tim"}}`); code != http.StatusOK {
		t.Fatalf("rekan setim mengubah catatan RFS: got %d, want 200", code)
	}

	// Bug 2b: penangan dapat menutup RFS (install_stage activated + status).
	// Alur workflow RFS: planned → in_progress → activated.
	if code := changeStatus(joiner, rfs.ID, "in_progress"); code != http.StatusOK {
		t.Fatalf("penangan memulai RFS: got %d, want 200", code)
	}
	if code := patch(joiner, rfs.ID, `{"rfs":{"install_stage":"activated"}}`); code != http.StatusOK {
		t.Fatalf("penangan menutup RFS (install_stage): got %d, want 200", code)
	}
	if code := changeStatus(joiner, rfs.ID, "activated"); code != http.StatusOK {
		t.Fatalf("penangan mengubah status RFS ke activated: got %d, want 200", code)
	}
	closed, err := store.GetWorkItem(ctx, rfs.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if closed.Status != "activated" {
		t.Fatalf("status RFS = %q, want activated", closed.Status)
	}
	if closed.RFS == nil || closed.RFS.InstallStage != "activated" {
		t.Fatalf("install_stage RFS tidak tersimpan activated: %+v", closed.RFS)
	}

	// --- Kasus 2: bukan penangan (tim C) tetap ditolak. ---
	other := newRFS(sales.Username)
	if code := patch(outsider, other.ID, `{"rfs":{"issue_found":"x"}}`); code != http.StatusForbidden {
		t.Fatalf("non-penangan mengubah RFS: got %d, want 403", code)
	}
	if code := changeStatus(outsider, other.ID, "activated"); code != http.StatusForbidden {
		t.Fatalf("non-penangan menutup RFS: got %d, want 403", code)
	}
	// Viewer juga ditolak.
	if code := patch(viewer, other.ID, `{"rfs":{"issue_found":"x"}}`); code != http.StatusForbidden {
		t.Fatalf("viewer mengubah RFS: got %d, want 403", code)
	}

	// --- Kasus 3: RFS tanpa owner → "Kerjakan" mencatat owner + seluruh tim. ---
	unclaimed := newRFS("")
	assign := func(u *models.User, id uuid.UUID) int {
		t.Helper()
		body := strings.NewReader(`{"owner_username":"` + u.Username + `"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/assign", body)
		return callHandler(t, r, u, id, s.handleAssignItem)
	}
	if code := assign(joiner, unclaimed.ID); code != http.StatusOK {
		t.Fatalf("NOC mengerjakan RFS tanpa owner: got %d, want 200", code)
	}
	claimed, err := store.GetWorkItem(ctx, unclaimed.ID)
	if err != nil {
		t.Fatalf("baca item: %v", err)
	}
	if !strings.EqualFold(claimed.OwnerUsername, joiner.Username) {
		t.Fatalf("owner RFS = %q, want %q", claimed.OwnerUsername, joiner.Username)
	}
	names = collaboratorNames(unclaimed.ID)
	if !hasCollab(names, joiner.Username) || !hasCollab(names, mate.Username) {
		t.Fatalf("self-claim tidak mencatat penangan + rekan tim: %v", names)
	}
	// Rekan setim hasil ekspansi juga berhak menutup RFS tersebut.
	if code := changeStatus(mate, unclaimed.ID, "in_progress"); code != http.StatusOK {
		t.Fatalf("rekan tim mengubah status RFS hasil claim: got %d, want 200", code)
	}
}
