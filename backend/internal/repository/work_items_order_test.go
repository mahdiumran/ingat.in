package repository

import (
	"strings"
	"testing"

	"ingatin/backend/internal/models"
)

// TestOpenFirstOrderExpr memverifikasi ekspresi open_first berbentuk CASE dan
// memuat status terminal yang benar untuk setiap tipe item.
func TestOpenFirstOrderExpr(t *testing.T) {
	cases := []struct {
		itemType string
		want     []string
	}{
		{models.ItemIncident, []string{"status = 'closed'"}},
		{models.ItemRequest, []string{"status = 'closed'"}},
		{models.ItemChange, []string{"'completed'", "'rolled_back'", "'cancelled'"}},
		{models.ItemRFS, []string{
			"status IN ('activated','cancelled')",
			"rfs_details",
			"rd.work_item_id = work_items.id",
			"rd.install_stage IN ('activated','cancelled')",
		}},
		// Tipe lain / daftar campuran: status terminal global.
		{"", []string{"'closed'", "'canceled'", "'cancelled'", "'completed'",
			"'rolled_back'", "'activated'", "'resolved'", "'expired'",
			"'fulfilled'", "'done'"}},
	}

	for _, tc := range cases {
		expr := openFirstOrderExpr(tc.itemType)
		if !strings.HasPrefix(expr, "CASE WHEN ") || !strings.HasSuffix(expr, " THEN 1 ELSE 0 END") {
			t.Errorf("tipe %q: bukan ekspresi CASE utuh: %s", tc.itemType, expr)
		}
		for _, w := range tc.want {
			if !strings.Contains(expr, w) {
				t.Errorf("tipe %q: ekspresi %q tidak memuat %q", tc.itemType, expr, w)
			}
		}
	}

	// Tiket incident & request berbagi aturan; change, rfs, dan campuran beda.
	if openFirstOrderExpr(models.ItemIncident) != openFirstOrderExpr(models.ItemRequest) {
		t.Error("incident dan request harus memakai aturan terminal yang sama")
	}
	distinct := map[string]string{}
	for _, mt := range []string{models.ItemIncident, models.ItemChange, models.ItemRFS, ""} {
		e := openFirstOrderExpr(mt)
		if prev, ok := distinct[e]; ok {
			t.Errorf("tipe %q dan %q memakai ekspresi identik", mt, prev)
		}
		distinct[e] = mt
	}
}
