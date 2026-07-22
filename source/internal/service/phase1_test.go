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
        if call.Amount != 536.99 {
                t.Errorf("ApproveLoan Amount = %v, want 536.99 (500 principal + (27/73)*100 interest)", call.Amount)
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

// --- PR #51 tests: new rejection rules ---

// TestProcessApplication_AkbScoreBelow200 verifies that an AKB score below 200
// causes rejection (PR #51, rule 1).
func TestProcessApplication_AkbScoreBelow200(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // LW returns AKB score 150 — below 200 threshold.
        provider.akbScore = &lw.AkbScoreResponse{Fin: "PIN1", Score: 150}

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
        if !contains(du.RejectionReason, "AKB score 150") {
                t.Errorf("rejection reason = %q, want AKB score 150 below minimum", du.RejectionReason)
        }

        // Verify the akb_score_check was saved with failed status.
        var akbCheck *model.ApplicationCheckResult
        for i, cs := range store.checkSaves {
                if cs.CheckType == "akb_score_check" {
                        akbCheck = &store.checkSaves[i]
                        break
                }
        }
        if akbCheck == nil {
                t.Fatal("expected akb_score_check to be saved, got none")
        }
        if akbCheck.Status != model.CheckStatusFailed {
                t.Errorf("akb_score_check status = %q, want failed", akbCheck.Status)
        }
}

// TestProcessApplication_AkbScoreZeroNoRejection verifies that an AKB score of
// 0 (no AKB override from LW) does NOT cause rejection — fail-soft.
func TestProcessApplication_AkbScoreZeroNoRejection(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0
        store.currentLevel = ""

        provider := newMockLWProvider()
        // LW returns AKB score 0 — fallback to request value (400).
        provider.akbScore = &lw.AkbScoreResponse{Fin: "PIN1", Score: 0}

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusPendingApproval {
                t.Errorf("status = %q, want pending_approval (no AKB override = no rejection)", du.Status)
        }
}

// TestProcessApplication_AkbStopFactorRejection verifies that any non-empty
// AKB stop factor list causes rejection (PR #51, rule 4).
func TestProcessApplication_AkbStopFactorRejection(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 700,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // High AKB score BUT stop factors present — must still reject.
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:         "PIN1",
                Score:       750,
                StopFactors: []string{"AB", "TY"},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (stop factors override score)", du.Status)
        }
        if !contains(du.RejectionReason, "AKB stop factor") {
                t.Errorf("rejection reason = %q, want stop factor message", du.RejectionReason)
        }
        if !contains(du.RejectionReason, "AB") || !contains(du.RejectionReason, "TY") {
                t.Errorf("rejection reason = %q, want both codes AB and TY listed", du.RejectionReason)
        }

        // Verify the akb_stop_factor_check was saved with failed status.
        var stopCheck *model.ApplicationCheckResult
        for i, cs := range store.checkSaves {
                if cs.CheckType == "akb_stop_factor_check" {
                        stopCheck = &store.checkSaves[i]
                        break
                }
        }
        if stopCheck == nil {
                t.Fatal("expected akb_stop_factor_check to be saved, got none")
        }
        if stopCheck.Status != model.CheckStatusFailed {
                t.Errorf("akb_stop_factor_check status = %q, want failed", stopCheck.Status)
        }
}

// TestProcessApplication_AgeOver69 verifies that a customer older than 69 is
// rejected (PR #51, rule 3).
func TestProcessApplication_AgeOver69(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // DOB 1950-01-01 → age ~76 → reject.
        provider.personalInfo = &lw.PersonalInfoResponse{
                Fin:         "PIN1",
                FullName:    "Old Customer",
                DateOfBirth: "1950-01-01",
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (age > 69)", du.Status)
        }
        if !contains(du.RejectionReason, "age") || !contains(du.RejectionReason, "exceeds maximum") {
                t.Errorf("rejection reason = %q, want age exceeds maximum message", du.RejectionReason)
        }

        // Verify the age_check was saved with failed status.
        var ageCheck *model.ApplicationCheckResult
        for i, cs := range store.checkSaves {
                if cs.CheckType == "age_check" {
                        ageCheck = &store.checkSaves[i]
                        break
                }
        }
        if ageCheck == nil {
                t.Fatal("expected age_check to be saved, got none")
        }
        if ageCheck.Status != model.CheckStatusFailed {
                t.Errorf("age_check status = %q, want failed", ageCheck.Status)
        }
}

