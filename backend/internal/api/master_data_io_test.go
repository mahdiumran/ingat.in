package api

import (
	"strings"
	"testing"
)

// TestMapCustomerHeader memastikan header CSV dikenali (F40).
func TestMapCustomerHeader(t *testing.T) {
	idx, err := mapCustomerHeader([]string{"kode", "cid", "nama", "jenis", "pic", "telepon", "email", "alamat", "kapasitas"})
	if err != nil {
		t.Fatalf("header lengkap ditolak: %v", err)
	}
	if idx["nama"] != 2 || idx["jenis"] != 3 || idx["cid"] != 1 {
		t.Errorf("pemetaan salah: %v", idx)
	}
	if _, err := mapCustomerHeader([]string{"nama"}); err == nil {
		t.Error("header tanpa 'jenis' harus ditolak")
	}
	if _, err := mapCustomerHeader([]string{"jenis"}); err == nil {
		t.Error("header tanpa 'nama' harus ditolak")
	}
	// BOM UTF-8 pada kolom pertama.
	idx2, err := mapCustomerHeader([]string{"\ufeffnama", "jenis"})
	if err != nil || idx2["nama"] != 0 {
		t.Errorf("BOM tidak ditangani: %v %v", idx2, err)
	}
}

// TestCustomerDescription memastikan deskripsi terbentuk dari jenis+kapasitas.
func TestCustomerDescription(t *testing.T) {
	cases := []struct{ jenis, cap, want string }{
		{"corporate", "100 Mbps", "Corporate · 100 Mbps"},
		{"personal", "", "Personal"},
		{"reseller", "50", "Reseller · 50"},
		{"dedicated", "1:1", "Dedicated · 1:1"},
	}
	for _, c := range cases {
		if got := customerDescription(c.jenis, c.cap); got != c.want {
			t.Errorf("customerDescription(%q,%q) = %q, want %q", c.jenis, c.cap, got, c.want)
		}
	}
}

// TestSheetNameForJenis memastikan nama sheet valid.
func TestSheetNameForJenis(t *testing.T) {
	if got := sheetNameForJenis("corporate"); got != "Corporate" {
		t.Errorf("sheetNameForJenis(corporate) = %q", got)
	}
	if got := sheetNameForJenis("aneh"); got != "Customer" {
		t.Errorf("fallback sheet = %q", got)
	}
	if n := sheetNameForJenis(strings.Repeat("x", 40)); len(n) > 31 {
		t.Errorf("nama sheet melebihi 31 karakter: %q", n)
	}
}
