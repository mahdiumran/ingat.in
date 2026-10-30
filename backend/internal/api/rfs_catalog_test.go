package api

import "testing"

// TestRFSTagAllowed memverifikasi pencocokan tag RFS: cocok dengan kode/label
// ewo_tag aktif, nilai legacy diizinkan, dan tag tak dikenal ditolak.
func TestRFSTagAllowed(t *testing.T) {
	allowed := []string{"new_install", "New Install", "urgent"}
	legacy := []string{"tag_lama"}

	cases := []struct {
		name string
		tag  string
		want bool
	}{
		{"cocok kode", "new_install", true},
		{"cocok label", "New Install", true},
		{"cocok tanpa peduli huruf besar-kecil", "URGENT", true},
		{"dengan spasi", "  urgent  ", true},
		{"legacy diizinkan", "tag_lama", true},
		{"tak dikenal ditolak", "tag_baru", false},
		{"kosong selalu sah", "  ", true},
	}
	for _, tc := range cases {
		if got := rfsTagAllowed(tc.tag, allowed, legacy); got != tc.want {
			t.Errorf("%s: rfsTagAllowed(%q) = %v, want %v", tc.name, tc.tag, got, tc.want)
		}
	}
}

// TestRFSTagAllowedNoLegacy memastikan legacy hanya berlaku bila diberikan.
func TestRFSTagAllowedNoLegacy(t *testing.T) {
	if rfsTagAllowed("tag_lama", []string{"urgent"}, nil) {
		t.Error("tag legacy seharusnya ditolak tanpa daftar allowLegacy")
	}
}

// TestRFSMaterialUpdate memverifikasi sebuah PATCH dianggap material (memicu
// ringkasan on_change) hanya untuk field yang relevan.
func TestRFSMaterialUpdate(t *testing.T) {
	str := "x"
	material := []workItemUpdateRequest{
		{RFS: &rfsDetailsRequest{}},
		{Title: &str},
		{Owner: &str},
		{TeamID: &str},
		{DueAt: &str},
		{StartAt: &str},
		{ExpireAt: &str},
		{Tags: []string{"urgent"}},
	}
	for i, req := range material {
		if !rfsMaterialUpdate(req) {
			t.Errorf("patch material #%d seharusnya true", i)
		}
	}

	nonMaterial := []workItemUpdateRequest{
		{},
		{Priority: &str},
		{Description: &str},
		{DeviceRef: &str},
		{},
	}
	for i, req := range nonMaterial {
		if rfsMaterialUpdate(req) {
			t.Errorf("patch non-material #%d seharusnya false", i)
		}
	}
}
