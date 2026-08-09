package middleware

import (
	"context"
	"net/http"

	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// principalKey is the context key under which the authenticated user is stored.
type principalKey struct{}

// WithPrincipal stores the authenticated user in the request context.
func WithPrincipal(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, principalKey{}, user)
}

// PrincipalFromContext extracts the authenticated user from the context.
// Returns nil if no user is set (unauthenticated request).
func PrincipalFromContext(ctx context.Context) *model.User {
	if v, ok := ctx.Value(principalKey{}).(*model.User); ok {
		return v
	}
	return nil
}

// RequireAuth returns middleware that requires a valid session token.
// The token is read from the rdc_session cookie or Authorization: Bearer header.
// On success, the authenticated user is stored in the request context.
// On failure, a 401 JSON response is written.
func RequireAuth(authSvc *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := service.ExtractToken(r)
			if token == "" {
				writeAuthError(w, http.StatusUnauthorized, "giriş tələb olunur")
				return
			}

			user, err := authSvc.ValidateSession(r.Context(), token)
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "sessiya yoxlanıla bilmədi")
				return
			}
			if user == nil {
				writeAuthError(w, http.StatusUnauthorized, "yanlış və ya vaxtı keçmiş sessiya")
				return
			}

			ctx := WithPrincipal(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns middleware that requires the authenticated user to have
// the "admin" role. Must be used after RequireAuth in the middleware chain.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := PrincipalFromContext(r.Context())
			if user == nil {
				writeAuthError(w, http.StatusUnauthorized, "giriş tələb olunur")
				return
			}
			if user.Role != model.RoleAdmin {
				writeAuthError(w, http.StatusForbidden, "bu əməliyyat üçün admin hüququ tələb olunur")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeAuthError writes a JSON error response for auth failures.
func writeAuthError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(`{"error":"` + message + `"}`))
}
