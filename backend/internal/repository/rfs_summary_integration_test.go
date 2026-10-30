//go:build integration

package repository_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/workitems"
)

// TestListOpenRFSSummary memverifikasi kueri ringkasan Aktivasi/EWO (RFS)
// terhadap database uji:
//
//   - status terminal (activated/cancelled) dikecualikan;
//   - pengelompokan status: planned+postponed, in_progress+in_progress_field,
//     pending_troubleshoot;
//   - snapshot customer/kategori, owner fallback, tag JSONB, dan overdue.
//
// Jalankan dengan `go test -tags=integration ./internal/repository/...`.
func TestListOpenRFSSummary(t *testing.T) {
	store, ctx := setupStore(t)

	type seedInput struct {
		status   string
		overdue  bool
		customer string
		category string
		owner    string
		picNOC   string
		tags     []string
	}

	past := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(48 * time.Hour)

	// seed membuat satu work item RFS + rfs_details dan mengembalikan ref_no.
	seed := func(in seedInput) string {
		t.Helper()
		var refNo string
		var id uuid.UUID
		err := store.Tx(ctx, func(tx pgx.Tx) error {
			ref, err := workitems.NextRefNo(ctx, tx, models.ItemRFS, time.Now())
			if err != nil {
				return err
			}
			expire := future
			if in.overdue {
				expire = past
			}
			wi, err := store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
				RefNo: ref, ItemType: models.ItemRFS, Title: "RFS uji " + in.status,
				Priority: "normal", Status: in.status, Source: "test",
				OwnerUsername: in.owner, ExpireAt: &expire,
				Tags: in.tags, CreatedBy: "integration-test",
			})
			if err != nil {
				return err
			}
			refNo, id = ref, wi.ID
			// Snapshot kategori: code+label disalin dari master data (di sini
			// dipakai nilai label yang sama agar mudah diverifikasi).
			return store.CreateRFSDetails(ctx, tx, wi.ID, repository.CreateRFSDetailsParams{
				CustomerName:  in.customer,
				PicNOC:        in.picNOC,
				CategoryCode:  in.category,
				CategoryLabel: in.category,
			})
		})
		if err != nil {
			t.Fatalf("seed %s: %v", in.status, err)
		}
		t.Cleanup(func() { _, _ = store.SoftDeleteWorkItem(ctx, id) })
		return refNo
	}

	plannedOverdue := seed(seedInput{status: "planned", overdue: true, customer: "PT Alpha", category: "Aktivasi", tags: []string{"urgent", "new_install"}})
	postponed := seed(seedInput{status: "postponed", customer: "PT Beta", picNOC: "NOC-Beta"})
	inProgress := seed(seedInput{status: "in_progress", owner: "agent1", customer: "PT Gamma", category: "Downgrade"})
	inField := seed(seedInput{status: "in_progress_field", customer: "PT Delta"})
	pending := seed(seedInput{status: "pending_troubleshoot", customer: "PT Epsilon"})
	activated := seed(seedInput{status: "activated", customer: "PT Zeta"})
	cancelled := seed(seedInput{status: "cancelled", customer: "PT Eta"})

	sum, err := store.ListOpenRFSSummary(ctx)
	if err != nil {
		t.Fatalf("ListOpenRFSSummary: %v", err)
	}

	groupOf := func(ref string) string {
		for _, it := range sum.Planned {
			if it.RefNo == ref {
				return "planned"
			}
		}
		for _, it := range sum.InProgress {
			if it.RefNo == ref {
				return "in_progress"
			}
		}
		for _, it := range sum.Pending {
			if it.RefNo == ref {
				return "pending"
			}
		}
		return "absent"
	}

	wantGroup := map[string]string{
		plannedOverdue: "planned",
		postponed:      "planned",
		inProgress:     "in_progress",
		inField:        "in_progress",
		pending:        "pending",
	}
	for ref, want := range wantGroup {
		if got := groupOf(ref); got != want {
			t.Errorf("ref %s: kelompok = %q, ingin %q", ref, got, want)
		}
	}
	if got := groupOf(activated); got != "absent" {
		t.Errorf("RFS activated seharusnya dikecualikan, dapat kelompok %q", got)
	}
	if got := groupOf(cancelled); got != "absent" {
		t.Errorf("RFS cancelled seharusnya dikecualikan, dapat kelompok %q", got)
	}

	find := func(ref string) *repository.OpenRFSSummaryItem {
		for _, list := range [][]repository.OpenRFSSummaryItem{sum.Planned, sum.InProgress, sum.Pending} {
			for i := range list {
				if list[i].RefNo == ref {
					return &list[i]
				}
			}
		}
		return nil
	}

	po := find(plannedOverdue)
	if po == nil {
		t.Fatal("baris planned overdue tidak ditemukan")
	}
	if !po.Overdue {
		t.Error("RFS dengan expire_at lampau seharusnya Overdue=true")
	}
	if po.Customer != "PT Alpha" {
		t.Errorf("customer = %q, ingin PT Alpha", po.Customer)
	}
	if po.Category != "Aktivasi" {
		t.Errorf("category = %q, ingin Aktivasi", po.Category)
	}
	if len(po.Tags) != 2 {
		t.Errorf("tags = %#v, ingin 2 tag", po.Tags)
	}

	// Owner fallback: owner_username kosong -> pic_noc.
	post := find(postponed)
	if post == nil {
		t.Fatal("baris postponed tidak ditemukan")
	}
	if post.Owner != "NOC-Beta" {
		t.Errorf("owner fallback = %q, ingin NOC-Beta", post.Owner)
	}
	if post.Overdue {
		t.Error("RFS dengan expire_at mendatang seharusnya Overdue=false")
	}

	// Owner langsung dari work_items.owner_username (prioritas tertinggi).
	ip := find(inProgress)
	if ip == nil {
		t.Fatal("baris in_progress tidak ditemukan")
	}
	if ip.Owner != "agent1" {
		t.Errorf("owner = %q, ingin agent1", ip.Owner)
	}
	if ip.Category != "Downgrade" {
		t.Errorf("category = %q, ingin Downgrade", ip.Category)
	}
}
