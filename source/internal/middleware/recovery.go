package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery catches any panic that occurs while handling a request, logs the
// stack trace with the request ID (if available), and returns a 500 to the
// client. Without this middleware, a single panic crashes the whole goroutine
// (and on some Go versions, leaves the connection in a broken state).
//
// The middleware never silences the panic — it logs the full stack trace so
// the operator can diagnose the root cause from logs alone.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"error", rec,
						"request_id", FromContext(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
