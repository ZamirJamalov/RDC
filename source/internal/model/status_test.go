package model

import "testing"

// TestIsFinal verifies the IsFinal helper. Terminal states are approved and
// rejected; everything else should return false.
func TestIsFinal(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{StatusApproved, true},
		{StatusRejected, true},
		{StatusPending, false},
		{StatusChecking, false},
		{StatusPendingApproval, false},
		{"", false},
		{"unknown", false},
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			if got := IsFinal(tc.status); got != tc.want {
				t.Errorf("IsFinal(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestIsActive verifies the IsActive helper. Active states are pending,
// checking, and pending_approval; terminal states and unknown values return false.
func TestIsActive(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{StatusPending, true},
		{StatusChecking, true},
		{StatusPendingApproval, true},
		{StatusApproved, false},
		{StatusRejected, false},
		{"", false},
		{"unknown", false},
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			if got := IsActive(tc.status); got != tc.want {
				t.Errorf("IsActive(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestIsValidCreditLevel verifies the IsValidCreditLevel helper. Only the
// four canonical level names are valid.
func TestIsValidCreditLevel(t *testing.T) {
	tests := []struct {
		level string
		want  bool
	}{
		{CreditLevelNew, true},
		{CreditLevelTrusted, true},
		{CreditLevelValuable, true},
		{CreditLevelElite, true},
		{"", false},
		{"platinum", false},
		{"NEW", false}, // case-sensitive
		{"new ", false}, // trailing space
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			if got := IsValidCreditLevel(tc.level); got != tc.want {
				t.Errorf("IsValidCreditLevel(%q) = %v, want %v", tc.level, got, tc.want)
			}
		})
	}
}
