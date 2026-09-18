package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"ingatin/backend/internal/auth"
	"ingatin/backend/internal/models"
)

type ctxKey string

const (
	ctxUserKey   ctxKey = "ingatin.user"
	ctxClaimsKey ctxKey = "ingatin.claims"
)

// requireAuth memverifikasi Bearer token dan menyimpan user di context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "header Authorization tidak ada atau tidak valid")
			return
		}

		user, err := s.auth.ParseAccessToken(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				writeErr(w, http.StatusUnauthorized, "sesi kedaluwarsa, silakan login ulang")
			case errors.Is(err, auth.ErrTokenRevoked):
				writeErr(w, http.StatusUnauthorized, "sesi sudah dicabut, silakan login ulang")
			case errors.Is(err, auth.ErrUserInactive):
				writeErr(w, http.StatusForbidden, "akun tidak aktif")
			default:
				writeErr(w, http.StatusUnauthorized, "token tidak valid")
			}
			return
		}

		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole memastikan user memiliki salah satu role yang diizinkan.
func (s *Server) requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := userFrom(r)
			if user == nil {
				writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
				return
			}
			if !allowed[user.Role] {
				writeErr(w, http.StatusForbidden, "role anda tidak memiliki akses ke aksi ini")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAdmin adalah jalan pintas untuk role admin.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireRole(models.RoleAdmin)(next)
}

// requirePermission memeriksa matriks izin (role_permissions) untuk sebuah
// action. Admin selalu lolos (dijaga di Store.PermissionAllowed) sehingga
// tidak mungkin mengunci diri sendiri dari panel.
func (s *Server) requirePermission(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := userFrom(r)
			if user == nil {
				writeErr(w, http.StatusUnauthorized, "belum terautentikasi")
				return
			}
			if !s.store.PermissionAllowed(r.Context(), user.Role, action) {
				writeErr(w, http.StatusForbidden, "role anda tidak memiliki izin "+action)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userFrom mengambil user terautentikasi dari context.
func userFrom(r *http.Request) *models.User {
	if v, ok := r.Context().Value(ctxUserKey).(*models.User); ok {
		return v
	}
	return nil
}

// bearerToken mengekstrak token dari header Authorization.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// clientIP mengembalikan alamat klien.
//
// Chi middleware.RealIP dipasang, tetapi hanya header yang berasal dari proxy
// terpercaya yang seharusnya dipercaya; untuk catatan audit, alamat koneksi
// langsung sudah memadai dan lebih sulit dipalsukan.
func (s *Server) clientIP(r *http.Request) string {
	if len(s.cfg.TrustedProxies) > 0 {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first := strings.TrimSpace(strings.Split(fwd, ",")[0])
			if s.isTrustedProxy(r.RemoteAddr) && net.ParseIP(first) != nil {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) isTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, raw := range s.cfg.TrustedProxies {
		if _, cidr, err := net.ParseCIDR(raw); err == nil {
			if cidr.Contains(ip) {
				return true
			}
			continue
		}
		if parsed := net.ParseIP(raw); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}