// TestProcessApplication_AgeExactly69Allowed verifies that age 69 is allowed
// (boundary: age > 69 is the rejection threshold, so 69 should pass).
func TestProcessApplication_AgeExactly69Allowed(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0
        store.currentLevel = ""

        provider := newMockLWProvider()
        // Calculate a DOB that gives exactly age 69 at test time.
        // We'll use 1957-01-01 which gives age 69 in mid-2026.
        provider.personalInfo = &lw.PersonalInfoResponse{
                Fin:         "PIN1",
                FullName:    "Senior Customer",
                DateOfBirth: "1957-01-01",
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        // age 69 is NOT > 69, so should not reject on age.
        if du.Status == model.StatusRejected && contains(du.RejectionReason, "age") {
                t.Errorf("age 69 should be allowed, but got rejection: %q", du.RejectionReason)
        }
}

// TestProcessApplication_PersonalInfoFailsNoAgeRejection verifies that when
// GetPersonalInfo fails (LW error), the age is unknown (0) and the application
// is NOT rejected on age grounds — fail-soft.
func TestProcessApplication_PersonalInfoFailsNoAgeRejection(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0
        store.currentLevel = ""

        provider := newMockLWProvider()
        provider.personalInfoErr = errors.New("LW unreachable")

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        // Should NOT reject on age when age is unknown.
        if du.Status == model.StatusRejected && contains(du.RejectionReason, "age") {
                t.Errorf("age unknown should not cause rejection, got: %q", du.RejectionReason)
        }

        // Verify the age_check was saved with passed status (fail-soft).
        var ageCheck *model.ApplicationCheckResult
        for i, cs := range store.checkSaves {
                if cs.CheckType == "age_check" {
                        ageCheck = &store.checkSaves[i]
                        break
                }
        }
        if ageCheck == nil {
                t.Fatal("expected age_check to be saved, got none")
        }
        if ageCheck.Status != model.CheckStatusPassed {
                t.Errorf("age_check status = %q, want passed (fail-soft on unknown age)", ageCheck.Status)
        }
}

// --- PR #52 tests: AKB History-based rejection rules ---

// helper: build an AKB liability with a monthly history of overdue days.
// `history` is a map of "YYYY-MM" -> overdueDays. The current liability state
// (DaysMainSumOverdue, MonthlyPaymentAmount, CreditStatus) is set from the
// other arguments.
func akbLiabilityWithHistory(id, status string, currentOverdue int, monthly float64, history map[string]int) lw.AkbLiability {
        lib := lw.AkbLiability{
                ID:                  id,
                CreditStatus:        status,
                DaysMainSumOverdue:  currentOverdue,
                MonthlyPaymentAmount: monthly,
        }
        for period, days := range history {
                lib.History = append(lib.History, lw.AkbLiabilityHistory{
                        ReportingPeriod: period,
                        OverdueDays:     days,
                })
        }
        return lib
}

// TestProcessApplication_DelayRatioAbove6 verifies rule 2: a delay ratio > 6
// (sum of overdue days in last 24 months / active months) triggers rejection.
//
// Setup: 12 active months, 90 overdue days → ratio 7.5 → reject.
func TestProcessApplication_DelayRatioAbove6(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // Build 12 reporting periods (last 12 months) with total 90 overdue days.
        // 90 / 12 = 7.5 > 6 → reject.
        history := map[string]int{}
        now := time.Now()
        for i := 0; i < 12; i++ {
                period := now.AddDate(0, -i, 0).Format("2006-01")
                history[period] = 7 // 7 days each → 84 total, ~7.0 ratio
        }
        history[now.AddDate(0, -1, 0).Format("2006-01")] = 13 // bump last month to 13 → total 90, ratio 7.5

        provider.akbHistory = &lw.AkbHistoryResponse{
                ReportID:      "MOCK",
                ReportingDate: "2026-01-01",
                Liabilities:   []lw.AkbLiability{akbLiabilityWithHistory("L1", "closed", 0, 0, history)},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (delay ratio > 6)", du.Status)
        }
        if !contains(du.RejectionReason, "Delay ratio") || !contains(du.RejectionReason, "exceeds maximum") {
                t.Errorf("rejection reason = %q, want delay ratio exceeds maximum", du.RejectionReason)
        }
}

// TestProcessApplication_ActiveLoanCurrentDelayAbove5 verifies rule 6: an
// active liability with DaysMainSumOverdue > 5 triggers rejection.
func TestProcessApplication_ActiveLoanCurrentDelayAbove5(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{
                        akbLiabilityWithHistory("L1", "active", 10, 200, nil), // 10 > 5 → reject
                },
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (active delay > 5)", du.Status)
        }
        if !contains(du.RejectionReason, "Active loan has 10 days") {
                t.Errorf("rejection reason = %q, want active loan 10 days overdue", du.RejectionReason)
        }
}

