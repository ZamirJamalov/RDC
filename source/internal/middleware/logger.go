package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
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
			// PR #263: Loki üçün request context məlumatları əlavə olundu.
			// user_agent → OS, browser, device info (məs: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			// forwarded_for → real IP (proxy/LB arxasında)
			// referer → hansı səhifədən gəlib
			// content_length → request body ölçüsü
			//
			// PR #292: HTML səhifə keçidlərinin Loki-də görünməsi üçün:
			//   type   → page / api / asset / other (LogQL filtrasiyası asan olsun)
			//   ip     → client IP (XFF varsa ilk dəyər, yoxsa RemoteAddr host)
			//   os / browser / device → UserAgent-dən parse (user_agent.go)
			client := ParseUserAgent(r.UserAgent())
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", rec.bytes,
				"request_id", FromContext(r.Context()),
				"remote_addr", r.RemoteAddr,
				"ip", clientIP(r),
				"user_agent", r.UserAgent(),
				"os", client.OS,
				"browser", client.Browser,
				"device", client.Device,
				"type", requestType(r.URL.Path),
				"forwarded_for", r.Header.Get("X-Forwarded-For"),
				"referer", r.Referer(),
				"content_length", r.ContentLength,
				"scheme", schemeFromRequest(r),
				"host", r.Host,
			}

			// PR #292: asset-lər (css/js/png və s.) DEBUG səviyyəsində — default
			// LOG_LEVEL=info ilə terminal/Loki çirklənmir, debug-a keçəndə görünür.
			// 5xx → WARN (PR #263 qaydası qalır).
			switch {
			case requestType(r.URL.Path) == "asset":
				logger.Debug("request_completed", attrs...)
			case rec.status >= 500:
				logger.Warn("request_completed", attrs...)
			default:
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

// clientIP returns the best-guess client IP for logging.
// PR #292: X-Forwarded-For varsa ilk dəyər (origin client), yoxsa RemoteAddr-ın
// host hissəsi (port silinir). NOTE: XFF klient tərəfindən spoof ola bilər —
// yalnız log üçün istifadə olunur, təhlükəsizlik qərarları üçün YOX.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// requestType classifies a request path for easy LogQL filtering.
// PR #292: page (HTML keçidləri — istifadəçi naviqasiyası), api (/api/...),
// asset (css/js/png və s.), other (404, robots.txt və s.).
// NOTE: clean URL siyahısı main.go-dakı cleanURLMap ilə sinxron saxlanmalıdır.
func requestType(path string) string {
	if strings.HasPrefix(path, "/api/") {
		return "api"
	}
	if strings.HasSuffix(path, ".html") {
		return "page"
	}
	switch path {
	case "/", "/login", "/dashboard", "/admin", "/detail", "/apply", "/landing":
		return "page"
	}
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		switch strings.ToLower(path[i:]) {
		case ".css", ".js", ".mjs", ".png", ".jpg", ".jpeg", ".gif", ".svg",
			".ico", ".webp", ".woff", ".woff2", ".ttf", ".eot", ".otf", ".map":
			return "asset"
		}
	}
	return "other"
}
