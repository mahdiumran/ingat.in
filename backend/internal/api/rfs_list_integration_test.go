//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
)

// TestListRFSIncludesDetails menjaga regresi: daftar AKTIVASI/EWO
// (GET /api/items?type=rfs) harus memuat extension rfs_details.
//
// Sebelumnya ListWorkItems tidak memuat rfs_details sehingga kolom Customer,
// Product, dan Bandwidth di UI selalu "—" walau data sudah diisi, dan filter
// tahap instalasi ikut salah.
func TestListRFSIncludesDetails(t *testing.T) {
	store, ctx := setupTestStore(t)
	cfg := &config.Config{Timezone: "Asia/Jakarta", OutboxMaxAttempts: 3}
	s := &Server{cfg: cfg, store: store, notifier: notify.NewResolver(cfg, store)}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	customer := "PT Daftar RFS " + suffix
	bandwidth := "150 Mbps"
	pkg := "Dedicated-" + suffix

	u, err := store.CreateUser(ctx, repository.CreateUserParams{
		Username: "it_rfslist_" + suffix, Email: "it_rfslist_" + suffix + "@example.test",
		FullName: "IT RFS List", PasswordHash: "x", Role: models.RoleNOC,
	})
	if err != nil {
		t.Fatalf("buat user: %v", err)
	}

	var itemID uuid.UUID
	err = store.Tx(ctx, func(tx pgx.Tx) error {
		wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:         "RFS-LIST-" + suffix,
			ItemType:      models.ItemRFS,
			Title:         "Aktivasi daftar " + suffix,
			Priority:      "normal",
			Status:        "new",
			CreatedBy:     u.Username,
			OwnerUsername: u.Username,
		})
		if err != nil {
			return err
		}
		itemID = wi.ID
		return store.CreateRFSDetails(ctx, tx, wi.ID, repository.CreateRFSDetailsParams{
			CustomerName:   customer,
			ServicePackage: pkg,
			Bandwidth:      bandwidth,
			InstallStage:   models.RFSStagePlanned,
		})
	})
	if err != nil {
		t.Fatalf("buat RFS: %v", err)
	}
	t.Cleanup(func() {
		p := store.Pool()
		_, _ = p.Exec(ctx, "DELETE FROM work_items WHERE id=$1", itemID)
		_, _ = p.Exec(ctx, "DELETE FROM users WHERE id=$1", u.ID)
	})

	// 1) Daftar tanpa filter pencarian: detail RFS ikut terkirim.
	list := func(t *testing.T, rawQuery string) ([]models.WorkItem, int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/items?"+rawQuery, nil)
		r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, u))
		w := httptest.NewRecorder()
		s.handleListWorkItems(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("list %q: got %d, body=%s", rawQuery, w.Code, w.Body.String())
		}
		var resp struct {
			Items []models.WorkItem `json:"items"`
			Total int               `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return resp.Items, resp.Total
	}
	find := func(items []models.WorkItem) *models.WorkItem {
		for i := range items {
			if items[i].ID == itemID {
				return &items[i]
			}
		}
		return nil
	}

	items, total := list(t, "type=rfs&limit=100&order=expire_at&desc=false")
	if total == 0 {
		t.Fatal("total RFS 0, ingin minimal 1")
	}
	got := find(items)
	if got == nil {
		t.Fatalf("RFS %s tidak ada pada daftar", itemID)
	}
	// Judul tetap berasal dari work_items.title dan independen dari customer.
	if wantTitle := "Aktivasi daftar " + suffix; got.Title != wantTitle {
		t.Errorf("title = %q, want %q", got.Title, wantTitle)
	}
	if got.RFS == nil {
		t.Fatal("rfs_details tidak ikut pada response daftar (rfs nil)")
	}
	if got.RFS.CustomerName == got.Title {
		t.Error("title tidak boleh tertimpa customer_name")
	}
	if got.RFS.CustomerName != customer {
		t.Errorf("customer_name = %q, want %q", got.RFS.CustomerName, customer)
	}
	if got.RFS.Bandwidth != bandwidth {
		t.Errorf("bandwidth = %q, want %q", got.RFS.Bandwidth, bandwidth)
	}
	if got.RFS.ServicePackage != pkg {
		t.Errorf("service_package = %q, want %q", got.RFS.ServicePackage, pkg)
	}
	if got.RFS.InstallStage != models.RFSStagePlanned {
		t.Errorf("install_stage = %q, want %q", got.RFS.InstallStage, models.RFSStagePlanned)
	}

	// 2) Pencarian memakai nama customer (placeholder UI: "customer, product,
	// ref…") harus menemukan item yang sama.
	searchItems, _ := list(t, "type=rfs&limit=100&q="+url.QueryEscape(customer))
	if find(searchItems) == nil {
		t.Fatalf("pencarian customer %q tidak menemukan RFS %s", customer, itemID)
	}

	// 3) Pencarian memakai paket layanan juga harus menemukan item.
	pkgItems, _ := list(t, "type=rfs&limit=100&q="+url.QueryEscape(pkg))
	if find(pkgItems) == nil {
		t.Fatalf("pencarian paket %q tidak menemukan RFS %s", pkg, itemID)
	}

	// 4) Sanity: item non-RFS tetap berfungsi (tidak error karena batch loader).
	if _, _, err := store.ListWorkItems(ctx, repository.ListWorkItemsParams{
		ItemType: models.ItemIncident, Limit: 5,
	}); err != nil {
		t.Fatalf("ListWorkItems non-RFS: %v", err)
	}
}
