package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
)

func testConfig() *config.Config {
	return &config.Config{
		AppName:            "Ingat.in",
		SecretKey:          strings.Repeat("k", 64),
		AccessTokenMinutes: 4320, // 72 jam
		RefreshTokenDays:   30,
	}
}

func testManager() *Manager {
	return New(testConfig(), nil) // store tidak dipakai oleh signAccessToken
}

func TestAccessTokenLifetimeIs72Hours(t *testing.T) {
	cfg := testConfig()
	m := New(cfg, nil)

	user := &models.User{
		Username:     "noc1",
		Role:         models.RoleNOC,
		TokenVersion: 0,
	}

	now := time.Now().UTC()
	expires := now.Add(time.Duration(cfg.AccessTokenMinutes) * time.Minute)

	tokenStr, err := m.signAccessToken(user, now, expires)
	if err != nil {
		t.Fatalf("signAccessToken: %v", err)
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return []byte(cfg.SecretKey), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		t.Fatal("klaim tidak sesuai")
	}

	if claims.Subject != "noc1" {
		t.Errorf("sub = %q, ingin noc1", claims.Subject)
	}
	if claims.Role != models.RoleNOC {
		t.Errorf("role = %q, ingin noc", claims.Role)
	}
	if claims.TokenVersion != 0 {
		t.Errorf("ver = %d, ingin 0", claims.TokenVersion)
	}
	if claims.ID == "" {
		t.Error("jti kosong")
	}

	// Umur token harus 72 jam (dengan toleransi beberapa detik).
	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	expected := 72 * time.Hour
	if diff := lifetime - expected; diff > time.Minute || diff < -time.Minute {
		t.Errorf("umur token = %s, ingin %s", lifetime, expected)
	}
}

func TestAccessTokenRejectsWrongSecret(t *testing.T) {
	m := testManager()
	now := time.Now().UTC()
	tokenStr, err := m.signAccessToken(&models.User{Username: "x", Role: "viewer"}, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	_, err = jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return []byte("kunci-lain-yang-tidak-cocok"), nil
	})
	if err == nil {
		t.Fatal("token dengan kunci salah seharusnya gagal diverifikasi")
	}
}

func TestAccessTokenRejectsWrongAlgorithm(t *testing.T) {
	cfg := testConfig()

	// Token dengan alg "none" tidak boleh diterima.
	claims := Claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "penyerang",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenStr, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	// ParseAccessToken memerlukan store; kita uji penolakan tanda tangan lewat jwt.Parse.
	_, err = jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(cfg.SecretKey), nil
	})
	if err == nil {
		t.Fatal("token dengan alg=none seharusnya ditolak")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	m := testManager()
	now := time.Now().UTC().Add(-100 * time.Hour)
	tokenStr, err := m.signAccessToken(
		&models.User{Username: "x", Role: "viewer"},
		now,
		now.Add(72*time.Hour), // kedaluwarsa 28 jam lalu
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return []byte(testConfig().SecretKey), nil
	})
	if err == nil {
		t.Fatal("token kedaluwarsa seharusnya ditolak")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error seharusnya menandakan kedaluwarsa, got: %v", err)
	}
}

func TestGenerateRefreshTokenIsRandomAndUrlSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		tok, err := generateRefreshToken()
		if err != nil {
			t.Fatalf("generateRefreshToken: %v", err)
		}
		if len(tok) < 40 {
			t.Fatalf("token terlalu pendek: %d karakter", len(tok))
		}
		if strings.ContainsAny(tok, "+/=") {
			t.Errorf("token memuat karakter tidak URL-safe: %q", tok)
		}
		if seen[tok] {
			t.Fatal("terjadi duplikasi refresh token")
		}
		seen[tok] = true
	}
}

func TestRefreshTokenHashIsStableAndDistinct(t *testing.T) {
	tok, err := generateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	h1 := hashForTest(tok)
	h2 := hashForTest(tok)
	if h1 != h2 {
		t.Error("hash token harus stabil")
	}
	if h1 == tok {
		t.Error("hash tidak boleh sama dengan token asli")
	}

	other, _ := generateRefreshToken()
	if hashForTest(other) == h1 {
		t.Error("token berbeda harus menghasilkan hash berbeda")
	}
}

// hashForTest memakai fungsi hash yang sama dengan produksi (crypto.HashToken).
func hashForTest(token string) string {
	return crypto.HashToken(token)
}
