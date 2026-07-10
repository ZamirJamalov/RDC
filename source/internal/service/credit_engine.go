package service

import (
	"context"
	"fmt"
	"math"
	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/lw"
	"strings"
	"sync"
	"time"
)

// CreditEngine processes loan applications through the credit decision pipeline.
type CreditEngine struct {
	lwProvider lw.Provider
	appRepo    *repository.ApplicationRepo
}

// NewCreditEngine creates a new CreditEngine with the given dependencies.
func NewCreditEngine(provider lw.Provider, repo *repository.ApplicationRepo) *CreditEngine {
	return &CreditEngine{
		lwProvider: provider,
		appRepo:    repo,
	}
}

// loanAnalytics holds pre-computed loan history metrics used by the credit engine.
type loanAnalytics struct {
	completedCount int
	allOnTime      bool
	hasEarly       bool
	hasActive      bool
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
			return fmt.Errorf("requested amount %.0f AZN, term %d ay is not valid for '%s' level (phase %d)", amount, termMonths, creditLevel, unlockPhase)
		}
		return fmt.Errorf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s", amount, termMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
	}

	_ = rate // rate exists — validation passed
	return nil
}

// buildRangeSummary creates a human-readable summary of available ranges for error messages.
func buildRangeSummary(ranges []repository.LevelRange, unlockPhase int) string {
	if len(ranges) == 0 {
		return "Bu level ucun hec bir araliq konfiqurasiya edilmeyib."
	}

	// Collect unique terms and track min/max amounts
	terms := make(map[int]bool)
	minAmt := math.MaxFloat64
	maxAmt := 0.0
	for _, r := range ranges {
		terms[r.TermMonths] = true
		if r.MinAmount < minAmt {
			minAmt = r.MinAmount
		}
		if r.MaxAmount > maxAmt {
			maxAmt = r.MaxAmount
		}
	}

	// Build term list
	termList := make([]string, 0, len(terms))
	for t := range terms {
		termList = append(termList, fmt.Sprintf("%d ay", t))
	}

	phaseNote := ""
	if unlockPhase == 1 {
		phaseNote = " (phase 1 — daha genis araliq ucun once 1 kredit baglayin)"
	}

	return fmt.Sprintf("Desteklenen araliq: %.0f-%.0f AZN. Desteklenen muddeetler: %s.%s", minAmt, maxAmt, strings.Join(termList, ", "), phaseNote)
}

