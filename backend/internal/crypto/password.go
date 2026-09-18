package crypto

import (
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword menghasilkan hash bcrypt untuk password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword membandingkan password plaintext dengan hash bcrypt.
func VerifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashToken menghasilkan SHA-256 heksadesimal dari sebuah token.
// Dipakai untuk menyimpan refresh token dalam bentuk hash (bukan plaintext).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
