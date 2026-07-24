package service

import (
        "context"
        "errors"
        "testing"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
)

// TestPreValidate covers the synchronous pre-validation that runs before
// an application is created. The function should reject invalid amount/term
// combinations early (returning a descriptive error) and accept valid ones.
func TestPreValidate(t *testing.T) {
        ctx := context.Background()

        tests := []struct {
                name        string
                loans       []lw.CustomerLoan
                akbScore    int
                amount      float64
                termMonths  int
                commission  float64    // mock return from GetCreditLevelRate
                commissionErr error  // mock error from GetCreditLevelRate
                levelRanges []repository.LevelRange
                wantErr     bool
                errSubstr   string
        }{
                {
                        name:       "valid: new customer, 200 AZN / 3 months, commission found",
                        loans:      nil,
                        akbScore:   400,
                        amount:     200,
                        termMonths: 3,
                        commission:  14.0,
                        wantErr:    false,
                },
                {
                        name:        "invalid: commission not found, ranges available for descriptive error",
                        loans:       nil,
                        akbScore:    400,
                        amount:      9999, // out-of-range
                        termMonths:  3,
                        commissionErr: errors.New("no commission found"),
                        levelRanges: []repository.LevelRange{{MinAmount: 50, MaxAmount: 500, TermMonths: 3, Commission: 14.0, Phase: 1}},
                        wantErr:     true,
                        errSubstr:   "kecerli deyil",
                },
                {
                        name:       "invalid: commission not found AND ranges query fails — fallback error",
                        loans:      nil,
                        akbScore:   400,
                        amount:     9999,
                        termMonths: 3,
                        commissionErr: errors.New("no commission found"),
                        levelRanges: nil, // implicit: GetLevelRanges returns levelRangesErr
                        wantErr:    true,
                        errSubstr:  "is not valid",
                },
                {
                        name:       "AKB override to valuable, commission found",
                        loans:      nil,
                        akbScore:   750,
                        amount:     700,
                        termMonths: 3,
                        commission:  15.0,
                        wantErr:    false,
                },
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        store := newMockStore()
                        store.commission = tc.commission
                        store.rateErr = tc.commissionErr
                        if tc.levelRanges != nil {
                                store.levelRanges = tc.levelRanges
                        } else if tc.commissionErr != nil {
                                // Simulate ranges query failure when no ranges configured
                                store.levelRangesErr = errors.New("no ranges")
                        }
                        store.approvedCount = 0

                        provider := newMockLWProvider().withLoans(tc.loans)
                        engine := NewCreditEngine(provider, store)

                        err := engine.PreValidate(ctx, "PIN123", tc.amount, tc.termMonths, tc.akbScore)

                        if tc.wantErr {
                                if err == nil {
                                        t.Fatalf("PreValidate() expected error, got nil")
                                }
                                if tc.errSubstr != "" && !contains(err.Error(), tc.errSubstr) {
                                        t.Errorf("PreValidate() error = %q, want substring %q", err.Error(), tc.errSubstr)
                                }
                                return
                        }
                        if err != nil {
                                t.Fatalf("PreValidate() unexpected error: %v", err)
                        }
                })
        }
}

// TestProcessApplication_RejectActiveLoans verifies that an application is
// rejected when the customer has an active loan.
func TestProcessApplication_RejectActiveLoans(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID:          1,
                CustomerPIN: "PIN1",
                Amount:      200,
                TermMonths:  3,
                AkbScore:    400,
        }
        store.commission = 30.0
        store.approvedCount = 0

        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "active", WasOnTime: true}, // triggers rejection
        })

        engine := NewCreditEngine(provider, store)
        err := engine.ProcessApplication(ctx, 1)
        if err != nil {
                t.Fatalf("ProcessApplication() unexpected error: %v", err)
        }

        // Verify a rejection decision was recorded
        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision update, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("decision status = %q, want %q", du.Status, model.StatusRejected)
        }
        if du.RejectionReason != "Customer has active loans" {
                t.Errorf("rejection reason = %q, want active-loans reason", du.RejectionReason)
        }
}

// TestProcessApplication_RejectLatePayments verifies rejection when the
// customer has completed loans with late payments.
func TestProcessApplication_RejectLatePayments(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID:          1,
                CustomerPIN: "PIN1",
                Amount:      200,
                TermMonths:  3,
                AkbScore:    400,
        }
        store.commission = 30.0
        store.approvedCount = 0

        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "completed", WasOnTime: false, DelayDays: 5, TermMonths: 3, LevelAtClose: "new"}, // late payment
        })

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
        if !contains(du.RejectionReason, "Late payments") {
                t.Errorf("rejection reason = %q, want late-payments reason", du.RejectionReason)
        }
}

