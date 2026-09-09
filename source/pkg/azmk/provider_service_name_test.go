package azmk

import "testing"

// TestAzmkServiceName — PR #431: path-dən törədilən sabit servis adları.
// ID seqmentləri (16+ hex) çıxarılır ki, service_audit_logs / sağlamlıq
// panelində hər ID üçün ayrı "servis" yaranmasın.
func TestAzmkServiceName(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		// Dəyişməyən adlar (ID yoxdur)
		{"/application/create", "AZMK_APPLICATION_CREATE"},
		{"/application/disburse", "AZMK_APPLICATION_DISBURSE"},
		{"/partner", "AZMK_PARTNER"},
		{"/card", "AZMK_CARD"},
		{"/kyc", "AZMK_KYC"},

		// PR #431 əvvəl ID ilə dinamik idilər — indi sabit:
		{"/partner/2F12F4810B7C40F1B95F62001B647259/phones", "AZMK_PARTNER_PHONES"},
		{"/card/2F12F4810B7C40F1B95F62001B647259", "AZMK_CARD"},
		{"/application/1A2B3C4D5E6F7A8B9C0D1E2F3A4B5C6D/status", "AZMK_APPLICATION_STATUS"},
		{"/kyc/DEEAA6B7DE064F2D9F49D66F2D0118A2", "AZMK_KYC"},
	}
	for _, c := range cases {
		if got := azmkServiceName(c.path); got != c.want {
			t.Errorf("azmkServiceName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestIsHexID — ID təyini: qısa seqmentlər adın hissəsi qalır.
func TestIsHexID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2F12F4810B7C40F1B95F62001B647259", true}, // 32 hex UUID
		{"DEEAA6B7DE064F2D9F49D66F2D0118A2", true}, // 32 hex
		{"1a2b3c4d5e6f7a8b", true},                 // 16 hex
		{"create", false},                          // qısa söz
		{"status", false},                          // qısa söz
		{"phones", false},                          // qısa söz
		{"ABC123", false},                          // qısa
		{"123456789012345G", false},                // 16 simvol amma hex deyil (G)
	}
	for _, c := range cases {
		if got := isHexID(c.in); got != c.want {
			t.Errorf("isHexID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
