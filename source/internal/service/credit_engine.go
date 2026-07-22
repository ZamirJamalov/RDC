package service

import (
        "context"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
)

// CreditEngine processes loan applications through the credit decision pipeline.
type CreditEngine struct {
        lwProvider lw.Provider
        appRepo    ApplicationStore
}

// NewCreditEngine creates a new CreditEngine with the given dependencies.
// The repo parameter accepts any ApplicationStore implementation (e.g.
// *repository.ApplicationRepo in production, or a mock in tests).
func NewCreditEngine(provider lw.Provider, repo ApplicationStore) *CreditEngine {
        return &CreditEngine{
                lwProvider: provider,
                appRepo:    repo,
        }
}

// PreValidate checks whether the requested amount and term are valid for the customer's
// credit level BEFORE creating the application. Returns a descriptive error if not.
//
// This is called synchronously in CreateApplication so the user gets an immediate 400 response
// instead of a delayed rejection via the async engine.
//
// AKB score resolution (T-1.6): the score is fetched from LW first; if LW returns
// a non-zero score it overrides the request-supplied value. If LW fails or returns
// 0, the request-supplied value is used as a fallback (preserving backward compat).
func (e *CreditEngine) PreValidate(ctx context.Context, customerPIN string, amount float64, termMonths int, akbScore int) error {
        // 1. Get customer loans from LW
        customerLoans, err := e.lwProvider.GetCustomerLoans(ctx, customerPIN)
        if err != nil {
                return fmt.Errorf("failed to fetch customer loans from LW: %w", err)
        }

        // 2. Resolve AKB score (LW first, request value as fallback)
        resolvedAkb := e.resolveAkbScore(ctx, customerPIN, akbScore)

        // 3. Determine credit level (LW history + AKB score override + current level from DB)
        analytics := computeAnalytics(customerLoans.Loans)
        currentLevel, _ := e.appRepo.GetCustomerCurrentLevel(ctx, customerPIN)
        creditLevel := determineCreditLevel(analytics, resolvedAkb, currentLevel)

        // 4. Determine unlock phase
        unlockPhase := 1
        approvedCount, err := e.appRepo.CountApprovedAtLevel(ctx, customerPIN, creditLevel)
        if err != nil {
                return fmt.Errorf("failed to count approved loans: %w", err)
        }
        if approvedCount >= 1 {
                unlockPhase = 2
        }

        // 5. Check if a rate exists for this combination
        rate, err := e.appRepo.GetCreditLevelRate(ctx, creditLevel, amount, termMonths, unlockPhase)
        if err != nil {
                // No rate found — build a descriptive error message
                ranges, rngErr := e.appRepo.GetLevelRanges(ctx, creditLevel, unlockPhase)
                if rngErr != nil {
                        return fmt.Errorf("requested amount %.0f AZN, term %d ay is not valid for '%s' level (phase %d)",
                                amount, termMonths, creditLevel, unlockPhase)
                }
                return fmt.Errorf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s",
                        amount, termMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
        }

        _ = rate // rate exists — validation passed
        return nil
}

// resolveAkbScore fetches the AKB score from LW. If LW returns a non-zero
// score, that value wins (authoritative source). If LW fails or returns 0,
// the request-supplied fallback is used. Errors from LW are logged but never
// cause PreValidate / ProcessApplication to fail — AKB is an enhancement,
// not a hard dependency.
//
// T-1.6: previously the engine used the request-supplied akbScore directly,
// which meant the operator could inject any score they wanted. Now LW is the
// source of truth (when available).
//
// PR #51: this method now delegates to resolveAkbScoreAndStopFactors and
// discards the stop factors. PreValidate / offer flow don't need them —
// only ProcessApplication uses the stop factors (via the new method).
func (e *CreditEngine) resolveAkbScore(ctx context.Context, customerPIN string, fallback int) int {
        score, _ := e.resolveAkbScoreAndStopFactors(ctx, customerPIN, fallback)
        return score
}

// resolveAkbScoreAndStopFactors is the PR #51 extension of resolveAkbScore:
// it returns both the score and the stop factor list returned by AKB.
//
// Fail-soft: on LW error or nil response, the fallback score is used and
// the stop factor list is empty. This matches the existing resolveAkbScore
// semantics — AKB is an enhancement, not a hard dependency.
//
// The caller populates the loanAnalytics struct with these values before
// invoking computeDecision / runChecks.
func (e *CreditEngine) resolveAkbScoreAndStopFactors(ctx context.Context, customerPIN string, fallback int) (int, []string) {
        resp, err := e.lwProvider.GetAkbScore(ctx, customerPIN, "")
        if err != nil {
                slog.Warn("failed to fetch AKB score from LW — using request fallback",
                        "customer_pin", customerPIN,
                        "fallback_score", fallback,
                        "error", err)
                return fallback, nil
        }
        if resp == nil || resp.Score == 0 {
                return fallback, nil
        }
        return resp.Score, resp.StopFactors
}

