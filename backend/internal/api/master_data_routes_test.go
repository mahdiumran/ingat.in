package api

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Instalasi Baru":    "instalasi_baru",
		"EWO":               "ewo",
		"  Lokasi POP  ":    "lokasi_pop",
		"Trial-3 Hari":      "trial_3_hari",
		"":                  "",
		"A   B":             "a_b",
		"--Sudah--Unders--": "sudah_unders",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsValidKindSlug(t *testing.T) {
	valid := []string{"pop", "ticket_category", "product2", "a"}
	for _, s := range valid {
		if !isValidKindSlug(s) {
			t.Errorf("isValidKindSlug(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "POP", "ticket-category", "kategori tiket", "a.b", "k/1"}
	for _, s := range invalid {
		if isValidKindSlug(s) {
			t.Errorf("isValidKindSlug(%q) = true, want false", s)
		}
	}
}

func TestIsKnownRole(t *testing.T) {
	valid := []string{"admin", "noc", "agent", "sales", "viewer", "customer"}
	for _, r := range valid {
		if !isKnownRole(r) {
			t.Errorf("isKnownRole(%q) = false, want true", r)
		}
	}
	if isKnownRole("superuser") {
		t.Errorf("isKnownRole(superuser) = true, want false")
	}
}

func TestIsKnownItemTypeAndPriority(t *testing.T) {
	for _, it := range []string{"task", "reminder", "rfs", "incident", "request", "change"} {
		if !isKnownItemType(it) {
			t.Errorf("isKnownItemType(%q) = false, want true", it)
		}
	}
	if isKnownItemType("ticket") {
		t.Errorf("isKnownItemType(ticket) = true, want false (bukan item_type generik)")
	}
	for _, p := range []string{"low", "normal", "high", "critical"} {
		if !isKnownPriority(p) {
			t.Errorf("isKnownPriority(%q) = false, want true", p)
		}
	}
	if isKnownPriority("urgent") {
		t.Errorf("isKnownPriority(urgent) = true, want false")
	}
}

func TestValidateCustomerMeta(t *testing.T) {
	cases := []struct {
		name    string
		meta    map[string]any
		wantErr bool
	}{
		{"personal tanpa pic", map[string]any{"jenis": "personal"}, false},
		{"corporate dengan pic", map[string]any{"jenis": "corporate", "pic": "Budi"}, false},
		{"corporate tanpa pic", map[string]any{"jenis": "corporate"}, true},
		{"corporate pic kosong spasi", map[string]any{"jenis": "corporate", "pic": "   "}, true},
		{"jenis kosong", map[string]any{}, true},
		{"jenis tidak dikenal", map[string]any{"jenis": "yayasan"}, true},
		{"jenis kapitalisasi", map[string]any{"jenis": "Corporate", "pic": "A"}, true},
		{"meta nil", nil, true},
	}
	for _, tc := range cases {
		err := validateCustomerMeta(tc.meta)
		if tc.wantErr && err == nil {
			t.Errorf("%s: validateCustomerMeta = nil, want error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: validateCustomerMeta = %v, want nil", tc.name, err)
		}
	}
}

func TestMetaString(t *testing.T) {
	if got := metaString(map[string]any{"pic": "  Budi  "}, "pic"); got != "Budi" {
		t.Errorf("metaString trim = %q, want Budi", got)
	}
	if got := metaString(map[string]any{"capacity": 100}, "capacity"); got != "100" {
		t.Errorf("metaString non-string = %q, want 100", got)
	}
	if got := metaString(map[string]any{"x": nil}, "x"); got != "" {
		t.Errorf("metaString nil = %q, want empty", got)
	}
	if got := metaString(nil, "pic"); got != "" {
		t.Errorf("metaString nil map = %q, want empty", got)
	}
	if got := metaString(map[string]any{}, "missing"); got != "" {
		t.Errorf("metaString missing = %q, want empty", got)
	}
}
