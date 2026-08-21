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
//      INFO request_completed method=POST path=/api/applications status=201
//          duration_ms=42 bytes=187 request_id=abc-123
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
			// PR #263: Loki üçün request context məlumatları əlavə olundu.
			// user_agent → OS, browser, device info (məs: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			// forwarded_for → real IP (proxy/LB arxasında)
			// referer → hansı səhifədən gəlib
			// content_length → request body ölçüsü
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", rec.bytes,
				"request_id", FromContext(r.Context()),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"forwarded_for", r.Header.Get("X-Forwarded-For"),
				"referer", r.Referer(),
				"content_length", r.ContentLength,
				"scheme", schemeFromRequest(r),
				"host", r.Host,
			}

			if rec.status >= 500 {
				logger.Warn("request_completed", attrs...)
			} else {
				logger.Info("request_completed", attrs...)
			}
		})
	}
}

// schemeFromRequest returns "https" if the request was made over TLS or
// behind a proxy that set X-Forwarded-Proto, otherwise "http".
// PR #263: Loki loglarına scheme əlavə etmək üçün.
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
