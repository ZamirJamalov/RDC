package service

import (
        "strings"
        "testing"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
)

// contains is a small helper for substring matching in tests.
func contains(s, substr string) bool {
        return len(s) >= len(substr) && (s == substr || strings.Contains(s, substr))
}

func TestDetermineCreditLevel(t *testing.T) {
        tests := []struct {
                name         string
                analytics    *loanAnalytics
                akbScore     int
                currentLevel string
                wantLevel    string
        }{
                // --- AKB override ---
                {"AKB 700+ → valuable", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{}}, 750, "", model.CreditLevelValuable},
                {"AKB 699 → no override", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{}}, 699, "", model.CreditLevelNew},

                // --- No current level (new customer) ---
                {"No loans, no level → new", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{}}, 400, "", model.CreditLevelNew},

                // --- New → Trusted promotion ---
                {"New level: 2 loans, 0 delay, 3mo term → trusted", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "new": {{DelayDays: 0, TermMonths: 3}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "new", model.CreditLevelTrusted},

                {"New level: 2 loans, 2 delay, 3mo term → trusted", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "new": {{DelayDays: 2, TermMonths: 3}, {DelayDays: 1, TermMonths: 4}},
                }}, 400, "new", model.CreditLevelTrusted},

                {"New level: 2 loans, 3 delay (exceeded) → new (stay)", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "new": {{DelayDays: 3, TermMonths: 3}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "new", model.CreditLevelNew},

                {"New level: 2 loans, 0 delay, 2mo term (too short) → new", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "new": {{DelayDays: 0, TermMonths: 2}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "new", model.CreditLevelNew},

                {"New level: 1 loan only → new", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "new": {{DelayDays: 0, TermMonths: 3}},
                }}, 400, "new", model.CreditLevelNew},

                // --- Trusted → Valuable promotion ---
                {"Trusted: 2 loans, 0 delay, 3mo → valuable", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "trusted": {{DelayDays: 0, TermMonths: 3}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "trusted", model.CreditLevelValuable},

                {"Trusted: 2 loans, 3 delay, 3mo → valuable", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "trusted": {{DelayDays: 3, TermMonths: 3}, {DelayDays: 2, TermMonths: 4}},
                }}, 400, "trusted", model.CreditLevelValuable},

                {"Trusted: 2 loans, 4 delay (exceeded) → new (downgrade)", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "trusted": {{DelayDays: 4, TermMonths: 3}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "trusted", model.CreditLevelNew},

                // --- Valuable → Elite promotion ---
                {"Valuable: 2 loans, 0 delay, 2mo → elite", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "valuable": {{DelayDays: 0, TermMonths: 2}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "valuable", model.CreditLevelElite},

                {"Valuable: 2 loans, 4 delay, 2mo → elite", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "valuable": {{DelayDays: 4, TermMonths: 2}, {DelayDays: 2, TermMonths: 3}},
                }}, 400, "valuable", model.CreditLevelElite},

                {"Valuable: 2 loans, 5 delay (exceeded) → trusted (downgrade)", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "valuable": {{DelayDays: 5, TermMonths: 3}, {DelayDays: 0, TermMonths: 3}},
                }}, 400, "valuable", model.CreditLevelTrusted},

                // --- Elite stays elite ---
                {"Elite: stays elite", &loanAnalytics{loansByLevel: map[string][]completedLoanInfo{
                        "elite": {{DelayDays: 0, TermMonths: 6}},
                }}, 400, "elite", model.CreditLevelElite},
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        got := determineCreditLevel(tc.analytics, tc.akbScore, tc.currentLevel)
                        if got != tc.wantLevel {
                                t.Errorf("determineCreditLevel() = %q, want %q", got, tc.wantLevel)
                        }
                })
        }
}

func TestComputeAnalytics(t *testing.T) {
        // Test that analytics correctly groups loans by level_at_close
        loans := []lw.CustomerLoan{
                {Status: "completed", DelayDays: 1, TermMonths: 3, LevelAtClose: "new"},
                {Status: "completed", DelayDays: 0, TermMonths: 4, LevelAtClose: "new"},
                {Status: "completed", DelayDays: 2, TermMonths: 3, LevelAtClose: "trusted"},
                {Status: "active", DelayDays: 0, TermMonths: 6, LevelAtClose: "trusted"},
        }

        a := computeAnalytics(loans)
        if !a.hasActive {
                t.Error("expected hasActive=true")
        }
        if len(a.loansByLevel["new"]) != 2 {
                t.Errorf("expected 2 loans at new level, got %d", len(a.loansByLevel["new"]))
        }
        if len(a.loansByLevel["trusted"]) != 1 {
                t.Errorf("expected 1 completed loan at trusted level, got %d", len(a.loansByLevel["trusted"]))
        }
}

func TestPreviousLevel(t *testing.T) {
        tests := []struct {
                level string
                want  string
        }{
                {"new", "new"},
                {"trusted", "new"},
                {"valuable", "trusted"},
                {"elite", "valuable"},
        }
        for _, tc := range tests {
                t.Run(tc.level, func(t *testing.T) {
                        if got := previousLevel(tc.level); got != tc.want {
                                t.Errorf("previousLevel(%q) = %q, want %q", tc.level, got, tc.want)
                        }
                })
        }
}

func TestResolveUnlockPhase(t *testing.T) {
        tests := []struct {
                approved  int
                wantPhase int
        }{
                {0, 1},
                {1, 2},
                {2, 2},
        }
        for _, tc := range tests {
                t.Run("", func(t *testing.T) {
                        if got := resolveUnlockPhase(tc.approved); got != tc.wantPhase {
                                t.Errorf("resolveUnlockPhase(%d) = %d, want %d", tc.approved, got, tc.wantPhase)
                        }
                })
        }
}

func TestBuildRangeSummary(t *testing.T) {
        tests := []struct {
                name       string
                ranges     []repository.LevelRange
                unlockPhase int
                wantEmpty  bool
                wantSubstr string
        }{
                {"empty", nil, 1, true, ""},
                {"single range phase 1", []repository.LevelRange{{MinAmount: 100, MaxAmount: 500, TermMonths: 3, Rate: 30.0, Phase: 1}}, 1, false, "100-500 AZN"},
                {"multiple ranges phase 2", []repository.LevelRange{{MinAmount: 100, MaxAmount: 300, TermMonths: 3, Rate: 29.0, Phase: 1}, {MinAmount: 301, MaxAmount: 500, TermMonths: 3, Rate: 28.0, Phase: 2}}, 2, false, "100-500 AZN"},
        }
        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        got := buildRangeSummary(tc.ranges, tc.unlockPhase)
                        if tc.wantEmpty {
                                if got == "" {
                                        t.Error("expected non-empty message")
                                }
                                return
                        }
                        if !contains(got, tc.wantSubstr) {
                                t.Errorf("buildRangeSummary() = %q, want substring %q", got, tc.wantSubstr)
                        }
                })
        }
}
