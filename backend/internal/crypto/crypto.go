// Package crypto menyediakan enkripsi simetris untuk menyimpan kredensial
// (API key provider WA, token, dst.) dalam keadaan terenkripsi di database.
//
// Algoritma: AES-256-GCM. Nonce di-prepend ke ciphertext, seluruh blob
// di-encode base64 StdEncoding. Kunci adalah string 32 byte mentah.
//
// Implementasi diadopsi dari mcnvpn/internal/crypto/crypto.go agar konsisten
// di seluruh proyek.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKey dikembalikan bila panjang kunci bukan 32 byte.
var ErrInvalidKey = errors.New("kunci enkripsi harus tepat 32 byte")

// ErrCiphertextTooShort dikembalikan bila data yang akan didekripsi lebih pendek
// dari ukuran nonce.
var ErrCiphertextTooShort = errors.New("ciphertext terlalu pendek")

// GenerateKey membuat kunci acak 32 byte yang di-encode base64 (44 karakter)
// atau hex (64 karakter). Nilai yang dikembalikan aman dipakai sebagai
// INGATIN_CREDENTIAL_KEY hanya bila panjangnya 32 byte; helper ini
// mengembalikan string base64 mentah agar pemanggil dapat memilih encoding.
//
// Untuk kebutuhan .env, gunakan GenerateHexKey (64 karakter hex = 32 byte).
func GenerateKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return string(raw), nil
}

// GenerateHexKey membuat kunci 32 byte dan mengembalikannya sebagai 64 karakter
// hex. Cocok untuk disimpan di .env.
func GenerateHexKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range raw {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out), nil
}

// ValidKey melaporkan apakah kunci berukuran tepat 32 byte.
func ValidKey(key string) bool { return len(key) == 32 }

// Encrypt mengenkripsi plaintext dengan AES-256-GCM memakai kunci 32 byte.
// Jika plaintext kosong, hasilnya adalah string kosong tanpa error (no-op),
// sehingga kolom opsional tidak menyimpan ciphertext palsu.
func Encrypt(key, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt membalikkan Encrypt. String kosong dikembalikan sebagai kosong tanpa error.
func Decrypt(key, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", ErrCiphertextTooShort
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("dekripsi gagal: %w", err)
	}
	return string(pt), nil
}

// Mask menyembunyikan bagian tengah secret untuk ditampilkan di UI/log.
// Contoh: "abcdefghijkl" -> "abcd...ijkl".
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	if len(secret) <= 8 {
		return secret[:2] + "****"
	}
	return secret[:4] + "****" + secret[len(secret)-4:]
}
