package crypto

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef" // tepat 32 byte

func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		plaintext string
	}{
		{"api key biasa", "waha-api-key-1234567890"},
		{"unicode", "kunci-rahasia-üñîçødé-🔐"},
		{"panjang", strings.Repeat("a", 4096)},
		{"satu karakter", "x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := Encrypt(testKey, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt error: %v", err)
			}
			if enc == tc.plaintext {
				t.Fatal("ciphertext sama dengan plaintext")
			}
			if _, err := base64.StdEncoding.DecodeString(enc); err != nil {
				t.Fatalf("hasil bukan base64 valid: %v", err)
			}
			dec, err := Decrypt(testKey, enc)
			if err != nil {
				t.Fatalf("Decrypt error: %v", err)
			}
			if dec != tc.plaintext {
				t.Fatalf("round-trip gagal: got %q want %q", dec, tc.plaintext)
			}
		})
	}
}

func TestEncryptEmptyIsNoOp(t *testing.T) {
	enc, err := Encrypt(testKey, "")
	if err != nil {
		t.Fatalf("Encrypt kosong error: %v", err)
	}
	if enc != "" {
		t.Fatalf("Encrypt kosong harus menghasilkan string kosong, got %q", enc)
	}
	dec, err := Decrypt(testKey, "")
	if err != nil {
		t.Fatalf("Decrypt kosong error: %v", err)
	}
	if dec != "" {
		t.Fatalf("Decrypt kosong harus menghasilkan string kosong, got %q", dec)
	}
}

func TestEncryptNonceIsUnique(t *testing.T) {
	const pt = "nilai yang sama"
	a, err := Encrypt(testKey, pt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(testKey, pt)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("dua enkripsi plaintext sama menghasilkan ciphertext identik (nonce tidak acak)")
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	for _, k := range []string{"", "pendek", strings.Repeat("x", 31), strings.Repeat("x", 33)} {
		if _, err := Encrypt(k, "data"); err == nil {
			t.Fatalf("kunci %d byte seharusnya ditolak", len(k))
		}
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	enc, err := Encrypt(testKey, "rahasia")
	if err != nil {
		t.Fatal(err)
	}
	otherKey := "abcdefabcdefabcdefabcdefabcdefab" // 32 byte, berbeda
	if _, err := Decrypt(otherKey, enc); err == nil {
		t.Fatal("dekripsi dengan kunci salah seharusnya gagal")
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	enc, err := Encrypt(testKey, "rahasia penting")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff // rusak 1 byte terakhir
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := Decrypt(testKey, tampered); err == nil {
		t.Fatal("ciphertext yang dirusak seharusnya gagal didekripsi (integritas GCM)")
	}
}

func TestDecryptTooShort(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	if _, err := Decrypt(testKey, short); err == nil {
		t.Fatal("ciphertext terlalu pendek seharusnya error")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	if _, err := Decrypt(testKey, "bukan-base64!!!"); err == nil {
		t.Fatal("base64 tidak valid seharusnya error")
	}
}

func TestGenerateHexKey(t *testing.T) {
	k1, err := GenerateHexKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := GenerateHexKey()
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("dua kunci yang di-generate tidak boleh sama")
	}
	if len(k1) != 64 {
		t.Fatalf("hex key harus 64 karakter, got %d", len(k1))
	}
	if _, err := hex.DecodeString(k1); err != nil {
		t.Fatalf("hex key tidak valid: %v", err)
	}
}

func TestValidKey(t *testing.T) {
	if !ValidKey(testKey) {
		t.Fatal("kunci 32 byte harus valid")
	}
	if ValidKey("") || ValidKey(strings.Repeat("x", 31)) || ValidKey(strings.Repeat("x", 33)) {
		t.Fatal("kunci selain 32 byte harus tidak valid")
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"abc":                  "****",
		"abcdefgh":             "ab****",
		"waha-api-key-1234567": "waha****4567",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
