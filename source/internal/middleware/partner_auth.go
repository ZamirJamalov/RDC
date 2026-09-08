package middleware

import (
	"crypto/subtle"
	"net/http"
)

// RequirePartnerAPIKey — PR #427: LW (partner) server-to-server çağırışları üçün
// API açarı qapısı. Açar X-API-Key header-də gözlənilir və constant-time
// müqayisə ilə yoxlanılır (timing attack qorunması).
//
// PARTNER_VIDEO_API_KEY env-i boşdursa endpoint 404 qaytarır — servis "yox kimi"
// görünür (xarici skanerlər üçün mövcudluğu açıqlanmır).
func RequirePartnerAPIKey(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				writeAuthError(w, http.StatusNotFound, "not found")
				return
			}
			provided := r.Header.Get("X-API-Key")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
				writeAuthError(w, http.StatusUnauthorized, "yanlış API açarı")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
