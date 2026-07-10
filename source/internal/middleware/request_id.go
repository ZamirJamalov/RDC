package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// requestIDKey is the context key under which the per-request UUID is stored.
type requestIDKey struct{}

// HeaderRequestID is the HTTP header name used to propagate the request ID
// between client and server (and downstream services, when applicable).
const HeaderRequestID = "X-Request-ID"

// RequestID assigns a unique ID to every request. If the incoming request
// already carries an X-Request-ID header (e.g. from a gateway), that value is
// reused; otherwise a fresh UUIDv4 is generated.
//
// The ID is added to the request context (so handlers and services can log it)
// and echoed back in the response header for client-side correlation.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}

		// Stash in context for downstream handlers / loggers
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		// Echo back so the client can correlate logs with their request
		w.Header().Set(HeaderRequestID, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext extracts the request ID stored by RequestID middleware.
// Returns empty string if no request ID is in the context (e.g. when called
// outside an HTTP request — common in background goroutines).
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
