package service

import (
        "context"
        "fmt"
        "log/slog"
        "math"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
)

// decisionResult encapsulates the outcome of the credit decision pipeline.
// The caller (ProcessApplication) applies it to the DB via applyDecisionTx.
type decisionResult struct {
        Status          string
        RejectionReason string
        ApprovedAmount  float64
        ApprovedRate    float64
        TotalAmount     float64 // Principal + Interest (sent to LW)
}

// computeDecision evaluates the analytics, blacklist, credit level, and rate
// lookup to produce a final decision. Returns a decisionResult that the caller
// applies via applyDecisionTx.
//
// Decision order (first match wins):
//  1.  Blacklisted (LW)              → reject (T-1.5)
//  1b. AZMK blacklisted              → reject (PR #53, rule 5)
//  2.  AKB stop factors              → reject (PR #51, rule 4)
//  3.  AKB score < 200               → reject (PR #51, rule 1)
//  4.  Age > 69                      → reject (PR #51, rule 3)
//  4b. Delay ratio > 6 (24 months)   → reject (PR #52, rule 2)
//  4c. Active delay > 5 days         → reject (PR #52, rule 6)
//  4d. Last 3 months max ≥ 20        → reject (PR #52, rule 7)
//  4e. Last 6 months max ≥ 30        → reject (PR #52, rule 8)
//  4f. Last 12 months max ≥ 45       → reject (PR #52, rule 9)
//  4g. Last 18 months max ≥ 60       → reject (PR #52, rule 10)
//  4h. Monthly payments > 2000 AZN   → reject (PR #52, rule 12)
//  5.  Active loan                   → reject
//  6.  Late payments                 → reject
//  7.  No applicable rate            → reject (with descriptive reason)
//  8.  Elite level                   → approve (auto)
//  9.  Other levels                  → pending_approval (manual review)
//
// Rules 1b, 4b–4h are skipped when the corresponding LW call is unavailable
// (fail-soft). Rule 1b is skipped when azmkCheckAvailable is false.
func (e *CreditEngine) computeDecision(analytics *loanAnalytics, creditLevel string,
        unlockPhase int, app *model.LoanApplication, blacklisted bool) (*decisionResult, error) {

        // 1. Reject: blacklisted (T-1.5)
        if blacklisted {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "LW_BLACKLIST",
                }, nil
        }

        // 1b. Reject: AZMK blacklist (PR #53, rule 5)
        if analytics.azmkCheckAvailable && analytics.azmkBlacklisted {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "AZMK_BLACKLIST",
                }, nil
        }

        // 2. Reject: AKB stop factor present (PR #51, rule 4; PR #55 real format)
        // AKB signals a stop factor with Point == 1 and a 2-letter code in
        // <response>. Only one code is returned at a time (per business).
        if analytics.akbHasStopFactor {
                code := analytics.akbStopFactorCode
                if code == "" {
                        code = "unknown"
                }
                return &decisionResult{
                        Status: model.StatusRejected,
                        RejectionReason: fmt.Sprintf("AKB_STOP_FACTOR:%s", code),
                }, nil
        }

        // 3. Reject: AKB score below threshold (PR #51, rule 1)
        // Note: score == 0 means AKB didn't return a usable value (we fell back to
        // the request-supplied akbScore, which may also be 0). We only reject on a
        // genuine low score (> 0 and < 200) — a missing score is treated as
        // "no information" and does not block the application.
        // Score == 1 is already handled above as a stop factor.
        if analytics.akbScore > 0 && analytics.akbScore < 200 {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "AKB_SCORE_LOW",
                }, nil
        }

        // 4. Reject: customer age > 69 (PR #51, rule 3)
        // Note: customerAge == 0 means GetPersonalInfo failed or didn't return DOB;
        // we treat that as "unknown age" and do NOT reject (fail-soft).
        if analytics.customerAge > 69 {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "AGE_OVER_69",
                }, nil
        }

        // 4b-4h: AKB History-based rejections (PR #52).
        // All 7 rules are gated on analytics.akbHistoryAvailable — when AKB is
        // unreachable, the rules are skipped (fail-soft). Per business decision,
        // an unreachable AKB must not block the application.
        if analytics.akbHistoryAvailable {
                // 4b. Reject: delay ratio > 6 days/month over last 24 months (rule 2)
                if analytics.delayRatio > 6 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "DELAY_RATIO_HIGH",
                        }, nil
                }

                // 4c. Reject: active liability with current delay > 5 days (rule 6)
                if analytics.activeMaxDelayDays > 5 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "ACTIVE_DELAY_HIGH",
                        }, nil
                }

                // 4d. Reject: any single-month delay >= 20 in last 3 months (rule 7)
                if analytics.maxDelayLast3Months >= 20 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "DELAY_3M",
                        }, nil
                }

                // 4e. Reject: any single-month delay >= 30 in last 6 months (rule 8)
                if analytics.maxDelayLast6Months >= 30 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "DELAY_6M",
                        }, nil
                }

                // 4f. Reject: any single-month delay >= 45 in last 12 months (rule 9)
                if analytics.maxDelayLast12Months >= 45 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "DELAY_12M",
                        }, nil
                }

                // 4g. Reject: any single-month delay >= 60 in last 18 months (rule 10)
                if analytics.maxDelayLast18Months >= 60 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "DELAY_18M",
                        }, nil
                }

                // 4h. Reject: total monthly payments on active liabilities > 2000 AZN (rule 12)
                if analytics.totalMonthlyPayments > 2000 {
                        return &decisionResult{
                                Status: model.StatusRejected,
                                RejectionReason: "MONTHLY_PAYMENTS_HIGH",
                        }, nil
                }
        }

        // 5. Reject: active loans
        if analytics.hasActive {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "ACTIVE_LOAN",
                }, nil
        }

        // 6. Reject: late payments in history
        if analytics.completedCount > 0 && !analytics.allOnTime {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "LATE_PAYMENT",
                }, nil
        }

        // 7. Look up the rate for this credit level, amount, term, and unlock phase
        rate, err := e.appRepo.GetCreditLevelRate(context.Background(), creditLevel, app.Amount, app.TermMonths, unlockPhase)
        if err != nil {
                reason := "NO_COMMISSION_FOUND"
                if ranges, rngErr := e.appRepo.GetLevelRanges(context.Background(), creditLevel, unlockPhase); rngErr == nil {
                        reason = fmt.Sprintf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s",
                                app.Amount, app.TermMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
                }
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: reason,
                }, nil
        }

        // 8. Elite: auto-approve; 9. Others: pending_approval (manual review)
        // Calculate total amount sent to LW = Principal + Interest, where
        // Interest = (Rate / (100 - Rate)) × 100.
        // Example: 300 + (30/70)*100 = 300 + 42.86 = 342.86 AZN
        totalAmount := calculateTotalAmount(app.Amount, rate)

        if creditLevel == model.CreditLevelElite {
                return &decisionResult{
                        Status:         model.StatusApproved,
                        ApprovedAmount: app.Amount,
                        ApprovedRate:   rate,
                        TotalAmount:    totalAmount,
                }, nil
        }

        // New / Trusted / Valuable: pending_approval with proposed amount and rate
        return &decisionResult{
                Status:         model.StatusPendingApproval,
                ApprovedAmount: app.Amount,
                ApprovedRate:   rate,
                TotalAmount:    totalAmount,
        }, nil
}

