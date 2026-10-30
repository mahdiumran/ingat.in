//go:build integration

// Uji integrasi: mengambil tiket tanpa owner ("Kerjakan") otomatis memindahkan
// status ke in_progress (incident/request) atau implementing (change scheduled),
// serta tidak menyentuh tiket yang sudah punya owner.
//
//	export INGATIN_DB_URL='postgres://ingatin:<pw>@127.0.0.1:5432/ingatin_test?sslmode=disable'
//	go test -tags=integration ./internal/api/... -run TestClaimTicketAutoProgress -v
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

func TestClaimTicketAutoProgress(t *testing.T) {
	store, ctx := setupTestStore(t)
	cfg := &config.Config{Timezone: "Asia/Jakarta", OutboxMaxAttempts: 3}
	s := &Server{cfg: cfg, store: store, notifier: notify.NewResolver(cfg, store)}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	claimant := "it_claim_" + suffix

	created := make([]uuid.UUID, 0, 4)
	t.Cleanup(func() {
		p := store.Pool()
		for _, id := range created {
			_, _ = p.Exec(ctx, "DELETE FROM work_items WHERE id=$1", id)
		}
	})

	seq := 0
	newTicket := func(itemType, status, owner string) *models.WorkItem {
		t.Helper()
		seq++
		var wi *models.WorkItem
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			wi, err = store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
				RefNo:         fmt.Sprintf("%s-IT-%s-%d", strings.ToUpper(itemType[:3]), suffix, seq),
				ItemType:      itemType,
				Title:         "Uji claim " + suffix,
				Priority:      "normal",
				Status:        status,
				OwnerUsername: owner,
				CreatedBy:     "it_creator_" + suffix,
			})
			if err != nil {
				return err
			}
			return store.CreateTicketDetails(ctx, tx, wi.ID, repository.CreateTicketDetailsParams{})
		})
		if err != nil {
			t.Fatalf("buat tiket %s/%s: %v", itemType, status, err)
		}
		created = append(created, wi.ID)
		return wi
	}

	selfClaim := func(id uuid.UUID) int {
		t.Helper()
		body := strings.NewReader(`{"owner_username":"` + claimant + `"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/items/"+id.String()+"/assign", body)
		return callHandler(t, r, &models.User{Username: claimant, Role: models.RoleNOC}, id, s.handleAssignItem)
	}

	statusOf := func(id uuid.UUID) (string, string) {
		t.Helper()
		it, err := store.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatalf("baca tiket: %v", err)
		}
		return it.Status, it.OwnerUsername
	}

	t.Run("incident new menjadi in_progress", func(t *testing.T) {
		it := newTicket(models.ItemIncident, "new", "")
		if code := selfClaim(it.ID); code != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", code)
		}
		status, owner := statusOf(it.ID)
		if status != "in_progress" || owner != claimant {
			t.Fatalf("hasil claim = status %q owner %q, want in_progress/%s", status, owner, claimant)
		}
	})

	t.Run("request assigned menjadi in_progress", func(t *testing.T) {
		it := newTicket(models.ItemRequest, "assigned", "")
		if code := selfClaim(it.ID); code != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", code)
		}
		if status, _ := statusOf(it.ID); status != "in_progress" {
			t.Fatalf("status = %q, want in_progress", status)
		}
	})

	t.Run("change scheduled menjadi implementing", func(t *testing.T) {
		it := newTicket(models.ItemChange, "scheduled", "")
		if code := selfClaim(it.ID); code != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", code)
		}
		if status, _ := statusOf(it.ID); status != "implementing" {
			t.Fatalf("status = %q, want implementing", status)
		}
	})

	t.Run("change draft tidak dipaksa pindah", func(t *testing.T) {
		it := newTicket(models.ItemChange, "draft", "")
		if code := selfClaim(it.ID); code != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", code)
		}
		if status, _ := statusOf(it.ID); status != "draft" {
			t.Fatalf("status = %q, want tetap draft", status)
		}
	})

	t.Run("tiket dengan owner tidak berubah status", func(t *testing.T) {
		it := newTicket(models.ItemIncident, "new", claimant)
		if code := selfClaim(it.ID); code != http.StatusOK {
			t.Fatalf("claim ulang status = %d, want 200", code)
		}
		if status, _ := statusOf(it.ID); status != "new" {
			t.Fatalf("status = %q, want tetap new", status)
		}
	})
}
