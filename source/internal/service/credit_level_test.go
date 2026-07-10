package service

import (
	"testing"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/lw"
)

// TestDetermineCreditLevel covers all branches of the credit-level
// determination logic. The rules (from credit_level.go):
//
//   - AKB score 700+  → valuable (override, regardless of loan history)
//   - 0 completed     → new
//   - 1 completed, all on time → trusted
//   - 2+ completed, all on time → valuable
//   - 2+ completed, all on time, 1+ early → elite
//   - has late payments → new (even if completed count is high)
func TestDetermineCreditLevel(t *testing.T) {
	tests := []struct {
		name       string
		analytics  *loanAnalytics
		akbScore   int
		wantLevel  string
	}{
		{
			name:      "AKB override 700+ → valuable (no loan history)",
			analytics: &loanAnalytics{completedCount: 0, allOnTime: true},
			akbScore:  750,
			wantLevel: model.CreditLevelValuable,
		},
		{
			name:      "AKB override exactly 700 → valuable",
			analytics: &loanAnalytics{completedCount: 0, allOnTime: true},
			akbScore:  700,
			wantLevel: model.CreditLevelValuable,
		},
		{
			name:      "AKB 699 does NOT override → new (no completed loans)",
			analytics: &loanAnalytics{completedCount: 0, allOnTime: true},
			akbScore:  699,
			wantLevel: model.CreditLevelNew,
		},
		{
			name:      "no completed loans, AKB low → new",
			analytics: &loanAnalytics{completedCount: 0, allOnTime: true},
			akbScore:  400,
			wantLevel: model.CreditLevelNew,
		},
		{
			name:      "1 completed on time → trusted",
			analytics: &loanAnalytics{completedCount: 1, allOnTime: true},
			akbScore:  400,
			wantLevel: model.CreditLevelTrusted,
		},
		{
			name:      "2 completed on time, no early → valuable",
			analytics: &loanAnalytics{completedCount: 2, allOnTime: true, hasEarly: false},
			akbScore:  400,
			wantLevel: model.CreditLevelValuable,
		},
		{
			name:      "2 completed on time + 1 early → elite",
			analytics: &loanAnalytics{completedCount: 2, allOnTime: true, hasEarly: true},
			akbScore:  400,
			wantLevel: model.CreditLevelElite,
		},
		{
			name:      "3 completed on time + 1 early → elite",
			analytics: &loanAnalytics{completedCount: 3, allOnTime: true, hasEarly: true},
			akbScore:  400,
			wantLevel: model.CreditLevelElite,
		},
		{
			name:      "5 completed with late payments → new (downgraded)",
			analytics: &loanAnalytics{completedCount: 5, allOnTime: false, hasEarly: true},
			akbScore:  400,
			wantLevel: model.CreditLevelNew,
		},
		{
			name:      "1 completed with late payment → new (not trusted)",
			analytics: &loanAnalytics{completedCount: 1, allOnTime: false},
			akbScore:  400,
			wantLevel: model.CreditLevelNew,
		},
		{
			name:      "AKB override wins even with late payments",
			analytics: &loanAnalytics{completedCount: 5, allOnTime: false},
			akbScore:  800,
			wantLevel: model.CreditLevelValuable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := determineCreditLevel(tc.analytics, tc.akbScore)
			if got != tc.wantLevel {
				t.Errorf("determineCreditLevel() = %q, want %q", got, tc.wantLevel)
			}
		})
	}
}