// calculateTotalAmount returns the total credit amount = principal + commission.
//
// PR #86: 'commission' in credit_levels is the COMMISSION rate.
// Commission = principal × (commission / (100 - commission)) × 100
// Credit amount (total_amount in DB, sent to LW) = principal + commission
//
// Interest is separate: interest = principal × annual_interest_rate × (term / 12)
// Interest is NOT included in total_amount — it's shown to the customer
// in the summary panel but not sent to LW.
//
// Example: 300 AZN principal, commission=14:
//   commission_amount = 300 × (14/86) × 100 = 162.79
//   total = 300 + 162.79 = 462.79 AZN
func calculateTotalAmount(principal, commission float64) float64 {
        if commission <= 0 || commission >= 100 {
                return principal
        }
        commissionAmount := (commission / (100 - commission)) * 100
        total := principal + commissionAmount
        return math.Round(total*100) / 100
}

// applyDecisionTx writes the decision to the DB inside the given transaction.
// For approved applications, it also saves the credit-level history (best-effort
// — a failure here is logged but does not roll back the tx).
func (e *CreditEngine) applyDecisionTx(ctx context.Context, runner repository.TxRunner,
        appID int, app *model.LoanApplication, creditLevel string, decision *decisionResult) error {

        if err := e.appRepo.UpdateApplicationDecisionTx(ctx, runner, appID,
                decision.Status, creditLevel, decision.RejectionReason,
                decision.ApprovedAmount, decision.ApprovedRate, decision.TotalAmount); err != nil {
                return fmt.Errorf("failed to update decision: %w", err)
        }

        // Save credit-level history only on approval (same as before)
        if decision.Status == model.StatusApproved {
                if err := e.appRepo.SaveCreditLevelHistoryTx(ctx, runner, app.CustomerPIN, creditLevel, appID); err != nil {
                        // Best-effort: log and continue (don't fail the whole tx for this)
                        slog.Warn("failed to save credit level history (tx)",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "error", err)
                }
        }
        return nil
}

// notifyLwApproval calls LW.ApproveLoan to push the approved application to
// the LW system (T-1.1). Returns an error if LW rejects or is unreachable.
//
// The request includes the application ID, amount, credit level, term, and
// the customer's 16-digit card number (collected during application creation).
//
// The creditLevel is passed explicitly (rather than read from app.CreditLevel)
// because app is fetched at the start of the pipeline, before the decision
// writes the level to the DB. Using the parameter ensures we send the level
// that was actually decided, not a stale value.
func (e *CreditEngine) notifyLwApproval(ctx context.Context, app *model.LoanApplication, creditLevel string, decision *decisionResult) error {
        req := &lw.ApproveLoanRequest{
                ApplicationID: app.ID,
                Amount:        decision.TotalAmount, // Send total (principal + interest) to LW
                CardNumber:    app.CardNumber,
                CreditLevel:   creditLevel,
                TermMonths:    app.TermMonths,
        }
        resp, err := e.lwProvider.ApproveLoan(ctx, req)
        if err != nil {
                return fmt.Errorf("LW ApproveLoan call failed: %w", err)
        }
        slog.Info("LW approval succeeded",
                "application_id", app.ID,
                "contract_status", resp.ContractStatus,
                "transfer_status", resp.TransferStatus,
                "lms_loan_id", resp.LmsLoanID)
        return nil
}
