package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter to capture the status code so the
// logger can report it after the handler returns. Writes to the underlying
// writer are forwarded unchanged.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logger emits one structured log line per HTTP request, including method,
// path, status, duration, response size, and request ID. The log level is
// INFO for 2xx/3xx/4xx, WARN for 5xx.
//
// Example log line:
//
//	INFO request_completed method=POST path=/api/applications status=201
//	    duration_ms=42 bytes=187 request_id=abc-123
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			duration := time.Since(start)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", rec.bytes,
				"request_id", FromContext(r.Context()),
				"remote_addr", r.RemoteAddr,
			}

			if rec.status >= 500 {
				logger.Warn("request_completed", attrs...)
			} else {
				logger.Info("request_completed", attrs...)
			}
		})
	}
}