// TestProcessApplication_EliteAutoApprove verifies that elite-level customers
// (2+ completed on-time loans with 1+ early completion) are auto-approved
// and their credit-level history is saved.
func TestProcessApplication_EliteAutoApprove(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID:          1,
                CustomerPIN: "PIN1",
                Amount:      500,
                TermMonths:  6,
                AkbScore:    400, // AKB below 700, so level comes from loan history
        }
        store.commission = 27.0
        store.approvedCount = 1 // phase 2
        store.currentLevel = "valuable" // customer is at valuable level

        // 2 completed loans at "valuable" level, 0 delay, 2mo term → elite
        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 2, LevelAtClose: "valuable"},
                {Status: "completed", WasOnTime: true, DelayDays: 0, TermMonths: 3, LevelAtClose: "valuable"},
        })

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusApproved {
                t.Errorf("status = %q, want approved", du.Status)
        }
        if du.CreditLevel != model.CreditLevelElite {
                t.Errorf("credit level = %q, want elite", du.CreditLevel)
        }
        if du.ApprovedAmount != 500 {
                t.Errorf("approved amount = %v, want 500", du.ApprovedAmount)
        }
        if du.ApprovedRate != 27.0 {
                t.Errorf("approved rate = %v, want 27.0", du.ApprovedRate)
        }

        // Verify credit-level history was saved
        if len(store.historySaves) != 1 {
                t.Fatalf("expected 1 history save, got %d", len(store.historySaves))
        }
        if store.historySaves[0].ToLevel != model.CreditLevelElite {
                t.Errorf("history level = %q, want elite", store.historySaves[0].ToLevel)
        }
}

// TestProcessApplication_NewLevelPendingApproval verifies that new/trusted/
// valuable level applications go to pending_approval (manual review needed).
func TestProcessApplication_NewLevelPendingApproval(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID:          1,
                CustomerPIN: "PIN1",
                Amount:      200,
                TermMonths:  3,
                AkbScore:    400,
        }
        store.commission = 30.0
        store.approvedCount = 0

        // No completed loans → new level
        provider := newMockLWProvider().withLoans(nil)

        engine := NewCreditEngine(provider, store)
        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusPendingApproval {
                t.Errorf("status = %q, want pending_approval", du.Status)
        }
        if du.CreditLevel != model.CreditLevelNew {
                t.Errorf("credit level = %q, want new", du.CreditLevel)
        }

        // Verify NO credit-level history was saved (only saved on approval)
        if len(store.historySaves) != 0 {
                t.Errorf("expected 0 history saves for pending_approval, got %d", len(store.historySaves))
        }
}

// TestProcessApplication_StatusTransitionToChecking verifies the first
// pipeline step: the application status is updated to "checking" before any
// other work happens.
func TestProcessApplication_StatusTransitionToChecking(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.commission = 30.0

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(store.statusUpdates) == 0 {
                t.Fatal("expected at least 1 status update (→ checking)")
        }
        if store.statusUpdates[0].Status != model.StatusChecking {
                t.Errorf("first status update = %q, want %q",
                        store.statusUpdates[0].Status, model.StatusChecking)
        }
}

// TestProcessApplication_ChecksSaved verifies that all check results are
// persisted to the store.
func TestProcessApplication_ChecksSaved(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Amount: 200, TermMonths: 3, AkbScore: 400,
        }
        store.commission = 30.0

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        if err := engine.ProcessApplication(ctx, 1); err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Expect 4 checks saved: active-loan, payment-history, credit-level, blacklist
        if len(store.checkSaves) != 4 {
                t.Fatalf("expected 4 check saves, got %d", len(store.checkSaves))
        }

        checkTypes := map[string]bool{
                "lms_active_loan_check":     false,
                "lms_payment_history_check": false,
                "credit_level_check":        false,
                "blacklist_check":           false,
        }
        for _, cs := range store.checkSaves {
                if _, ok := checkTypes[cs.CheckType]; !ok {
                        t.Errorf("unexpected check type: %s", cs.CheckType)
                }
                checkTypes[cs.CheckType] = true
        }
        for ct, found := range checkTypes {
                if !found {
                        t.Errorf("check type %q was not saved", ct)
                }
        }
}

// TestProcessApplication_AppNotFound verifies that a missing application ID
// produces an error (not a panic).
func TestProcessApplication_AppNotFound(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        // appByID is empty → GetApplicationByID returns errNotFound
        store.commission = 30.0

        provider := newMockLWProvider()
        engine := NewCreditEngine(provider, store)

        err := engine.ProcessApplication(ctx, 999)
        if err == nil {
                t.Fatal("expected error for non-existent application, got nil")
        }
}
