package service

import (
        "context"
        "fmt"
        "log/slog"

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
}

// computeDecision evaluates the analytics, blacklist, credit level, and rate
// lookup to produce a final decision. Returns a decisionResult that the caller
// applies via applyDecisionTx.
//
// Decision order (first match wins):
//  1. Blacklisted       → reject (T-1.5)
//  2. Active loan       → reject
//  3. Late payments     → reject
//  4. No applicable rate → reject (with descriptive reason)
//  5. Elite level       → approve (auto)
//  6. Other levels      → pending_approval (manual review)
//
// This function does NOT write to the DB — it's a pure decision computation
// so it's easy to test in isolation.
func (e *CreditEngine) computeDecision(analytics *loanAnalytics, creditLevel string,
        unlockPhase int, app *model.LoanApplication, blacklisted bool) (*decisionResult, error) {

        // 1. Reject: blacklisted (T-1.5)
        if blacklisted {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "Customer is blacklisted",
                }, nil
        }

        // 2. Reject: active loans
        if analytics.hasActive {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "Customer has active loans",
                }, nil
        }

        // 3. Reject: late payments in history
        if analytics.completedCount > 0 && !analytics.allOnTime {
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: "Late payments found in loan history",
                }, nil
        }

        // 4. Look up the rate for this credit level, amount, term, and unlock phase
        rate, err := e.appRepo.GetCreditLevelRate(context.Background(), creditLevel, app.Amount, app.TermMonths, unlockPhase)
        if err != nil {
                reason := fmt.Sprintf("No applicable rate: %v", err)
                if ranges, rngErr := e.appRepo.GetLevelRanges(context.Background(), creditLevel, unlockPhase); rngErr == nil {
                        reason = fmt.Sprintf("mebleg %.0f AZN, %d ay '%s' level ucun kecerli deyil (phase %d). %s",
                                app.Amount, app.TermMonths, creditLevel, unlockPhase, buildRangeSummary(ranges, unlockPhase))
                }
                return &decisionResult{
                        Status:          model.StatusRejected,
                        RejectionReason: reason,
                }, nil
        }

        // 5. Elite: auto-approve; 6. Others: pending_approval (manual review)
        if creditLevel == model.CreditLevelElite {
                return &decisionResult{
                        Status:         model.StatusApproved,
                        ApprovedAmount: app.Amount,
                        ApprovedRate:   rate,
                }, nil
        }

        // New / Trusted / Valuable: pending_approval with proposed amount and rate
        return &decisionResult{
                Status:         model.StatusPendingApproval,
                ApprovedAmount: app.Amount,
                ApprovedRate:   rate,
        }, nil
}

// applyDecisionTx writes the decision to the DB inside the given transaction.
// For approved applications, it also saves the credit-level history (best-effort
// — a failure here is logged but does not roll back the tx).
func (e *CreditEngine) applyDecisionTx(ctx context.Context, runner repository.TxRunner,
        appID int, app *model.LoanApplication, creditLevel string, decision *decisionResult) error {

        if err := e.appRepo.UpdateApplicationDecisionTx(ctx, runner, appID,
                decision.Status, creditLevel, decision.RejectionReason,
                decision.ApprovedAmount, decision.ApprovedRate); err != nil {
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
                Amount:        decision.ApprovedAmount,
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
