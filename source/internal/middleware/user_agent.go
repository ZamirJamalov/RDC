package middleware

import "strings"

// ClientInfo holds the parsed User-Agent fields logged with each request.
// PR #292: Loki/Grafana-da OS və brauzer üzrə filtrasiya üçün raw user_agent
// string-i ilə yanaşı parse olunmuş sahələr də loglanır.
type ClientInfo struct {
	OS      string // Windows, macOS, Linux, Android, iOS, iPadOS, ChromeOS, Other
	Browser string // Chrome, Firefox, Safari, Edge, Opera, Samsung Internet, Other
	Device  string // desktop, mobile, tablet, bot, cli
}

// ParseUserAgent extracts os / browser / device from a User-Agent string.
// Heuristik parser (xarici asılılıq yoxdur) — ən yaygın klientləri əhatə edir.
// Sıralama vacibdir: məs. Chrome UA-da "Safari/" də olur, iPhone UA-da
// "like Mac OS X" — ona görə xüsusi hallar ümumilərdən əvvəl yoxlanılır.
func ParseUserAgent(ua string) ClientInfo {
	s := strings.ToLower(ua)
	info := ClientInfo{OS: "Other", Browser: "Other", Device: "desktop"}

	// --- Device: bot / cli / tablet / mobile / desktop ---
	switch {
	case containsAny(s, "bot", "crawler", "spider", "slurp", "bingpreview", "headless"):
		info.Device = "bot"
	case containsAny(s, "curl/", "wget", "postmanruntime", "python-requests", "go-http-client", "okhttp", "insomnia"):
		info.Device = "cli"
	case strings.Contains(s, "ipad"):
		info.Device = "tablet"
	case containsAny(s, "mobi", "iphone", "ipod", "android"):
		info.Device = "mobile"
	}

	// --- OS (iPhone/iPad UA-da "Mac OS X" keçdiyi üçün əvvəl yoxlanılır) ---
	switch {
	case strings.Contains(s, "windows phone"):
		info.OS = "Windows Phone"
	case strings.Contains(s, "windows"):
		info.OS = "Windows"
	case strings.Contains(s, "iphone"):
		info.OS = "iOS"
	case strings.Contains(s, "ipad"):
		info.OS = "iPadOS"
	case strings.Contains(s, "android"):
		info.OS = "Android"
	case containsAny(s, "macintosh", "mac os x"):
		info.OS = "macOS"
	case strings.Contains(s, "cros"):
		info.OS = "ChromeOS"
	case containsAny(s, "linux", "x11"):
		info.OS = "Linux"
	}

	// --- Browser (Edge/Opera/Samsung UA-larında "Chrome/" və "Safari/" olur —
	// ona görə xüsusi brauzerlər Chrome/Safari-dən əvvəl yoxlanılır) ---
	switch {
	case containsAny(s, "edg/", "edge/", "edga/", "edgios/"):
		info.Browser = "Edge"
	case containsAny(s, "opr/", "opera"):
		info.Browser = "Opera"
	case strings.Contains(s, "samsungbrowser"):
		info.Browser = "Samsung Internet"
	case containsAny(s, "firefox/", "fxios"):
		info.Browser = "Firefox"
	case containsAny(s, "chrome/", "crios"):
		info.Browser = "Chrome"
	case strings.Contains(s, "safari/"):
		info.Browser = "Safari"
	}

	return info
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
