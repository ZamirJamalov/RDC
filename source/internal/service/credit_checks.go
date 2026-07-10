package service

import (
        "context"
        "fmt"
        "rdc-source/internal/model"
        "sync"
        "time"
)

// runChecks executes the parallel loan-history checks (active loan + payment history)
// and the credit-level check, returning the aggregated results.
//
// The active-loan and payment-history checks run in parallel goroutines (they both
// depend only on the pre-computed `analytics`, so there is no contention on LW or DB).
// The credit-level check runs sequentially after them because it needs DB lookups
// (approved count + phase determination) that we want to keep in one place.
func (e *CreditEngine) runChecks(analytics *loanAnalytics, app *model.LoanApplication,
        creditLevel string, unlockPhase int, approvedCount int) []model.ApplicationCheckResult {

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
                        check.Status = "failed"
                        check.Detail = "Customer has active loans"
                } else {
                        check.Status = "passed"
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
                        check.Status = "passed"
                        check.Detail = "No completed loans — no payment history to evaluate"
                } else if !analytics.allOnTime {
                        check.Status = "failed"
                        check.Detail = fmt.Sprintf("Late payments found in %d completed loan(s)", analytics.completedCount)
                } else {
                        check.Status = "passed"
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
                Status:    "passed",
                Detail: fmt.Sprintf("Credit level: %s, unlock_phase: %d (%d previous approved loan(s) at this level)%s",
                        creditLevel, unlockPhase, approvedCount, akbNote),
                CheckedAt: time.Now().Format(time.RFC3339),
        })

        return checks
}

// makeDecision applies the final credit decision based on analytics, credit level,
// and the rate lookup result. Updates the application record via the repository.
//
// Decision order:
//  1. Active loan → reject
//  2. Late payments in history → reject
//  3. No applicable rate → reject (with descriptive reason)
//  4. Elite level → auto-approve
//  5. Other levels → pending_approval (manual review)
func (e *CreditEngine) makeDecision(ctx context.Context, appID int, app *model.LoanApplication,
        analytics *loanAnalytics, creditLevel string, unlockPhase int) error {

        // 1. Reject: active loans
        if analytics.hasActive {
                return e.appRepo.UpdateApplicationDecision(ctx, appID,
                        "rejected", creditLevel, "Customer has active loans", 0, 0)
        }

        // 2. Reject: late payments in history
        if analytics.completedCount > 0 && !analytics.allOnTime {
                return e.appRepo.UpdateApplicationDecision(ctx, appID,
                        "rejected", creditLevel, "Late payments found in loan history", 0, 0)
        }

        // 3. Look up the rate for this credit level, amount, term, and unlock phase
        rate, err := e.appRepo.GetCreditLevelRate(ctx, creditLevel, app.Amount, app.TermMonths, unlockPhase)
        if err != nil {
                reason := fmt.Sprintf("No applicable rate: %v", err)
                if ranges, rngErr := e.appRepo.GetLevelRanges(ctx, creditLevel, unlockPhase); rngErr == nil {
                        reason = fmt.Sprintf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s",
                                app.Amount, app.TermMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
                }
                return e.appRepo.UpdateApplicationDecision(ctx, appID,
                        "rejected", creditLevel, reason, 0, 0)
        }

        // 4. Elite: auto-approve; 5. Others: pending_approval (manual review)
        if creditLevel == "elite" {
                if err := e.appRepo.UpdateApplicationDecision(ctx, appID,
                        "approved", creditLevel, "", app.Amount, rate); err != nil {
                        return err
                }
                // Save credit level history (best-effort, do not fail the decision on this)
                _ = e.appRepo.SaveCreditLevelHistory(ctx, app.CustomerPIN, creditLevel, appID)
                return nil
        }

        // New / Trusted / Valuable: pending_approval with proposed amount and rate
        return e.appRepo.UpdateApplicationDecision(ctx, appID,
                "pending_approval", creditLevel, "", app.Amount, rate)
}

// resolveUnlockPhase returns 2 if the customer already has 1+ approved application at
// this level (phase 2 unlocks the wider amount ranges), otherwise 1.
func resolveUnlockPhase(approvedCount int) int {
        if approvedCount >= 1 {
                return 2
        }
        return 1
}
