// Package auth menangani autentikasi: JWT access token (72 jam) dan refresh
// token rotating (30 hari) yang disimpan sebagai hash dan dapat dicabut.
//
// Rancangan (PLAN.md §2 keputusan #6):
//   - Access token JWT HS256 berumur INGATIN_ACCESS_TOKEN_MINUTES (default 4320 = 72 jam)
//     dengan klaim sub, role, ver (token_version), jti, iat, exp
//   - Refresh token acak 32 byte (base64url) yang disimpan sebagai SHA-256 hash
//     di tabel refresh_tokens; rotasi menghasilkan token baru dan mencabut yang lama
//   - users.token_version dinaikkan saat ganti password / revoke semua sesi,
//     sehingga seluruh access token lama langsung tidak valid
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/repository"
)

// Error autentikasi.
var (
	ErrInvalidCredentials = errors.New("username atau password salah")
	ErrUserInactive       = errors.New("akun tidak aktif")
	ErrInvalidToken       = errors.New("token tidak valid")
	ErrTokenExpired       = errors.New("token kedaluwarsa")
	ErrTokenRevoked       = errors.New("sesi sudah dicabut")
	ErrTokenMismatch      = errors.New("token tidak cocok dengan pemiliknya")
)

// Claims adalah klaim JWT access token.
type Claims struct {
	Role         string `json:"role"`
	TokenVersion int    `json:"ver"`
	jwt.RegisteredClaims
}

// Session adalah hasil login/refresh yang dikembalikan ke klien.
type Session struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresAt    time.Time    `json:"expires_at"`
	ExpiresIn    int          `json:"expires_in"`
	User         *models.User `json:"user,omitempty"`
}

// Manager menangani pembuatan dan verifikasi token.
type Manager struct {
	cfg   *config.Config
	store *repository.Store
}

// New membuat Manager.
func New(cfg *config.Config, store *repository.Store) *Manager {
	return &Manager{cfg: cfg, store: store}
}

// Login memverifikasi kredensial lalu membuat sesi baru.
func (m *Manager) Login(ctx context.Context, username, password, userAgent, ip string) (*Session, error) {
	user, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Jangan bocorkan apakah username ada atau tidak.
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !crypto.VerifyPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}
	if !user.IsActive {
		return nil, ErrUserInactive
	}
	return m.issueSession(ctx, user, userAgent, ip, nil)
}

// Refresh menukar refresh token lama dengan sesi baru (rotasi).
//
// Token lama langsung dicabut; token baru mencatat asal rotasinya sehingga
// penyalahgunaan token lama dapat ditelusuri.
func (m *Manager) Refresh(ctx context.Context, refreshToken, userAgent, ip string) (*Session, error) {
	hash := crypto.HashToken(refreshToken)

	stored, err := m.store.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if stored.RevokedAt != nil {
		// Token yang sudah dipakai lalu dipakai lagi menandakan kemungkinan
		// pencurian token. Cabut seluruh sesi user sebagai tindakan pengamanan.
		_ = m.store.RevokeAllUserTokens(ctx, stored.UserID)
		_ = m.store.BumpTokenVersion(ctx, stored.UserID)
		return nil, ErrTokenRevoked
	}
	if time.Now().UTC().After(stored.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	user, err := m.store.GetUserByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if !user.IsActive {
		return nil, ErrUserInactive
	}

	return m.issueSession(ctx, user, userAgent, ip, &stored.ID)
}

// issueSession membuat access token + refresh token baru.
//
// rotatedFrom tidak nil berarti sesi ini hasil rotasi: token lama dicabut di
// dalam transaksi yang sama agar tidak ada celah token ganda.
func (m *Manager) issueSession(ctx context.Context, user *models.User, userAgent, ip string, rotatedFrom *uuid.UUID) (*Session, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(m.cfg.AccessTokenMinutes) * time.Minute)

	accessToken, err := m.signAccessToken(user, now, expiresAt)
	if err != nil {
		return nil, err
	}

	rawRefresh, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}
	refreshExpiry := now.Add(time.Duration(m.cfg.RefreshTokenDays) * 24 * time.Hour)

	err = m.store.Tx(ctx, func(tx pgx.Tx) error {
		if rotatedFrom != nil {
			if err := m.store.RevokeRefreshTokenTx(ctx, tx, *rotatedFrom); err != nil {
				return err
			}
		}
		return m.store.CreateRefreshTokenTx(ctx, tx, repository.CreateRefreshTokenParams{
			UserID:      user.ID,
			TokenHash:   crypto.HashToken(rawRefresh),
			UserAgent:   truncate(userAgent, 400),
			IP:          truncate(ip, 64),
			ExpiresAt:   refreshExpiry,
			RotatedFrom: rotatedFrom,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("simpan refresh token: %w", err)
	}

	_ = m.store.TouchLastLogin(ctx, user.ID)

	// Hash password tidak boleh keluar ke klien.
	user.PasswordHash = ""

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt,
		ExpiresIn:    m.cfg.AccessTokenMinutes * 60,
		User:         user,
	}, nil
}

// signAccessToken membuat JWT beserta klaim role dan token_version.
func (m *Manager) signAccessToken(user *models.User, now, expiresAt time.Time) (string, error) {
	claims := Claims{
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Username,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    m.cfg.AppName,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.cfg.SecretKey))
}

