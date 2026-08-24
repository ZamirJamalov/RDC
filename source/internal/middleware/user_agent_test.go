package middleware

import "testing"

// PR #292: User-Agent parser testləri — real UA string-ləri ilə.
func TestParseUserAgent(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want ClientInfo
	}{
		{
			name: "Windows Chrome",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
			want: ClientInfo{OS: "Windows", Browser: "Chrome", Device: "desktop"},
		},
		{
			name: "Windows Edge",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0",
			want: ClientInfo{OS: "Windows", Browser: "Edge", Device: "desktop"},
		},
		{
			name: "macOS Safari",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
			want: ClientInfo{OS: "macOS", Browser: "Safari", Device: "desktop"},
		},
		{
			name: "iPhone Safari (UA-da 'like Mac OS X' keçir)",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
			want: ClientInfo{OS: "iOS", Browser: "Safari", Device: "mobile"},
		},
		{
			name: "Android Chrome",
			ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
			want: ClientInfo{OS: "Android", Browser: "Chrome", Device: "mobile"},
		},
		{
			name: "iPad Safari",
			ua:   "Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/604.1",
			want: ClientInfo{OS: "iPadOS", Browser: "Safari", Device: "tablet"},
		},
		{
			name: "Linux Firefox",
			ua:   "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0",
			want: ClientInfo{OS: "Linux", Browser: "Firefox", Device: "desktop"},
		},
		{
			name: "Samsung Internet (UA-da Chrome/Safari də var)",
			ua:   "Mozilla/5.0 (Linux; Android 13; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36",
			want: ClientInfo{OS: "Android", Browser: "Samsung Internet", Device: "mobile"},
		},
		{
			name: "Googlebot",
			ua:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			want: ClientInfo{OS: "Other", Browser: "Other", Device: "bot"},
		},
		{
			name: "curl",
			ua:   "curl/8.5.0",
			want: ClientInfo{OS: "Other", Browser: "Other", Device: "cli"},
		},
		{
			name: "Go HTTP client",
			ua:   "Go-http-client/2.0",
			want: ClientInfo{OS: "Other", Browser: "Other", Device: "cli"},
		},
		{
			name: "boş UA",
			ua:   "",
			want: ClientInfo{OS: "Other", Browser: "Other", Device: "desktop"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseUserAgent(tc.ua)
			if got != tc.want {
				t.Errorf("ParseUserAgent(%q)\n= %+v\nwant %+v", tc.ua, got, tc.want)
			}
		})
	}
}