// TestProcessApplication_Delay3MonthsAbove20 verifies rule 7: any single
// reporting period in the last 3 months with OverdueDays >= 20 triggers rejection.
func TestProcessApplication_Delay3MonthsAbove20(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // One month ago, 25 days overdue — within 3-month window, ≥ 20 → reject.
        lastMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
        history := map[string]int{lastMonth: 25}
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{akbLiabilityWithHistory("L1", "closed", 0, 0, history)},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (3-month delay ≥ 20)", du.Status)
        }
        if !contains(du.RejectionReason, "last 3 months") {
                t.Errorf("rejection reason = %q, want last 3 months", du.RejectionReason)
        }
}

// TestProcessApplication_Delay6MonthsAbove30 verifies rule 8.
func TestProcessApplication_Delay6MonthsAbove30(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // 4 months ago, 35 days overdue — within 6-month window (but outside 3-month),
        // ≥ 30 → reject on 6-month rule.
        fourMonthsAgo := time.Now().AddDate(0, -4, 0).Format("2006-01")
        history := map[string]int{fourMonthsAgo: 35}
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{akbLiabilityWithHistory("L1", "closed", 0, 0, history)},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (6-month delay ≥ 30)", du.Status)
        }
        if !contains(du.RejectionReason, "last 6 months") {
                t.Errorf("rejection reason = %q, want last 6 months", du.RejectionReason)
        }
}

// TestProcessApplication_Delay12MonthsAbove45 verifies rule 9.
func TestProcessApplication_Delay12MonthsAbove45(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // 8 months ago, 50 days overdue — within 12-month window, ≥ 45 → reject.
        eightMonthsAgo := time.Now().AddDate(0, -8, 0).Format("2006-01")
        history := map[string]int{eightMonthsAgo: 50}
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{akbLiabilityWithHistory("L1", "closed", 0, 0, history)},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (12-month delay ≥ 45)", du.Status)
        }
        if !contains(du.RejectionReason, "last 12 months") {
                t.Errorf("rejection reason = %q, want last 12 months", du.RejectionReason)
        }
}

