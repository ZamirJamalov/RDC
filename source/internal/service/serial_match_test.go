package service

import "testing"

// PR #486: serialMatches — AZMK cavabındaki DocumentSeriaNumber ile daxil
// edilən seriya müqayisəsi (normalizasiya: case, boşluq, defis).
func TestSerialMatches(t *testing.T) {
	cases := []struct {
		name     string
		azmk     string
		entered  string
		expected bool
	}{
		{"exact match", "AZE1234567", "AZE1234567", true},
		{"case insensitive", "aze1234567", "AZE1234567", true},
		{"mixed case", "Aze1234567", "aZE1234567", true},
		{"azmk has space", "AZE 1234567", "AZE1234567", true},
		{"entered has space", "AZE1234567", "AZE 1234567", true},
		{"azmk has dash", "AZE-1234567", "AZE1234567", true},
		{"different digits", "AZE1234567", "AZE7654321", false},
		{"different prefix", "AZE1234567", "AA1234567", false},
		{"completely different", "AZE1234567", "XYZ9999999", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serialMatches(tc.azmk, tc.entered); got != tc.expected {
				t.Errorf("serialMatches(%q, %q) = %v, want %v", tc.azmk, tc.entered, got, tc.expected)
			}
		})
	}
}