// TestComputeAnalytics covers the loan-history aggregation logic.
// Loans with status "active" set hasActive; "completed" or "closed" count
// toward completedCount and influence allOnTime / hasEarly.
func TestComputeAnalytics(t *testing.T) {
	tests := []struct {
		name           string
		loans          []lw.CustomerLoan
		wantCompleted  int
		wantAllOnTime  bool
		wantHasEarly   bool
		wantHasActive  bool
	}{
		{
			name:          "empty loan list",
			loans:         nil,
			wantCompleted: 0,
			wantAllOnTime: true, // default true, vacuously
			wantHasEarly:  false,
			wantHasActive: false,
		},
		{
			name: "only active loans",
			loans: []lw.CustomerLoan{
				{Status: "active", WasOnTime: true},
			},
			wantCompleted: 0,
			wantAllOnTime: true,
			wantHasEarly:  false,
			wantHasActive: true,
		},
		{
			name: "one completed on time",
			loans: []lw.CustomerLoan{
				{Status: "completed", WasOnTime: true, EarlyCompletion: false},
			},
			wantCompleted: 1,
			wantAllOnTime: true,
			wantHasEarly:  false,
			wantHasActive: false,
		},
		{
			name: "one completed with late payment",
			loans: []lw.CustomerLoan{
				{Status: "completed", WasOnTime: false, EarlyCompletion: false},
			},
			wantCompleted: 1,
			wantAllOnTime: false,
			wantHasEarly:  false,
			wantHasActive: false,
		},
		{
			name: "one completed early (on time)",
			loans: []lw.CustomerLoan{
				{Status: "completed", WasOnTime: true, EarlyCompletion: true},
			},
			wantCompleted: 1,
			wantAllOnTime: true,
			wantHasEarly:  true,
			wantHasActive: false,
		},
		{
			name: "closed status counts as completed",
			loans: []lw.CustomerLoan{
				{Status: "closed", WasOnTime: true, EarlyCompletion: false},
			},
			wantCompleted: 1,
			wantAllOnTime: true,
			wantHasEarly:  false,
			wantHasActive: false,
		},
		{
			name: "mixed: 1 active + 2 completed (1 on time + 1 late + 1 early)",
			loans: []lw.CustomerLoan{
				{Status: "active", WasOnTime: true},
				{Status: "completed", WasOnTime: true, EarlyCompletion: true},
				{Status: "completed", WasOnTime: false, EarlyCompletion: false},
			},
			wantCompleted: 2,
			wantAllOnTime: false, // the late one drags it down
			wantHasEarly:  true,
			wantHasActive: true,
		},
		{
			name: "unknown status ignored",
			loans: []lw.CustomerLoan{
				{Status: "paused", WasOnTime: true},
				{Status: "suspended", WasOnTime: false},
			},
			wantCompleted: 0,
			wantAllOnTime: true, // stays at default
			wantHasEarly:  false,
			wantHasActive: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeAnalytics(tc.loans)
			if got.completedCount != tc.wantCompleted {
				t.Errorf("completedCount = %d, want %d", got.completedCount, tc.wantCompleted)
			}
			if got.allOnTime != tc.wantAllOnTime {
				t.Errorf("allOnTime = %v, want %v", got.allOnTime, tc.wantAllOnTime)
			}
			if got.hasEarly != tc.wantHasEarly {
				t.Errorf("hasEarly = %v, want %v", got.hasEarly, tc.wantHasEarly)
			}
			if got.hasActive != tc.wantHasActive {
				t.Errorf("hasActive = %v, want %v", got.hasActive, tc.wantHasActive)
			}
		})
	}
}

// TestResolveUnlockPhase verifies the unlock phase calculation.
//   - 0 approved → phase 1 (first loan at this level, limited ranges)
//   - 1+ approved → phase 2 (full ranges unlocked)
func TestResolveUnlockPhase(t *testing.T) {
	tests := []struct {
		name          string
		approvedCount int
		wantPhase     int
	}{
		{"0 approved → phase 1", 0, 1},
		{"1 approved → phase 2", 1, 2},
		{"2 approved → phase 2", 2, 2},
		{"5 approved → phase 2", 5, 2},
		{"100 approved → phase 2", 100, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveUnlockPhase(tc.approvedCount)
			if got != tc.wantPhase {
				t.Errorf("resolveUnlockPhase(%d) = %d, want %d",
					tc.approvedCount, got, tc.wantPhase)
			}
		})
	}
}

// TestBuildRangeSummary verifies the human-readable error message builder
// for cases when a customer requests an amount/term that has no matching rate.
func TestBuildRangeSummary(t *testing.T) {
	tests := []struct {
		name        string
		ranges      []repository.LevelRange
		unlockPhase int
		wantSubstr  string // the result must contain this substring
		wantEmpty   bool   // if true, expect the "no ranges" message
	}{
		{
			name:        "empty ranges → no-configured message",
			ranges:      nil,
			unlockPhase: 1,
			wantEmpty:   true,
		},
		{
			name: "single range, phase 1",
			ranges: []repository.LevelRange{
				{MinAmount: 100, MaxAmount: 500, TermMonths: 3, Rate: 30.0, Phase: 1},
			},
			unlockPhase: 1,
			wantSubstr:  "100-500 AZN",
		},
		{
			name: "multiple ranges with different terms",
			ranges: []repository.LevelRange{
				{MinAmount: 100, MaxAmount: 300, TermMonths: 3, Rate: 29.0, Phase: 1},
				{MinAmount: 301, MaxAmount: 500, TermMonths: 3, Rate: 28.0, Phase: 1},
				{MinAmount: 501, MaxAmount: 700, TermMonths: 6, Rate: 29.0, Phase: 1},
			},
			unlockPhase: 1,
			wantSubstr:  "100-700 AZN",
		},
		{
			name: "phase 1 includes hint to complete a loan",
			ranges: []repository.LevelRange{
				{MinAmount: 100, MaxAmount: 300, TermMonths: 3, Rate: 30.0, Phase: 1},
			},
			unlockPhase: 1,
			wantSubstr:  "phase 1",
		},
		{
			name: "phase 2 does NOT include the hint",
			ranges: []repository.LevelRange{
				{MinAmount: 100, MaxAmount: 300, TermMonths: 3, Rate: 30.0, Phase: 2},
			},
			unlockPhase: 2,
			wantSubstr:  "100-300 AZN",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildRangeSummary(tc.ranges, tc.unlockPhase)
			if tc.wantEmpty {
				if got == "" {
					t.Error("expected non-empty message for empty ranges")
				}
				return
			}
			if !contains(got, tc.wantSubstr) {
				t.Errorf("buildRangeSummary() = %q, want substring %q", got, tc.wantSubstr)
			}
		})
	}
}

// contains is a small helper for substring matching in tests.
// (We don't use strings.Contains directly to keep imports minimal in the test file.)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
