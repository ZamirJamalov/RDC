package service

import (
        "fmt"
        "rdc-source/internal/model"
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

        // Check 4b: AZMK blacklist check (PR #53, rule 5) — sequential,
        // result pre-populated on `analytics` by the caller.
        azmkCheck := model.ApplicationCheckResult{
                CheckType: "azmk_blacklist_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if !analytics.azmkCheckAvailable {
                azmkCheck.Status = model.CheckStatusPassed
                azmkCheck.Detail = "AZMK blacklist check unavailable (LW error) — fail-soft"
        } else if analytics.azmkBlacklisted {
                azmkCheck.Status = model.CheckStatusFailed
                azmkCheck.Detail = "Customer is on the AZMK (Central Credit Register) blacklist"
        } else {
                azmkCheck.Status = model.CheckStatusPassed
                azmkCheck.Detail = "Customer is not on the AZMK blacklist"
        }
        checks = append(checks, azmkCheck)

        // Check 5: AKB stop factor check (PR #51, rule 4; PR #55 real format) —
        // sequential, data pre-populated on `analytics` by the caller.
        // AKB signals stop factor with Point == 1 and a 2-letter code in <response>.
        stopFactorCheck := model.ApplicationCheckResult{
                CheckType: "akb_stop_factor_check",
                CheckedAt: time.Now().Format(time.RFC3339),
        }
        if analytics.akbHasStopFactor {
                code := analytics.akbStopFactorCode
                if code == "" {
                        code = "unknown"
                }
                stopFactorCheck.Status = model.CheckStatusFailed
                stopFactorCheck.Detail = fmt.Sprintf("AKB stop factor: %s (score=1)", code)
        } else {
                stopFactorCheck.Status = model.CheckStatusPassed
                stopFactorCheck.Detail = "No AKB stop factor"
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

        // Checks 8–14: AKB History-based checks (PR #52).
        // All 7 are gated on akbHistoryAvailable — when AKB is unreachable, each
        // check is recorded with passed status + "fail-soft" note so the operator
        // can see in the UI that AKB history was not consulted.
        if !analytics.akbHistoryAvailable {
                failSoftNote := "AKB history unavailable (LW error) — fail-soft"
                checks = append(checks,
                        akbHistoryCheck("delay_ratio_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("active_delay_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("delay_history_3m_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("delay_history_6m_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("delay_history_12m_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("delay_history_18m_check", model.CheckStatusPassed, failSoftNote),
                        akbHistoryCheck("monthly_payments_check", model.CheckStatusPassed, failSoftNote),
                )
                return checks
        }

        // Check 8: Delay ratio over last 24 months (rule 2)
        checks = append(checks, akbHistoryCheck("delay_ratio_check",
                thresholdStatus(analytics.delayRatio > 6),
                fmt.Sprintf("Delay ratio %.2f days/month over last 24 months (max 6)", analytics.delayRatio)))

        // Check 9: Active liability current delay (rule 6)
        checks = append(checks, akbHistoryCheck("active_delay_check",
                thresholdStatus(analytics.activeMaxDelayDays > 5),
                fmt.Sprintf("Active loan max current delay %d days (max 5)", analytics.activeMaxDelayDays)))

        // Check 10: Last 3 months max delay (rule 7)
        checks = append(checks, akbHistoryCheck("delay_history_3m_check",
                thresholdStatus(analytics.maxDelayLast3Months >= 20),
                fmt.Sprintf("Max delay %d days in last 3 months (threshold 20)", analytics.maxDelayLast3Months)))

        // Check 11: Last 6 months max delay (rule 8)
        checks = append(checks, akbHistoryCheck("delay_history_6m_check",
                thresholdStatus(analytics.maxDelayLast6Months >= 30),
                fmt.Sprintf("Max delay %d days in last 6 months (threshold 30)", analytics.maxDelayLast6Months)))

        // Check 12: Last 12 months max delay (rule 9)
        checks = append(checks, akbHistoryCheck("delay_history_12m_check",
                thresholdStatus(analytics.maxDelayLast12Months >= 45),
                fmt.Sprintf("Max delay %d days in last 12 months (threshold 45)", analytics.maxDelayLast12Months)))

        // Check 13: Last 18 months max delay (rule 10)
        checks = append(checks, akbHistoryCheck("delay_history_18m_check",
                thresholdStatus(analytics.maxDelayLast18Months >= 60),
                fmt.Sprintf("Max delay %d days in last 18 months (threshold 60)", analytics.maxDelayLast18Months)))

        // Check 14: Total monthly payments on active liabilities (rule 12)
        checks = append(checks, akbHistoryCheck("monthly_payments_check",
                thresholdStatus(analytics.totalMonthlyPayments > 2000),
                fmt.Sprintf("Total monthly payments %.2f AZN (max 2000)", analytics.totalMonthlyPayments)))

        return checks
}

// akbHistoryCheck builds an ApplicationCheckResult for an AKB-History-derived
// check with the given type, status, and detail.
func akbHistoryCheck(checkType, status, detail string) model.ApplicationCheckResult {
        return model.ApplicationCheckResult{
                CheckType: checkType,
                Status:    status,
                Detail:    detail,
                CheckedAt: time.Now().Format(time.RFC3339),
        }
}

// thresholdStatus returns failed if the condition is true, else passed.
func thresholdStatus(failed bool) string {
        if failed {
                return model.CheckStatusFailed
        }
        return model.CheckStatusPassed
}

// resolveUnlockPhase returns 2 if the customer already has 1+ approved application at
// this level (phase 2 unlocks the wider amount ranges), otherwise 1.
func resolveUnlockPhase(approvedCount int) int {
        if approvedCount >= 1 {
                return 2
        }
        return 1
}