// TestProcessApplication_Delay18MonthsAbove60 verifies rule 10.
func TestProcessApplication_Delay18MonthsAbove60(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // 14 months ago, 65 days overdue — within 18-month window (but outside 12-month),
        // ≥ 60 → reject on 18-month rule.
        fourteenMonthsAgo := time.Now().AddDate(0, -14, 0).Format("2006-01")
        history := map[string]int{fourteenMonthsAgo: 65}
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{akbLiabilityWithHistory("L1", "closed", 0, 0, history)},
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (18-month delay ≥ 60)", du.Status)
        }
        if !contains(du.RejectionReason, "last 18 months") {
                t.Errorf("rejection reason = %q, want last 18 months", du.RejectionReason)
        }
}

// TestProcessApplication_MonthlyPaymentsAbove2000 verifies rule 12: total
// monthly payments on active liabilities > 2000 AZN triggers rejection.
func TestProcessApplication_MonthlyPaymentsAbove2000(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0

        provider := newMockLWProvider()
        // Two active liabilities: 1200 + 900 = 2100 > 2000 → reject.
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{
                        akbLiabilityWithHistory("L1", "active", 0, 1200, nil),
                        akbLiabilityWithHistory("L2", "active", 0, 900, nil),
                },
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (monthly payments > 2000)", du.Status)
        }
        if !contains(du.RejectionReason, "Total monthly payments") || !contains(du.RejectionReason, "2100") {
                t.Errorf("rejection reason = %q, want total monthly payments 2100", du.RejectionReason)
        }
}

// TestProcessApplication_AkbHistoryUnavailableFailSoft verifies that when
// GetAkbHistory returns an error, the 7 AKB-History-based rules are all
// skipped (fail-soft) and the application proceeds normally.
func TestProcessApplication_AkbHistoryUnavailableFailSoft(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0
        store.currentLevel = ""

        provider := newMockLWProvider()
        provider.akbHistoryErr = errors.New("LW unreachable")

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        // Should NOT reject on any AKB-History rule when history is unavailable.
        if du.Status == model.StatusRejected {
                t.Errorf("status = rejected (%q), want non-rejected (fail-soft on AKB history error)", du.RejectionReason)
        }

        // All 7 AKB-History checks should be saved with passed status + fail-soft note.
        akbHistoryCheckTypes := []string{
                "delay_ratio_check", "active_delay_check",
                "delay_history_3m_check", "delay_history_6m_check",
                "delay_history_12m_check", "delay_history_18m_check",
                "monthly_payments_check",
        }
        for _, ct := range akbHistoryCheckTypes {
                var found *model.ApplicationCheckResult
                for i, cs := range store.checkSaves {
                        if cs.CheckType == ct {
                                found = &store.checkSaves[i]
                                break
                        }
                }
                if found == nil {
                        t.Errorf("expected check %q to be saved (fail-soft), got none", ct)
                        continue
                }
                if found.Status != model.CheckStatusPassed {
                        t.Errorf("check %q status = %q, want passed (fail-soft)", ct, found.Status)
                }
                if !contains(found.Detail, "fail-soft") {
                        t.Errorf("check %q detail = %q, want fail-soft note", ct, found.Detail)
                }
        }
}

// TestProcessApplication_AkbHistoryBelowThresholds verifies that an
// application with AKB history present but all metrics below thresholds
// does NOT get rejected on those rules.
func TestProcessApplication_AkbHistoryBelowThresholds(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.rate = 30.0
        store.approvedCount = 0
        store.currentLevel = ""

        provider := newMockLWProvider()
        // Small delay (1 day) in last month, small monthly payment (500) — below all thresholds.
        lastMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
        history := map[string]int{lastMonth: 1}
        provider.akbHistory = &lw.AkbHistoryResponse{
                Liabilities: []lw.AkbLiability{
                        akbLiabilityWithHistory("L1", "active", 1, 500, history),
                },
        }

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        du := store.decisionUpdates[0]
        if du.Status == model.StatusRejected && contains(du.RejectionReason, "AKB") {
                t.Errorf("should not reject on AKB history when all metrics below thresholds, got: %q", du.RejectionReason)
        }
}
