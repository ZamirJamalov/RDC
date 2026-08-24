package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PR #292: Logger middleware testləri — HTML səhifə keçidləri, asset səviyyəsi,
// IP çıxarışı, panic loglaması.

func newBufferLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level}))
}

func TestLogger_PageRequestFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf, slog.LevelInfo)
	h := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("X-Forwarded-For", "85.132.55.12, 10.0.0.1")
	req.RemoteAddr = "192.168.1.5:54321"
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	// PR #292-nin əsas tələbi: hansı səhifə, hansı IP/OS, nə vaxt (slog time)
	for _, want := range []string{
		`"msg":"request_completed"`,
		`"level":"INFO"`,
		`"method":"GET"`,
		`"path":"/dashboard"`,
		`"status":200`,
		`"type":"page"`,
		`"os":"Windows"`,
		`"browser":"Chrome"`,
		`"device":"desktop"`,
		`"ip":"85.132.55.12"`, // XFF ilk dəyər
		`"remote_addr":"192.168.1.5:54321"`,
		`"request_id":"`, // RequestID middleware varsız da boş gelmir — burada yalnız açar mövcudluğu yoxlanılır
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %s\noutput: %s", want, out)
		}
	}
}

func TestLogger_IPWithoutXFF(t *testing.T) {
	var buf bytes.Buffer
	h := Logger(newBufferLogger(&buf, slog.LevelInfo))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/apply", nil)
	req.RemoteAddr = "10.20.30.40:9999"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(buf.String(), `"ip":"10.20.30.40"`) {
		t.Errorf("expected ip=10.20.30.40 (RemoteAddr host, portsuz), got: %s", buf.String())
	}
}

func TestLogger_AssetDebugOnly(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// INFO səviyyəsində asset loglanMAMALIdır
	var infoBuf bytes.Buffer
	h := Logger(newBufferLogger(&infoBuf, slog.LevelInfo))(handler)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if infoBuf.Len() != 0 {
		t.Errorf("asset must not be logged at INFO level, got: %s", infoBuf.String())
	}

	// DEBUG səviyyəsində asset loglanmalıdır
	var debugBuf bytes.Buffer
	h = Logger(newBufferLogger(&debugBuf, slog.LevelDebug))(handler)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/style.css", nil))
	if !strings.Contains(debugBuf.String(), `"type":"asset"`) {
		t.Errorf("asset must be logged at DEBUG level, got: %s", debugBuf.String())
	}
}

func TestLogger_5xxWarnLevel(t *testing.T) {
	var buf bytes.Buffer
	h := Logger(newBufferLogger(&buf, slog.LevelInfo))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/test", nil))

	if !strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Errorf("5xx must be logged at WARN level, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"type":"api"`) {
		t.Errorf("expected type=api, got: %s", buf.String())
	}
}

// PR #292: Logger Recovery-dən KƏNARDA olmalıdır ki, panic 500-ləri də
// request_completed kimi loglansın (main.go-dakı zəncir sırası ilə eyni).
func TestLogger_PanicStillLoggedAsRequest(t *testing.T) {
	var buf bytes.Buffer
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	})
	// main.go zənciri: Logger(Recovery(handler))
	root := Logger(newBufferLogger(&buf, slog.LevelInfo))(Recovery(slog.Default())(panicking))
	root.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/detail", nil))

	out := buf.String()
	if !strings.Contains(out, `"msg":"request_completed"`) {
		t.Errorf("panic request must still produce request_completed log, got: %s", out)
	}
	if !strings.Contains(out, `"status":500`) {
		t.Errorf("panic request must be logged with status 500, got: %s", out)
	}
	if !strings.Contains(out, `"type":"page"`) {
		t.Errorf("expected type=page for /detail, got: %s", out)
	}
}

func TestRequestType(t *testing.T) {
	tests := []struct{ path, want string }{
		{"/", "page"},
		{"/login", "page"},
		{"/dashboard", "page"},
		{"/detail", "page"},
		{"/apply", "page"},
		{"/landing.html", "page"},
		{"/api/applications", "api"},
		{"/api/otp/send", "api"},
		{"/style.css", "asset"},
		{"/app.js", "asset"},
		{"/logo.png", "asset"},
		{"/favicon.ico", "asset"},
		{"/font.woff2", "asset"},
		{"/robots.txt", "other"},
		{"/nonexistent", "other"},
	}
	for _, tc := range tests {
		if got := requestType(tc.path); got != tc.want {
			t.Errorf("requestType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestClientIP(t *testing.T) {
	// XFF çoxlu dəyər — ilk dəyər götürülməlidir
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	r.RemoteAddr = "3.3.3.3:443"
	if got := clientIP(r); got != "1.1.1.1" {
		t.Errorf("clientIP with XFF = %q, want 1.1.1.1", got)
	}

	// XFF yoxdur — RemoteAddr host hissəsi
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.0.1:12345"
	if got := clientIP(r); got != "192.168.0.1" {
		t.Errorf("clientIP without XFF = %q, want 192.168.0.1", got)
	}

	// RemoteAddr portsuz (nadir hal) — olduğu kimi qaytarılmalıdır
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1"
	if got := clientIP(r); got != "10.0.0.1" {
		t.Errorf("clientIP portless = %q, want 10.0.0.1", got)
	}
}
