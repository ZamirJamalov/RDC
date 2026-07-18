package service

import (
        "context"
        "errors"
        "testing"
        "time"

        "rdc-source/internal/model"
        "rdc-source/pkg/lw"
)

// TestProcessApplication_BlacklistRejection (T-1.5) verifies that a blacklisted
// customer is rejected even if all other checks would pass.
func TestProcessApplication_BlacklistRejection(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        provider.blacklisted = true // triggers blacklist rejection

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected", du.Status)
        }
        if !contains(du.RejectionReason, "blacklisted") {
                t.Errorf("rejection reason = %q, want blacklist message", du.RejectionReason)
        }

        // Verify the blacklist check was saved with failed status
        var blacklistCheck *model.ApplicationCheckResult
        for i, cs := range store.checkSaves {
                if cs.CheckType == "blacklist_check" {
                        blacklistCheck = &store.checkSaves[i]
                        break
                }
        }
        if blacklistCheck == nil {
                t.Fatal("blacklist_check was not saved")
        }
        if blacklistCheck.Status != model.CheckStatusFailed {
                t.Errorf("blacklist check status = %q, want %q",
                        blacklistCheck.Status, model.CheckStatusFailed)
        }
}

// TestProcessApplication_BlacklistCheckFailureIsFailOpen (T-1.5) verifies that
// when the blacklist check itself errors (e.g. LW unreachable), the engine
// treats the customer as NOT blacklisted and continues. This is fail-open
// behavior — we don't reject just because we can't reach the blacklist service.
func TestProcessApplication_BlacklistCheckFailureIsFailOpen(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        provider.blacklistErr = errors.New("LW unreachable")

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Should NOT be rejected for blacklist (fail-open)
        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status == model.StatusRejected && contains(du.RejectionReason, "blacklist") {
                t.Errorf("should not reject for blacklist when LW is unreachable, but got: %q",
                        du.RejectionReason)
        }
}

// TestProcessApplication_LWApproveLoanCalledOnApproval (T-1.1) verifies that
// when the engine approves an application (elite level), it calls
// lwProvider.ApproveLoan to push the decision to LW.
func TestProcessApplication_LWApproveLoanCalledOnApproval(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 500, TermMonths: 6, AkbScore: 400,
        }
        store.rate = 27.0
        store.approvedCount = 1 // phase 2
        store.currentLevel = "valuable" // customer is at valuable level

        // 2 completed loans at "valuable" level, 0 delay, 2mo term → elite → auto-approve
        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 2, LevelAtClose: "valuable"},
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 3, LevelAtClose: "valuable"},
        })

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Verify ApproveLoan was called
        if len(provider.approveLoanCalls) != 1 {
                t.Fatalf("expected 1 ApproveLoan call, got %d", len(provider.approveLoanCalls))
        }
        call := provider.approveLoanCalls[0]
        if call.ApplicationID != 1 {
                t.Errorf("ApproveLoan ApplicationID = %d, want 1", call.ApplicationID)
        }
        // Amount sent to LW = Total (principal + interest)
        // 500 × (1 + 27/(100-27)) = 500 × (100/73) = 684.93
        if call.Amount != 684.93 {
                t.Errorf("ApproveLoan Amount (total) = %v, want 684.93", call.Amount)
        }
        if call.CreditLevel != model.CreditLevelElite {
                t.Errorf("ApproveLoan CreditLevel = %q, want elite", call.CreditLevel)
        }
        if call.TermMonths != 6 {
                t.Errorf("ApproveLoan TermMonths = %d, want 6", call.TermMonths)
        }
}

