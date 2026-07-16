package model

// Application status constants. Use these typed constants instead of magic strings
// ("approved", "rejected", etc.) so the compiler catches typos.
//
// Lifecycle:
//
//      pending → checking → approved | rejected | pending_approval
//                                        ↑            ↓
//                                        └── approved / rejected (operator decision)
const (
        // StatusPending means the application was just created and has not been picked
        // up by the credit engine yet.
        StatusPending = "pending"

        // StatusChecking means the credit engine is currently running the pipeline.
        StatusChecking = "checking"

        // StatusPendingApproval means all automated checks passed but the credit level
        // (new / trusted / valuable) requires manual operator approval.
        StatusPendingApproval = "pending_approval"

        // StatusApproved means the application is fully approved (either auto-approved
        // for elite level, or manually approved by an operator).
        StatusApproved = "approved"

        // StatusRejected means the application was rejected — by the credit engine
        // (active loan, late payments, no rate) or by an operator.
        StatusRejected = "rejected"

        // StatusPendingCustomer means the customer initiated the application via the
        // public website but has not yet verified their phone via OTP.
        StatusPendingCustomer = "pending_customer"

        // StatusPendingExpert means the customer verified their phone (OTP passed),
        // and the application is waiting for an expert to fill in the remaining
        // details (loan amount, term, card, contacts, address) and trigger the
        // credit engine.
        StatusPendingExpert = "pending_expert"
)

// IsFinal reports whether the status is a terminal state.
func IsFinal(status string) bool {
        return status == StatusApproved || status == StatusRejected
}

// IsActive reports whether the status indicates the application is still being
// processed (not yet in a terminal state).
func IsActive(status string) bool {
        return status == StatusPending || status == StatusChecking ||
                status == StatusPendingApproval || status == StatusPendingCustomer ||
                status == StatusPendingExpert
}

// ApplicationCheckResult status constants.
const (
        // CheckStatusPassed means the check passed.
        CheckStatusPassed = "passed"

        // CheckStatusFailed means the check failed (the application should be rejected
        // based on this check alone).
        CheckStatusFailed = "failed"

        // CheckStatusPending means the check has not been executed yet.
        CheckStatusPending = "pending"
)

// Credit level name constants.
const (
        // CreditLevelNew is the starting level — no completed loan history.
        CreditLevelNew = "new"

        // CreditLevelTrusted means the customer has 1+ completed, on-time loan.
        CreditLevelTrusted = "trusted"

        // CreditLevelValuable means the customer has 2+ completed, on-time loans,
        // or an AKB score of 700+ (override).
        CreditLevelValuable = "valuable"

        // CreditLevelElite means the customer has 2+ completed, on-time loans with
        // at least one early completion. Auto-approved by the credit engine.
        CreditLevelElite = "elite"
)

// IsValidCreditLevel reports whether the given string is one of the four
// supported credit levels.
func IsValidCreditLevel(level string) bool {
        switch level {
        case CreditLevelNew, CreditLevelTrusted, CreditLevelValuable, CreditLevelElite:
                return true
        }
        return false
}