// ParseAccessToken memverifikasi tanda tangan dan masa berlaku access token,
// lalu memastikan user masih ada, aktif, dan token_version-nya cocok.
//
// Pemeriksaan token_version inilah yang membuat "revoke semua sesi" bekerja
// walau access token belum kedaluwarsa.
func (m *Manager) ParseAccessToken(ctx context.Context, tokenString string) (*models.User, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("metode tanda tangan tidak didukung: %v", t.Header["alg"])
		}
		return []byte(m.cfg.SecretKey), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}

	user, err := m.store.GetUserByUsername(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if !user.IsActive {
		return nil, ErrUserInactive
	}
	if user.TokenVersion != claims.TokenVersion {
		// Password diganti atau sesi dicabut setelah token diterbitkan.
		return nil, ErrTokenRevoked
	}

	user.PasswordHash = ""
	return user, nil
}

// Logout mencabut satu refresh token.
func (m *Manager) Logout(ctx context.Context, refreshToken string) error {
	hash := crypto.HashToken(refreshToken)
	stored, err := m.store.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // sudah tidak ada = sudah logout
		}
		return err
	}
	return m.store.RevokeRefreshToken(ctx, stored.ID)
}

// LogoutAll mencabut seluruh sesi user dan menaikkan token_version.
func (m *Manager) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := m.store.RevokeAllUserTokens(ctx, userID); err != nil {
		return err
	}
	return m.store.BumpTokenVersion(ctx, userID)
}

// ChangePassword mengganti password, lalu mencabut seluruh sesi lain.
//
// token_version dinaikkan sehingga semua access token yang beredar langsung
// tidak valid — termasuk token milik sesi yang sedang memanggil.
func (m *Manager) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	user, err := m.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !crypto.VerifyPassword(currentPassword, user.PasswordHash) {
		return ErrInvalidCredentials
	}
	if len(newPassword) < 8 {
		return errors.New("password baru minimal 8 karakter")
	}
	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := m.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return err
	}
	return m.LogoutAll(ctx, userID)
}

// ActiveSessions mengembalikan sesi aktif milik user.
func (m *Manager) ActiveSessions(ctx context.Context, userID uuid.UUID) ([]models.RefreshToken, error) {
	return m.store.ListActiveRefreshTokens(ctx, userID)
}

// generateRefreshToken membuat token acak 32 byte (base64url, tanpa padding).
func generateRefreshToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