// TestProcessApplication_LWApproveLoanFailureDowngradesToRejected (T-1.1)
// verifies that when LW.ApproveLoan returns an error, the application is
// downgraded from approved to rejected with a descriptive reason.
func TestProcessApplication_LWApproveLoanFailureDowngradesToRejected(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 500, TermMonths: 6, AkbScore: 400,
        }
        store.rate = 27.0
        store.approvedCount = 1
        store.currentLevel = "valuable"

        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 2, LevelAtClose: "valuable"},
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 3, LevelAtClose: "valuable"},
        })
        provider.approveLoanErr = errors.New("LW contract signing failed")

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Expect TWO decision updates: first "approved" (in tx), then "rejected" (downgrade)
        if len(store.decisionUpdates) != 2 {
                t.Fatalf("expected 2 decision updates (approve then downgrade), got %d",
                        len(store.decisionUpdates))
        }

        first := store.decisionUpdates[0]
        if first.Status != model.StatusApproved {
                t.Errorf("first decision = %q, want approved", first.Status)
        }

        second := store.decisionUpdates[1]
        if second.Status != model.StatusRejected {
                t.Errorf("second decision = %q, want rejected (downgrade)", second.Status)
        }
        if !contains(second.RejectionReason, "LW approval failed") {
                t.Errorf("downgrade reason = %q, want LW-failed message", second.RejectionReason)
        }
}

// TestProcessApplication_LWApproveLoanNotCalledForRejection (T-1.1) verifies
// that ApproveLoan is NOT called when the application is rejected (only on approval).
func TestProcessApplication_LWApproveLoanNotCalledForRejection(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        // Active loan → rejected
        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "active", WasOnTime: true},
        })

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(provider.approveLoanCalls) != 0 {
                t.Errorf("expected 0 ApproveLoan calls for rejection, got %d",
                        len(provider.approveLoanCalls))
        }
}

// TestProcessApplication_LWApproveLoanNotCalledForPendingApproval (T-1.1)
// verifies that ApproveLoan is NOT called when the application goes to
// pending_approval (manual review). LW approval only happens after operator
// manually approves — which is a separate flow (UpdateStatus).
func TestProcessApplication_LWApproveLoanNotCalledForPendingApproval(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0

        // No loans → new level → pending_approval
        provider := newMockLWProvider()

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(provider.approveLoanCalls) != 0 {
                t.Errorf("expected 0 ApproveLoan calls for pending_approval, got %d",
                        len(provider.approveLoanCalls))
        }
}

// TestResolveAkbScore_LWOverride (T-1.6) verifies that when LW returns a
// non-zero AKB score, it overrides the request-supplied value.
func TestResolveAkbScore_LWOverride(t *testing.T) {
        ctx := context.Background()

        provider := newMockLWProvider()
        provider.akbScore = &lw.AkbScoreResponse{Score: 750, Fin: "PIN1"}

        engine := NewCreditEngine(provider, newMockStore())

        // Request supplied 400, but LW says 750 → 750 should win
        got := engine.resolveAkbScore(ctx, "PIN1", 400)
        if got != 750 {
                t.Errorf("resolveAkbScore() = %d, want 750 (LW override)", got)
        }
}

// TestResolveAkbScore_LWZeroFallbackToRequest (T-1.6) verifies that when LW
// returns score=0, the request-supplied value is used.
func TestResolveAkbScore_LWZeroFallbackToRequest(t *testing.T) {
        ctx := context.Background()

        provider := newMockLWProvider()
        provider.akbScore = &lw.AkbScoreResponse{Score: 0, Fin: "PIN1"} // LW has no score

        engine := NewCreditEngine(provider, newMockStore())

        got := engine.resolveAkbScore(ctx, "PIN1", 500)
        if got != 500 {
                t.Errorf("resolveAkbScore() = %d, want 500 (fallback)", got)
        }
}

// TestResolveAkbScore_LWErrorFallbackToRequest (T-1.6) verifies that when LW
// returns an error, the request-supplied value is used (fail-soft).
func TestResolveAkbScore_LWErrorFallbackToRequest(t *testing.T) {
        ctx := context.Background()

        provider := newMockLWProvider()
        provider.akbScoreErr = errors.New("LW unreachable")

        engine := NewCreditEngine(provider, newMockStore())

        got := engine.resolveAkbScore(ctx, "PIN1", 600)
        if got != 600 {
                t.Errorf("resolveAkbScore() = %d, want 600 (fallback on LW error)", got)
        }
}

