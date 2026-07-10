package service

import (
        "context"
        "fmt"
        "rdc-source/internal/model"
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
func (e *CreditEngine) PreValidate(ctx context.Context, customerPIN string, amount float64, termMonths int, akbScore int) error {
        // 1. Get customer loans from LW
        customerLoans, err := e.lwProvider.GetCustomerLoans(ctx, customerPIN)
        if err != nil {
                return fmt.Errorf("failed to fetch customer loans from LW: %w", err)
        }

        // 2. Determine credit level (LW history + AKB score override)
        analytics := computeAnalytics(customerLoans.Loans)
        creditLevel := determineCreditLevel(analytics, akbScore)

        // 3. Determine unlock phase
        unlockPhase := 1
        approvedCount, err := e.appRepo.CountApprovedAtLevel(ctx, customerPIN, creditLevel)
        if err != nil {
                return fmt.Errorf("failed to count approved loans: %w", err)
        }
        if approvedCount >= 1 {
                unlockPhase = 2
        }

        // 4. Check if a rate exists for this combination
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

// ProcessApplication runs the full credit decision pipeline for a loan application.
//
// Pipeline steps:
//  1. Update status to StatusChecking
//  2. Fetch application + customer loans from LW
//  3. Run checks (active loan + payment history + credit level) — see credit_checks.go
//  4. Save all check results
//  5. Make approve/reject/pending_approval decision — see credit_checks.go::makeDecision
func (e *CreditEngine) ProcessApplication(ctx context.Context, appID int) error {
        // Step 0: Update status to "checking"
        if err := e.appRepo.UpdateApplicationStatus(ctx, appID, model.StatusChecking); err != nil {
                return fmt.Errorf("failed to set checking status: %w", err)
        }

        // Step 1: Get application
        app, err := e.appRepo.GetApplicationByID(ctx, appID)
        if err != nil {
                return fmt.Errorf("failed to get application %d: %w", appID, err)
        }

        // Step 2: Get customer loans from LW
        customerLoans, err := e.lwProvider.GetCustomerLoans(ctx, app.CustomerPIN)
        if err != nil {
                return fmt.Errorf("failed to fetch customer loans from LW: %w", err)
        }

        // Step 3: Pre-compute loan analytics needed by multiple checks
        analytics := computeAnalytics(customerLoans.Loans)

        // Step 4: Determine credit level (LMS history + AKB score override)
        creditLevel := determineCreditLevel(analytics, app.AkbScore)

        // Step 4b: Determine unlock phase — phase 1 = first loan at this level,
        // phase 2 = 1+ approved (wider amount ranges unlocked)
        approvedCount, err := e.appRepo.CountApprovedAtLevel(ctx, app.CustomerPIN, creditLevel)
        if err != nil {
                return fmt.Errorf("failed to count approved loans at level: %w", err)
        }
        unlockPhase := resolveUnlockPhase(approvedCount)

        // Step 5: Run checks (parallel loan-history checks + sequential credit-level check)
        checks := e.runChecks(analytics, app, creditLevel, unlockPhase, approvedCount)

        // Step 6: Save all check results
        for i := range checks {
                if err := e.appRepo.SaveCheckResult(ctx, appID, &checks[i]); err != nil {
                        return fmt.Errorf("failed to save check result %s: %w", checks[i].CheckType, err)
                }
        }

        // Step 7: Make decision (reject / approve / pending_approval)
        if err := e.makeDecision(ctx, appID, app, analytics, creditLevel, unlockPhase); err != nil {
                return fmt.Errorf("failed to apply decision: %w", err)
        }

        return nil
}
