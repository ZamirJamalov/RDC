// Package extlog — xarici servislərə göndərilən sorğu/cavabların Loki-yə
// yazılması (PR #304).
//
// Zəncir: extlog.Call → slog (JSON, stdout) → app.log → Promtail → Loki → Grafana.
// Loki-də tapmaq:  {job="go-app"} | json | service="azmk"
//
//	{job="go-app"} | json | msg="external_call"
//
// Təhlükəsizlik:
//   - parol/api_key/token dəyərləri avtomatik maskalanır (URL-də və body-də)
//   - body-lər 4000 simvola kimi kəsilir (Loki sətirləri şişməsin)
//   - EXTERNAL_REQRESP_LOG=false ilə söndürülür (default: açıq)
package extlog

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// maxBody caps request/response body length in the log line.
const maxBody = 4000

// maskRe matches key=value / "key":"value" pairs for secrets in URLs and JSON.
var maskRe = regexp.MustCompile(`(?i)("?(api_?key|password|passwd|token|secret|client_secret)"?[=:]\s*"?)[^",;&\s}]+`)

var enabled atomic.Bool

func init() {
	// default: açıq; yalnız açıq şəkildə "false" yazılırsa sönr
	disabled := strings.EqualFold(strings.TrimSpace(os.Getenv("EXTERNAL_REQRESP_LOG")), "false")
	enabled.Store(!disabled)
}

// SetEnabled allows tests to toggle logging.
func SetEnabled(v bool) { enabled.Store(v) }

// mask hides secret values in URLs and bodies (PR #304).
func mask(s string) string {
	return maskRe.ReplaceAllString(s, `${1}***`)
}

// truncate caps the string at maxBody bytes, fixing a partial UTF-8 rune.
func truncate(s string) string {
	if len(s) <= maxBody {
		return s
	}
	cut := s[:maxBody]
	for i := 0; i < 4 && len(cut) > 0; i++ {
		if utf8.ValidString(cut) {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut + "...(truncated)"
}

// Call logs one external service request/response to Loki.
//
// service: "azmk" (lending + customer data), "lw", "otp", "mygov", "sima", "video"
// op:      əməliyyat adı (məs. "AZMK_APPLICATION_CREATE", "send_sms")
// errMsg:  boş deyilsə sətir ERROR səviyyəsində loglanır.
func Call(service, op, method, url, reqBody string, statusCode int, respBody string, durationMs int, errMsg string) {
	if !enabled.Load() {
		return
	}
	attrs := []any{
		"service", service,
		"op", op,
		"method", method,
		"url", truncate(mask(url)),
		"req_body", truncate(mask(reqBody)),
		"status", statusCode,
		"resp_body", truncate(mask(respBody)),
		"duration_ms", durationMs,
	}
	if errMsg != "" {
		attrs = append(attrs, "error", errMsg)
		slog.Error("external_call", attrs...)
		return
	}
	slog.Info("external_call", attrs...)
}