// TestProcessApplication_AKBFromLWUsedForLevel (T-1.6) verifies end-to-end
// that the AKB score fetched from LW is used for credit-level determination.
// A customer with no loan history but AKB 750 (from LW) should be classified
// as "valuable" (AKB override).
func TestProcessApplication_AKBFromLWUsedForLevel(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 700, TermMonths: 3,
                AkbScore: 400, // request says 400, but LW will say 750
        }
        store.rate = 26.0 // valuable level rate
        store.approvedCount = 0

        // No loans → would be "new" with AKB 400. But LW says AKB 750 → "valuable".
        provider := newMockLWProvider()
        provider.akbScore = &lw.AkbScoreResponse{Score: 750, Fin: "PIN1"}

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.CreditLevel != model.CreditLevelValuable {
                t.Errorf("credit level = %q, want valuable (AKB override from LW)",
                        du.CreditLevel)
        }
}

// --- Transaction tests (T-1.3) ---

// TestProcessApplication_TransactionRollbackOnSaveCheckFailure (T-1.3)
// verifies that when SaveCheckResultTx fails inside the transaction, the
// whole pipeline returns an error (and the tx is rolled back — though the
// mock doesn't truly rollback, we verify the error propagation).
func TestProcessApplication_TransactionRollbackOnSaveCheckFailure(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.saveCheckErr = errors.New("DB connection lost")

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        err := engine.ProcessApplication(ctx, 1)
        if err == nil {
                t.Fatal("expected error when SaveCheckResultTx fails, got nil")
        }
        if !contains(err.Error(), "transaction rolled back") {
                t.Errorf("error = %q, want 'transaction rolled back' message", err.Error())
        }
}

// TestProcessApplication_TransactionRollbackOnDecisionFailure (T-1.3)
// verifies that when UpdateApplicationDecisionTx fails, the transaction is
// rolled back and the error is propagated.
func TestProcessApplication_TransactionRollbackOnDecisionFailure(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.updateDecisionErr = errors.New("DB write failed")

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        err := engine.ProcessApplication(ctx, 1)
        if err == nil {
                t.Fatal("expected error when UpdateApplicationDecisionTx fails, got nil")
        }
}

// TestProcessApplication_WithTxFailure (T-1.3) verifies that when WithTx
// itself returns an error (e.g. BEGIN TRANSACTION failed), the pipeline
// returns that error.
func TestProcessApplication_WithTxFailure(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.withTxErr = errors.New("BEGIN TRANSACTION failed")

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        err := engine.ProcessApplication(ctx, 1)
        if err == nil {
                t.Fatal("expected error when WithTx fails, got nil")
        }
        if !contains(err.Error(), "BEGIN TRANSACTION failed") {
                t.Errorf("error = %q, want WithTx error", err.Error())
        }
}

// --- Retry tests (T-1.2) ---

// TestBackoff verifies the exponential backoff calculation.
func TestBackoff(t *testing.T) {
        tests := []struct {
                name        string
                initial     time.Duration
                factor      float64
                attempt     int
                maxDelay    time.Duration
                wantDelay   time.Duration
        }{
                {"attempt 1, factor 2", 100 * time.Millisecond, 2.0, 1, 4 * time.Second, 100 * time.Millisecond},
                {"attempt 2, factor 2", 100 * time.Millisecond, 2.0, 2, 4 * time.Second, 200 * time.Millisecond},
                {"attempt 3, factor 2", 100 * time.Millisecond, 2.0, 3, 4 * time.Second, 400 * time.Millisecond},
                {"attempt 4, factor 2", 100 * time.Millisecond, 2.0, 4, 4 * time.Second, 800 * time.Millisecond},
                {"caps at maxDelay", 100 * time.Millisecond, 2.0, 10, 1 * time.Second, 1 * time.Second},
                {"factor 1.5", 200 * time.Millisecond, 1.5, 3, 4 * time.Second, 450 * time.Millisecond},
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        got := backoff(tc.initial, tc.factor, tc.attempt, tc.maxDelay)
                        if got != tc.wantDelay {
                                t.Errorf("backoff() = %v, want %v", got, tc.wantDelay)
                        }
                })
        }
}