// ProcessApplication runs the full credit decision pipeline for a loan application.
//
// Pipeline steps:
//  1. Fetch application and customer loans from LMS
//  2. Run active-loan and payment-history checks in parallel
//  3. Determine credit level based on loan history
//  4. Save all check results
//  5. Make approve/reject decision
//  6. Update application record
func (e *CreditEngine) ProcessApplication(ctx context.Context, appID int) error {
	// Step 0: Update status to "checking"
	if err := e.appRepo.UpdateApplicationStatus(ctx, appID, "checking"); err != nil {
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

	// Step 3: Run checks in parallel (active loan check + payment history check)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var checks []model.ApplicationCheckResult

	// Pre-compute loan analytics needed by multiple checks
	analytics := computeAnalytics(customerLoans.Loans)

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

	// Step 4: Determine credit level (LMS history + AKB score override)
	creditLevel := determineCreditLevel(analytics, app.AkbScore)

	// Step 4b: Determine unlock phase — how many approved loans at this level?
	// phase 1 = first loan at this level (limited ranges), phase 2 = 1+ approved (full ranges)
	unlockPhase := 1
	approvedCount, err := e.appRepo.CountApprovedAtLevel(ctx, app.CustomerPIN, creditLevel)
	if err != nil {
		return fmt.Errorf("failed to count approved loans at level: %w", err)
	}
	if approvedCount >= 1 {
		unlockPhase = 2
	}

	akbNote := ""
	if app.AkbScore >= 700 {
		akbNote = " (AKB override: 700+ -> valuable)"
	}

	creditCheck := model.ApplicationCheckResult{
		CheckType: "credit_level_check",
		Status:    "passed",
		Detail:    fmt.Sprintf("Credit level: %s, unlock_phase: %d (%d previous approved loan(s) at this level)%s", creditLevel, unlockPhase, approvedCount, akbNote),
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	checks = append(checks, creditCheck)

	// Step 5: Save all check results
	for i := range checks {
		if err := e.appRepo.SaveCheckResult(ctx, appID, &checks[i]); err != nil {
			return fmt.Errorf("failed to save check result %s: %w", checks[i].CheckType, err)
		}
	}

	// Step 6: Make decision
	if analytics.hasActive {
		// Reject: customer has active loans
		err = e.appRepo.UpdateApplicationDecision(ctx, appID,
			"rejected", creditLevel, "Customer has active loans", 0, 0)
		if err != nil {
			return fmt.Errorf("failed to update rejection for active loans: %w", err)
		}
		return nil
	}

	if analytics.completedCount > 0 && !analytics.allOnTime {
		// Reject: late payments in history
		err = e.appRepo.UpdateApplicationDecision(ctx, appID,
			"rejected", creditLevel, "Late payments found in loan history", 0, 0)
		if err != nil {
			return fmt.Errorf("failed to update rejection for late payments: %w", err)
		}
		return nil
	}

	// Step 7: Approve — look up the rate for this credit level, amount, term, and unlock phase
	rate, err := e.appRepo.GetCreditLevelRate(ctx, creditLevel, app.Amount, app.TermMonths, unlockPhase)
	if err != nil {
		// No rate configured — build a descriptive rejection reason
		ranges, rngErr := e.appRepo.GetLevelRanges(ctx, creditLevel, unlockPhase)
		reason := fmt.Sprintf("No applicable rate: %v", err)
		if rngErr == nil {
			reason = fmt.Sprintf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s", app.Amount, app.TermMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
		}
		err = e.appRepo.UpdateApplicationDecision(ctx, appID,
			"rejected", creditLevel, reason, 0, 0)
		if err != nil {
			return fmt.Errorf("failed to update rejection for missing rate: %w", err)
		}
		return nil
	}

	// Step 7a: Determine if this level requires manual approval
	// Elite: fully automated approval
	// New/Trusted/Valuable: pending_approval (manual review required)
	if creditLevel == "elite" {
		// Elite: auto-approve immediately
		err = e.appRepo.UpdateApplicationDecision(ctx, appID,
			"approved", creditLevel, "", app.Amount, rate)
		if err != nil {
			return fmt.Errorf("failed to update approval: %w", err)
		}

		// Save credit level history
		if histErr := e.appRepo.SaveCreditLevelHistory(ctx, app.CustomerPIN, creditLevel, appID); histErr != nil {
			_ = histErr
		}
	} else {
		// New/Trusted/Valuable: set to pending_approval with proposed amount and rate
		err = e.appRepo.UpdateApplicationDecision(ctx, appID,
			"pending_approval", creditLevel, "", app.Amount, rate)
		if err != nil {
			return fmt.Errorf("failed to set pending_approval: %w", err)
		}
	}

	return nil
}

// computeAnalytics extracts loan history metrics from a list of LW loan records.
func computeAnalytics(loans []lw.CustomerLoan) *loanAnalytics {
	a := &loanAnalytics{allOnTime: true}
	for _, loan := range loans {
		if loan.Status == "active" {
			a.hasActive = true
		}
		if loan.Status == "completed" || loan.Status == "closed" {
			a.completedCount++
			if !loan.WasOnTime {
				a.allOnTime = false
			}
			if loan.EarlyCompletion {
				a.hasEarly = true
			}
		}
	}
	return a
}

// determineCreditLevel maps loan history analytics + AKB score to a credit level.
//
// Rules:
//   - AKB score 700+: overrides to "valuable" regardless of LW history
//   - New:     No completed loans (and AKB < 700)
//   - Trusted: 1+ completed loans, all on time (and AKB < 700)
//   - Valuable: 2+ completed loans, all on time (and AKB < 700)
//   - Elite:   2+ completed loans, all on time, at least 1 early completion (and AKB < 700)
func determineCreditLevel(a *loanAnalytics, akbScore int) string {
	// AKB 700+ override: directly assign valuable level
	if akbScore >= 700 {
		return "valuable"
	}

	if a.completedCount == 0 {
		return "new"
	}
	if a.completedCount >= 2 && a.allOnTime && a.hasEarly {
		return "elite"
	}
	if a.completedCount >= 2 && a.allOnTime {
		return "valuable"
	}
	if a.completedCount >= 1 && a.allOnTime {
		return "trusted"
	}
	// Has completed loans but with late payments — stays at new
	return "new"
}