// resolveCustomerAge fetches the customer's personal info from LW (via DIN)
// and computes age in years from DateOfBirth. Returns 0 if the date cannot
// be parsed or GetPersonalInfo fails — the caller treats 0 as "unknown age"
// and does NOT reject on it (fail-soft).
func (e *CreditEngine) resolveCustomerAge(ctx context.Context, customerPIN, serial string) int {
        resp, err := e.lwProvider.GetPersonalInfo(ctx, customerPIN, serial)
        if err != nil {
                slog.Warn("failed to fetch personal info from LW — age unknown (fail-soft)",
                        "customer_pin", customerPIN,
                        "error", err)
                return 0
        }
        if resp == nil || resp.DateOfBirth == "" {
                return 0
        }
        dob, err := time.Parse("2006-01-02", resp.DateOfBirth)
        if err != nil {
                slog.Warn("failed to parse DOB from personal info — age unknown (fail-soft)",
                        "customer_pin", customerPIN,
                        "dob", resp.DateOfBirth,
                        "error", err)
                return 0
        }
        now := time.Now()
        age := now.Year() - dob.Year()
        // Subtract 1 if the birthday hasn't happened yet this year.
        if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
                age--
        }
        if age < 0 {
                return 0 // defensive: bad DOB in the future
        }
        return age
}

// ProcessApplication runs the full credit decision pipeline for a loan application.
//
// Pipeline (DB writes wrapped in a single transaction — T-1.3):
//  1. Status → checking (outside tx, visible immediately)
//  2. Fetch application + customer loans from LW
//  3. Resolve AKB score from LW (T-1.6)
//  4. Blacklist check from LW (T-1.5, fail-open on error)
//  5. Determine credit level + unlock phase
//  6. Run checks (active-loan + payment-history + credit-level + blacklist)
//  7. Compute decision (credit_decision.go::computeDecision)
//  8. Save checks + apply decision in transaction (T-1.3)
//  9. If approved → call LW.ApproveLoan (T-1.1), downgrade to rejected on failure
func (e *CreditEngine) ProcessApplication(ctx context.Context, appID int) error {
        // Step 1: Update status to "checking" (outside tx — visible immediately)
        if err := e.appRepo.UpdateApplicationStatus(ctx, appID, model.StatusChecking); err != nil {
                return fmt.Errorf("failed to set checking status: %w", err)
        }

        // Step 2: Get application
        app, err := e.appRepo.GetApplicationByID(ctx, appID)
        if err != nil {
                return fmt.Errorf("failed to get application %d: %w", appID, err)
        }

        // Step 3: Get customer loans from LW
        customerLoans, err := e.lwProvider.GetCustomerLoans(ctx, app.CustomerPIN)
        if err != nil {
                return fmt.Errorf("failed to fetch customer loans from LW: %w", err)
        }

        // Step 4: Pre-compute loan analytics
        analytics := computeAnalytics(customerLoans.Loans)

        // Step 5: Resolve AKB score + stop factors from LW (T-1.6, PR #51)
        resolvedAkb, stopFactors := e.resolveAkbScoreAndStopFactors(ctx, app.CustomerPIN, app.AkbScore)
        app.AkbScore = resolvedAkb
        analytics.akbScore = resolvedAkb
        analytics.akbStopFactors = stopFactors

        // Step 5b: Resolve customer age from LW PersonalInfo (PR #51, rule 3)
        analytics.customerAge = e.resolveCustomerAge(ctx, app.CustomerPIN, app.CustomerSerial)

        // Step 6: Blacklist check (T-1.5, fail-open)
        blacklisted, blacklistErr := e.lwProvider.CheckBlacklist(ctx, app.CustomerPIN)
        if blacklistErr != nil {
                slog.Warn("failed to check blacklist — treating as not blacklisted",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "error", blacklistErr)
                blacklisted = false
        }

        // Step 7: Determine credit level + unlock phase
        currentLevel, _ := e.appRepo.GetCustomerCurrentLevel(ctx, app.CustomerPIN)
        creditLevel := determineCreditLevel(analytics, resolvedAkb, currentLevel)
        approvedCount, err := e.appRepo.CountApprovedAtLevel(ctx, app.CustomerPIN, creditLevel)
        if err != nil {
                return fmt.Errorf("failed to count approved loans at level: %w", err)
        }
        unlockPhase := resolveUnlockPhase(approvedCount)

        // Step 8: Run checks
        checks := e.runChecks(analytics, app, creditLevel, unlockPhase, approvedCount, blacklisted)

        // Step 9: Compute decision
        decision, err := e.computeDecision(analytics, creditLevel, unlockPhase, app, blacklisted)
        if err != nil {
                return fmt.Errorf("failed to compute decision: %w", err)
        }

        // Step 10: Save checks + apply decision in a transaction (T-1.3)
        if err := e.appRepo.WithTx(ctx, func(runner repository.TxRunner) error {
                for i := range checks {
                        if err := e.appRepo.SaveCheckResultTx(ctx, runner, appID, &checks[i]); err != nil {
                                return fmt.Errorf("failed to save check result %s: %w", checks[i].CheckType, err)
                        }
                }
                return e.applyDecisionTx(ctx, runner, appID, app, creditLevel, decision)
        }); err != nil {
                return fmt.Errorf("failed to apply decision (transaction rolled back): %w", err)
        }

        // Step 11: If approved, notify LW (T-1.1). After commit — can't roll back.
        // On failure, downgrade to rejected with a descriptive reason.
        if decision.Status == model.StatusApproved {
                if err := e.notifyLwApproval(ctx, app, creditLevel, decision); err != nil {
                        slog.Error("LW ApproveLoan failed — downgrading application to rejected",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "error", err)
                        rejectReason := fmt.Sprintf("LW approval failed: %v", err)
                        if err := e.appRepo.UpdateApplicationDecision(ctx, appID,
                                model.StatusRejected, creditLevel, rejectReason, 0, 0, 0); err != nil {
                                return fmt.Errorf("failed to downgrade after LW rejection: %w", err)
                        }
                }
        }

        return nil
}