// TestProcessApplicationWithRetry_SuccessOnFirstAttempt verifies that when
// ProcessApplication succeeds on the first try, no retries are attempted.
func TestProcessApplicationWithRetry_SuccessOnFirstAttempt(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        cfg := RetryConfig{
                MaxAttempts:   3,
                InitialDelay:  1 * time.Millisecond, // fast for tests
                MaxDelay:      10 * time.Millisecond,
                BackoffFactor: 2.0,
        }

        err := processApplicationWithRetry(ctx, engine, 1, cfg)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
}

// TestProcessApplicationWithRetry_SuccessAfterRetry verifies that when
// ProcessApplication fails on the first attempt but succeeds on the second,
// the function returns nil.
//
// We use a custom engine wrapper that fails the first call and succeeds on
// subsequent calls. This is more reliable than mutating mock state from a
// goroutine (which races with the retry loop).
func TestProcessApplicationWithRetry_SuccessAfterRetry(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        realEngine := NewCreditEngine(provider, store)

        // Wrap the engine so the first ProcessApplication call fails.
        wrapped := &flakyEngine{
                inner:       realEngine,
                failUntil:   1, // fail the first call only
                callCounter: 0,
        }

        cfg := RetryConfig{
                MaxAttempts:   3,
                InitialDelay:  1 * time.Millisecond,
                MaxDelay:      10 * time.Millisecond,
                BackoffFactor: 2.0,
        }

        err := processApplicationWithRetry(ctx, wrapped, 1, cfg)
        if err != nil {
                t.Fatalf("expected success after retry, got error: %v", err)
        }
        if wrapped.callCounter != 2 {
                t.Errorf("expected 2 calls (1 fail + 1 success), got %d", wrapped.callCounter)
        }
}

// flakyEngine wraps a CreditEngine and fails the first N calls to
// ProcessApplication. Used to test retry behavior without race conditions.
type flakyEngine struct {
        inner       *CreditEngine
        failUntil   int // fail calls 1..failUntil
        callCounter int
}

func (f *flakyEngine) ProcessApplication(ctx context.Context, appID int) error {
        f.callCounter++
        if f.callCounter <= f.failUntil {
                return errors.New("simulated transient failure")
        }
        return f.inner.ProcessApplication(ctx, appID)
}

// TestProcessApplicationWithRetry_AllAttemptsFail verifies that when all
// attempts fail, the last error is returned.
func TestProcessApplicationWithRetry_AllAttemptsFail(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        // appByID is empty → GetApplicationByID always fails → all attempts fail

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        cfg := RetryConfig{
                MaxAttempts:   2,
                InitialDelay:  1 * time.Millisecond,
                MaxDelay:      5 * time.Millisecond,
                BackoffFactor: 2.0,
        }

        err := processApplicationWithRetry(ctx, engine, 999, cfg)
        if err == nil {
                t.Fatal("expected error when all attempts fail, got nil")
        }
        if !contains(err.Error(), "application not found") {
                t.Errorf("error = %q, want 'not found' message", err.Error())
        }
}

// TestDefaultRetryConfig verifies the default retry config has sensible values.
func TestDefaultRetryConfig(t *testing.T) {
        cfg := DefaultRetryConfig()
        if cfg.MaxAttempts != 3 {
                t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
        }
        if cfg.InitialDelay != 500*time.Millisecond {
                t.Errorf("InitialDelay = %v, want 500ms", cfg.InitialDelay)
        }
        if cfg.MaxDelay != 4*time.Second {
                t.Errorf("MaxDelay = %v, want 4s", cfg.MaxDelay)
        }
        if cfg.BackoffFactor != 2.0 {
                t.Errorf("BackoffFactor = %v, want 2.0", cfg.BackoffFactor)
        }
}
