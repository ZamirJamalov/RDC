package service

import (
        "fmt"
        "rdc-source/internal/model"
        "strings"
        "sync"
        "time"
)

// runChecks executes the parallel loan-history checks (active loan + payment
// history) and the sequential checks (credit level + blacklist + AKB + age),
// returning the aggregated results.
//
// The active-loan and payment-history checks run in parallel goroutines (they
// both depend only on the pre-computed `analytics`, so there is no contention
// on LW or DB). The credit-level, blacklist, AKB-score, AKB-stop-factor, and
// age checks run sequentially after them: credit-level needs DB lookups,
// blacklist was already fetched by the caller (it's an LW call we don't want
// to duplicate), and AKB/age data is already populated on `analytics` by the
// caller.
func (e *CreditEngine) runChecks(analytics *loanAnalytics, app *model.LoanApplication,
        creditLevel string, unlockPhase int, approvedCount int, blacklisted bool) []model.ApplicationCheckResult {

        var wg sync.WaitGroup
        var mu sync.Mutex
        var checks []model.ApplicationCheckResult

        // Check 1: Active loan check
        wg.Add(1)
        go func() {
                defer wg.Done()
                check := model.ApplicationCheckResult{
                        CheckType: "lms_active_loan_check",
                        CheckedAt: time.Now().Format(time.RFC3339),
                }
                if analytics.hasActive {
                        check.Status = model.CheckStatusFailed
                        check.Detail = "Customer has active loans"
                } else {
                        check.Status = model.CheckStatusPassed
                        check.Detail = "No active loans found"
                }
                mu.Lock()
                checks = append(checks, check)
                mu.Unlock()
        }()

        // Check 2: Payment history check
        wg.Add(1)
        go func() {
                defer wg.Done()
                check := model.ApplicationCheckResult{
                        CheckType: "lms_payment_history_check",
                        CheckedAt: time.Now().Format(time.RFC3339),
                }
                if analytics.completedCount == 0 {
                        check.Status = model.CheckStatusPassed
                        check.Detail = "No completed loans — no payment history to evaluate"
                } else if !analytics.allOnTime {
                        check.Status = model.CheckStatusFailed
                        check.Detail = fmt.Sprintf("Late payments found in %d completed loan(s)", analytics.completedCount)
                } else {
                        check.Status = model.CheckStatusPassed
                        check.Detail = fmt.Sprintf("All %d completed loan(s) paid on time", analytics.completedCount)
                }
                mu.Lock()
                checks = append(checks, check)
                mu.Unlock()
        }()

        wg.Wait()

        // Check 3: Credit level check (sequential, depends on DB)
        akbNote := ""
        if app.AkbScore >= 700 {
                akbNote = " (AKB override: 700+ -> valuable)"
        }
        checks = append(checks, model.ApplicationCheckResult{
                CheckType: "credit_level_check",
                Status:    model.CheckStatusPassed,
                Detail: fmt.Sprintf("Credit level: %s, unlock_phase: %d (%d previous approved loan(s) at this level)%s",
                        creditLevel, unlockPhase, approvedCount, akbNote),
                CheckedAt: time.Now().Format(time.RFC3339),
        })

        // Check 4: Blacklist check (T-1.5) — sequential, result pre-fetched by caller
        blacklistCheck := model.ApplicationCheckResult{
                CheckType: "blacklist_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if blacklisted {
                blacklistCheck.Status = model.CheckStatusFailed
                blacklistCheck.Detail = "Customer is blacklisted"
        } else {
                blacklistCheck.Status = model.CheckStatusPassed
                blacklistCheck.Detail = "Customer is not blacklisted"
        }
        checks = append(checks, blacklistCheck)

        // Check 5: AKB stop factor check (PR #51, rule 4) — sequential,
        // data pre-populated on `analytics` by the caller.
        stopFactorCheck := model.ApplicationCheckResult{
                CheckType: "akb_stop_factor_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if len(analytics.akbStopFactors) > 0 {
                stopFactorCheck.Status = model.CheckStatusFailed
                stopFactorCheck.Detail = fmt.Sprintf("AKB stop factor(s): %s",
                        strings.Join(analytics.akbStopFactors, ", "))
        } else {
                stopFactorCheck.Status = model.CheckStatusPassed
                stopFactorCheck.Detail = "No AKB stop factors"
        }
        checks = append(checks, stopFactorCheck)

        // Check 6: AKB score check (PR #51, rule 1) — sequential.
        // Score 0 is treated as "no AKB information" and does NOT fail the check.
        akbScoreCheck := model.ApplicationCheckResult{
                CheckType: "akb_score_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if analytics.akbScore > 0 && analytics.akbScore < 200 {
                akbScoreCheck.Status = model.CheckStatusFailed
                akbScoreCheck.Detail = fmt.Sprintf("AKB score %d below minimum (200)", analytics.akbScore)
        } else if analytics.akbScore == 0 {
                akbScoreCheck.Status = model.CheckStatusPassed
                akbScoreCheck.Detail = "AKB score not available (no override from LW)"
        } else {
                akbScoreCheck.Status = model.CheckStatusPassed
                akbScoreCheck.Detail = fmt.Sprintf("AKB score %d", analytics.akbScore)
        }
        checks = append(checks, akbScoreCheck)

        // Check 7: Age check (PR #51, rule 3) — sequential.
        // Age 0 is treated as "unknown" (GetPersonalInfo failed) and does NOT fail.
        ageCheck := model.ApplicationCheckResult{
                CheckType: "age_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if analytics.customerAge > 69 {
                ageCheck.Status = model.CheckStatusFailed
                ageCheck.Detail = fmt.Sprintf("Customer age %d exceeds maximum (69)", analytics.customerAge)
        } else if analytics.customerAge == 0 {
                ageCheck.Status = model.CheckStatusPassed
                ageCheck.Detail = "Age unknown (GetPersonalInfo failed) — fail-soft"
        } else {
                ageCheck.Status = model.CheckStatusPassed
                ageCheck.Detail = fmt.Sprintf("Customer age %d", analytics.customerAge)
        }
        checks = append(checks, ageCheck)

        return checks
}

// resolveUnlockPhase returns 2 if the customer already has 1+ approved application at
// this level (phase 2 unlocks the wider amount ranges), otherwise 1.
func resolveUnlockPhase(approvedCount int) int {
        if approvedCount >= 1 {
                return 2
        }
        return 1
}
