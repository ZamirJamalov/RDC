package service

import (
        "context"
        "fmt"
        "log/slog"
        "math"
        "strings"
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

// resolveAkbScoreAndStopFactors fetches the AKB score from LW and interprets
// the SOAP-derived JSON response per PR #55.
//
// Returns:
//   - score: the real AKB credit score (>1). Returns 0 when LW is unavailable
//     or when AKB signalled a stop factor (Point == 1) — in the latter case
//     hasStopFactor is true and the caller should reject on stop-factor grounds
//     rather than score threshold.
//   - stopFactorCode: the 2-letter stop factor code when hasStopFactor is true
//   - hasStopFactor: true when AKB returned Point == 1
//
// Fail-soft: on LW error or nil response, returns (fallback, "", false) —
// the caller uses the request-supplied fallback score and assumes no stop
// factor. AKB is an enhancement, not a hard dependency.
//
// The caller populates the loanAnalytics struct with these values before
// invoking computeDecision / runChecks.
func (e *CreditEngine) resolveAkbScoreAndStopFactors(ctx context.Context, customerPIN string, fallback int) (score int, stopFactorCode string, hasStopFactor bool) {
        resp, err := e.lwProvider.GetAkbScore(ctx, customerPIN, "")
        if err != nil {
                slog.Warn("failed to fetch AKB score from LW — using request fallback",
                        "customer_pin", customerPIN,
                        "fallback_score", fallback,
                        "error", err)
                return fallback, "", false
        }
        if resp == nil || resp.Return == nil {
                return fallback, "", false
        }

        // Per AKB semantics (PR #55):
        //   Point == 1 → stop factor present (Response holds the 2-letter code)
        //   Point >  1 → real credit score
        //   Point == 0 → AKB returned no useful data (treat as fail-soft)
        if resp.Return.Point == 1 {
                return 0, resp.Return.Response, true
        }
        if resp.Return.Point == 0 {
                return fallback, "", false
        }
        return resp.Return.Point, "", false
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

// resolveAkbHistory fetches the customer's full AKB credit history and computes
// the metrics required by PR #52 rejection rules.
//
// Populates the following loanAnalytics fields:
//   - delayRatio:            sum(OverdueDays in last 24 months) / active months
//   - activeMaxDelayDays:    max(DaysMainSumOverdue) across active liabilities
//   - maxDelayLast3Months:   max OverdueDays in last 3 months (>= 20 → reject)
//   - maxDelayLast6Months:   max OverdueDays in last 6 months (>= 30 → reject)
//   - maxDelayLast12Months:  max OverdueDays in last 12 months (>= 45 → reject)
//   - maxDelayLast18Months:  max OverdueDays in last 18 months (>= 60 → reject)
//   - totalMonthlyPayments:  sum(MonthlyPaymentAmount) across active liabilities
//   - akbHistoryAvailable:   true if AKB returned usable data
//
// Fail-soft: on LW error or nil response, akbHistoryAvailable is set to false
// and all derived fields are left at zero — the caller (computeDecision) skips
// AKB-History-based rules when akbHistoryAvailable is false.
//
// Time windows are computed from time.Now() (application date) going backwards,
// per business requirement: "kredit müraciəti etdiyi tarixden geri sayılır".
func (e *CreditEngine) resolveAkbHistory(ctx context.Context, customerPIN, serial string, analytics *loanAnalytics) {
        resp, err := e.lwProvider.GetAkbHistory(ctx, customerPIN, serial)
        if err != nil {
                slog.Warn("failed to fetch AKB history from LW — skipping AKB-history rules (fail-soft)",
                        "customer_pin", customerPIN,
                        "error", err)
                analytics.akbHistoryAvailable = false
                return
        }
        if resp == nil || len(resp.Liabilities) == 0 {
                // No liabilities = no history to evaluate. Treat as "available but empty"
                // so the caller knows the call succeeded. All metrics remain 0.
                analytics.akbHistoryAvailable = true
                return
        }

        now := time.Now()
        window3m := now.AddDate(0, -3, 0)
        window6m := now.AddDate(0, -6, 0)
        window12m := now.AddDate(0, -12, 0)
        window18m := now.AddDate(0, -18, 0)
        window24m := now.AddDate(0, -24, 0)

        var (
                totalDelay24m int
                activeMonths  int
                max3, max6, max12, max18 int
                totalMonthly   float64
                maxActiveDelay int
        )

        for _, lib := range resp.Liabilities {
                // Active liability metrics (rules 6 + 12)
                isActive := isAkbLiabilityActive(lib.CreditStatus)
                if isActive {
                        if lib.DaysMainSumOverdue > maxActiveDelay {
                                maxActiveDelay = lib.DaysMainSumOverdue
                        }
                        totalMonthly += lib.MonthlyPaymentAmount
                }

                // Per-month history metrics (rules 2, 7, 8, 9, 10)
                for _, h := range lib.History {
                        period, err := time.Parse("2006-01", h.ReportingPeriod)
                        if err != nil {
                                // Unparseable period — skip this entry rather than failing the whole call.
                                continue
                        }
                        // Active month = any month the liability had a reporting entry within last 24m.
                        if period.After(window24m) {
                                activeMonths++
                                totalDelay24m += h.OverdueDays
                        }
                        if period.After(window3m) && h.OverdueDays > max3 {
                                max3 = h.OverdueDays
                        }
                        if period.After(window6m) && h.OverdueDays > max6 {
                                max6 = h.OverdueDays
                        }
                        if period.After(window12m) && h.OverdueDays > max12 {
                                max12 = h.OverdueDays
                        }
                        if period.After(window18m) && h.OverdueDays > max18 {
                                max18 = h.OverdueDays
                        }
                }
        }

        if activeMonths > 0 {
                // Round to 2 decimals to avoid floating-point noise.
                ratio := float64(totalDelay24m) / float64(activeMonths)
                analytics.delayRatio = math.Round(ratio*100) / 100
        }
        analytics.activeMaxDelayDays = maxActiveDelay
        analytics.maxDelayLast3Months = max3
        analytics.maxDelayLast6Months = max6
        analytics.maxDelayLast12Months = max12
        analytics.maxDelayLast18Months = max18
        analytics.totalMonthlyPayments = math.Round(totalMonthly*100) / 100
        analytics.akbHistoryAvailable = true
}

// isAkbLiabilityActive returns true if the liability's credit status indicates
// the loan is currently active (not closed / written off / sold).
// AKB CreditStatus values: "active", "closed", "written_off", "sold",
// "court", "expired". We treat only "active" as active.
func isAkbLiabilityActive(status string) bool {
        return strings.EqualFold(status, "active")
}

// ProcessApplication runs the full credit decision pipeline for a loan application.
//
// Pipeline (DB writes wrapped in a single transaction — T-1.3):
//  1.  Status → checking (outside tx, visible immediately)
//  2.  Fetch application + customer loans from LW
//  3.  Resolve AKB score + stop factors from LW (PR #51)
//  3b. Resolve customer age from LW PersonalInfo (PR #51)
//  3c. Resolve AKB history metrics from LW (PR #52)
//  4.  Blacklist check from LW (T-1.5, fail-open on error)
//  4b. AZMK blacklist check from LW router (PR #53, fail-soft on error)
//  5.  Determine credit level + unlock phase
//  6.  Run checks (active-loan + payment-history + credit-level + blacklist + AZMK + AKB + age)
//  7.  Compute decision (credit_decision.go::computeDecision)
//  8.  Save checks + apply decision in transaction (T-1.3)
//  9.  If approved → call LW.ApproveLoan (T-1.1), downgrade to rejected on failure
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

        // Step 5: Resolve AKB score + stop factors from LW (T-1.6, PR #51, PR #55)
        resolvedAkb, stopFactorCode, hasStopFactor := e.resolveAkbScoreAndStopFactors(ctx, app.CustomerPIN, app.AkbScore)
        app.AkbScore = resolvedAkb
        analytics.akbScore = resolvedAkb
        analytics.akbStopFactorCode = stopFactorCode
        analytics.akbHasStopFactor = hasStopFactor

        // Step 5b: Resolve customer age from LW PersonalInfo (PR #51, rule 3)
        analytics.customerAge = e.resolveCustomerAge(ctx, app.CustomerPIN, app.CustomerSerial)

        // Step 5c: Resolve AKB credit history metrics (PR #52, rules 2/6/7/8/9/10/12)
        e.resolveAkbHistory(ctx, app.CustomerPIN, app.CustomerSerial, analytics)

        // Step 6: Blacklist check (T-1.5, fail-open)
        blacklisted, blacklistErr := e.lwProvider.CheckBlacklist(ctx, app.CustomerPIN)
        if blacklistErr != nil {
                slog.Warn("failed to check blacklist — treating as not blacklisted",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "error", blacklistErr)
                blacklisted = false
        }

        // Step 6b: AZMK blacklist check (PR #53, rule 5, fail-soft)
        azmkBlacklisted, azmkErr := e.lwProvider.GetAzmkBlacklist(ctx, app.CustomerPIN)
        if azmkErr != nil {
                slog.Warn("failed to check AZMK blacklist — skipping AZMK rule (fail-soft)",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "error", azmkErr)
                analytics.azmkCheckAvailable = false
        } else {
                analytics.azmkCheckAvailable = true
                analytics.azmkBlacklisted = azmkBlacklisted
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
