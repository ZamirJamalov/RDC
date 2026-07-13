package service

import (
        "context"
        "errors"
        "strings"
        "testing"
        "time"

        "rdc-source/internal/model"
        "rdc-source/pkg/lw"
)

// --- CreateApplication validation tests ---

// TestCreateApplication_Validation verifies that CreateApplication rejects
// invalid requests before touching the store.
func TestCreateApplication_Validation(t *testing.T) {
        ctx := context.Background()

        tests := []struct {
                name    string
                req     *model.CreateApplicationRequest
                wantErr string // substring expected in error
        }{
                {
                        name:    "missing customer_pin",
                        req:     &model.CreateApplicationRequest{CustomerFullName: "Ali", Amount: 200, TermMonths: 3},
                        wantErr: "customer_pin is required",
                },
                {
                        name:    "missing customer_full_name",
                        req:     &model.CreateApplicationRequest{CustomerPIN: "PIN1", Amount: 200, TermMonths: 3},
                        wantErr: "customer_full_name is required",
                },
                {
                        name:    "amount zero",
                        req:     &model.CreateApplicationRequest{CustomerPIN: "PIN1", CustomerFullName: "Ali", Amount: 0, TermMonths: 3},
                        wantErr: "amount must be greater than zero",
                },
                {
                        name:    "amount negative",
                        req:     &model.CreateApplicationRequest{CustomerPIN: "PIN1", CustomerFullName: "Ali", Amount: -100, TermMonths: 3},
                        wantErr: "amount must be greater than zero",
                },
                {
                        name:    "term_months zero",
                        req:     &model.CreateApplicationRequest{CustomerPIN: "PIN1", CustomerFullName: "Ali", Amount: 200, TermMonths: 0},
                        wantErr: "term_months must be greater than zero",
                },
                {
                        name:    "term_months negative",
                        req:     &model.CreateApplicationRequest{CustomerPIN: "PIN1", CustomerFullName: "Ali", Amount: 200, TermMonths: -1},
                        wantErr: "term_months must be greater than zero",
                },
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())
                        _, err := svc.CreateApplication(ctx, tc.req)
                        if err == nil {
                                t.Fatal("expected error, got nil")
                        }
                        if !contains(err.Error(), tc.wantErr) {
                                t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
                        }
                })
        }
}

// TestCreateApplication_DuplicatePending verifies that a customer with an
// existing non-final application cannot create a new one.
func TestCreateApplication_DuplicatePending(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.pendingAppID = 42
        store.pendingStatus = model.StatusChecking

        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        req := &model.CreateApplicationRequest{
                CustomerPIN: "PIN1", CustomerFullName: "Ali",
                Amount: 200, TermMonths: 3, CardNumber: "4111111111111111",
        }
        _, err := svc.CreateApplication(ctx, req)
        if err == nil {
                t.Fatal("expected duplicate-application error, got nil")
        }
        if !contains(err.Error(), "işlənməkdə olan") {
                t.Errorf("error = %q, want Azerbaijani duplicate message", err.Error())
        }
}

// TestCreateApplication_PreValidateFails verifies that when PreValidate
// returns an error (e.g. invalid amount/term for the customer's level),
// CreateApplication propagates the error and does NOT insert the application.
func TestCreateApplication_PreValidateFails(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.pendingAppID = 0 // no duplicate
        // Configure rate lookup to fail → PreValidate returns error
        store.rateErr = errors.New("no rate found")
        store.levelRangesErr = errors.New("no ranges")

        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), store), newMockCustomerStore())

        req := &model.CreateApplicationRequest{
                CustomerPIN: "PIN1", CustomerFullName: "Ali",
                Amount: 9999, TermMonths: 99, CardNumber: "4111111111111111",
        }
        _, err := svc.CreateApplication(ctx, req)
        if err == nil {
                t.Fatal("expected PreValidate error, got nil")
        }

        // Verify NO application was inserted
        if len(store.createdApps) != 0 {
                t.Errorf("expected 0 created apps, got %d", len(store.createdApps))
        }
}

// TestCreateApplication_Success verifies the happy path: valid request,
// no duplicate, PreValidate passes, application inserted with status=pending.
func TestCreateApplication_Success(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.pendingAppID = 0 // no duplicate
        store.rate = 30.0      // PreValidate will find a rate → success

        provider := newMockLWProvider() // no loans → "new" level
        engine := NewCreditEngine(provider, store)
        svc := NewApplicationService(store, engine, newMockCustomerStore())

        req := &model.CreateApplicationRequest{
                CustomerPIN: "PIN1", CustomerFullName: "Ali Valiyev",
                Amount: 200, TermMonths: 3, AkbScore: 400, CardNumber: "4111111111111111",
        }
        app, err := svc.CreateApplication(ctx, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Verify the application was inserted with correct fields
        if app.CustomerPIN != "PIN1" {
                t.Errorf("CustomerPIN = %q, want PIN1", app.CustomerPIN)
        }
        if app.Status != model.StatusPending {
                t.Errorf("Status = %q, want %q", app.Status, model.StatusPending)
        }
        if app.Amount != 200 {
                t.Errorf("Amount = %v, want 200", app.Amount)
        }
        if app.AkbScore != 400 {
                t.Errorf("AkbScore = %v, want 400", app.AkbScore)
        }

        // Verify the store received the create call
        if len(store.createdApps) != 1 {
                t.Fatalf("expected 1 created app in store, got %d", len(store.createdApps))
        }
        if store.createdApps[0].CustomerPIN != "PIN1" {
                t.Errorf("stored app CustomerPIN = %q, want PIN1", store.createdApps[0].CustomerPIN)
        }
}

// --- GetApplication tests ---

// TestGetApplication_InvalidID verifies that ID <= 0 is rejected.
func TestGetApplication_InvalidID(t *testing.T) {
        ctx := context.Background()
        svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.GetApplication(ctx, 0)
        if err == nil || !contains(err.Error(), "invalid application id") {
                t.Errorf("error = %v, want 'invalid application id'", err)
        }

        _, err = svc.GetApplication(ctx, -5)
        if err == nil || !contains(err.Error(), "invalid application id") {
                t.Errorf("error = %v, want 'invalid application id'", err)
        }
}

// TestGetApplication_NotFound verifies that a missing ID returns the store error.
func TestGetApplication_NotFound(t *testing.T) {
        ctx := context.Background()
        store := newMockStore() // empty → not found
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.GetApplication(ctx, 999)
        if err == nil {
                t.Fatal("expected not-found error, got nil")
        }
}

// --- GetStatus tests ---

// TestGetStatus_DecisionIncluded verifies that the decision block is included
// only for terminal / pending_approval statuses, not for pending/checking.
func TestGetStatus_DecisionIncluded(t *testing.T) {
        ctx := context.Background()

        tests := []struct {
                name           string
                appStatus      string
                wantDecision   bool
                wantRejection  bool
        }{
                {name: "pending → no decision", appStatus: model.StatusPending, wantDecision: false},
                {name: "checking → no decision", appStatus: model.StatusChecking, wantDecision: false},
                {name: "pending_approval → decision yes, no rejection", appStatus: model.StatusPendingApproval, wantDecision: true, wantRejection: false},
                {name: "approved → decision yes", appStatus: model.StatusApproved, wantDecision: true, wantRejection: false},
                {name: "rejected → decision yes, rejection reason set", appStatus: model.StatusRejected, wantDecision: true, wantRejection: true},
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        store := newMockStore()
                        store.appByID[1] = &model.LoanApplication{
                                ID: 1, Status: tc.appStatus,
                                ApprovedAmount: 500, ApprovedRate: 27.0,
                                RejectionReason: "test rejection",
                                UpdatedAt:       time.Now().Format(time.RFC3339),
                        }
                        store.checkResults = []model.ApplicationCheckResult{
                                {CheckType: "lms_active_loan_check", Status: "passed"},
                        }
                        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

                        resp, err := svc.GetStatus(ctx, 1)
                        if err != nil {
                                t.Fatalf("unexpected error: %v", err)
                        }

                        if tc.wantDecision && resp.Decision == nil {
                                t.Error("expected non-nil Decision, got nil")
                        }
                        if !tc.wantDecision && resp.Decision != nil {
                                t.Error("expected nil Decision, got non-nil")
                        }
                        if tc.wantDecision && resp.Decision != nil {
                                if resp.Decision.Decision != tc.appStatus {
                                        t.Errorf("Decision = %q, want %q", resp.Decision.Decision, tc.appStatus)
                                }
                        }
                        if tc.wantRejection {
                                if resp.Decision == nil || resp.Decision.RejectionReason == "" {
                                        t.Error("expected rejection reason to be set")
                                }
                        }
                })
        }
}

// TestGetStatus_InvalidID verifies ID validation.
func TestGetStatus_InvalidID(t *testing.T) {
        ctx := context.Background()
        svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.GetStatus(ctx, -1)
        if err == nil || !contains(err.Error(), "invalid application id") {
                t.Errorf("error = %v, want 'invalid application id'", err)
        }
}

// TestGetStatus_IncludesChecks verifies that check results are included.
func TestGetStatus_IncludesChecks(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusApproved}
        store.checkResults = []model.ApplicationCheckResult{
                {CheckType: "lms_active_loan_check", Status: "passed", Detail: "No active loans"},
                {CheckType: "lms_payment_history_check", Status: "passed", Detail: "On time"},
                {CheckType: "credit_level_check", Status: "passed", Detail: "elite"},
        }
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        resp, err := svc.GetStatus(ctx, 1)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        if len(resp.Checks) != 3 {
                t.Fatalf("expected 3 checks, got %d", len(resp.Checks))
        }
}

// --- GetChecks tests ---

// TestGetChecks_InvalidID verifies ID validation.
func TestGetChecks_InvalidID(t *testing.T) {
        ctx := context.Background()
        svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.GetChecks(ctx, 0)
        if err == nil || !contains(err.Error(), "invalid application id") {
                t.Errorf("error = %v, want 'invalid application id'", err)
        }
}

// TestGetChecks_ReturnsResults verifies that checks are returned from the store.
func TestGetChecks_ReturnsResults(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.checkResults = []model.ApplicationCheckResult{
                {CheckType: "type1", Status: "passed"},
                {CheckType: "type2", Status: "failed", Detail: "reason"},
        }
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        checks, err := svc.GetChecks(ctx, 5)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if len(checks) != 2 {
                t.Fatalf("expected 2 checks, got %d", len(checks))
        }
}

// --- UpdateStatus tests ---

// TestUpdateStatus_InvalidStatus verifies that only approved/rejected are
// accepted.
func TestUpdateStatus_InvalidStatus(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval}
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        tests := []string{"pending", "checking", "pending_approval", "foo", ""}
        for _, status := range tests {
                t.Run("status="+status, func(t *testing.T) {
                        _, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: status})
                        if err == nil {
                                t.Errorf("expected error for status %q, got nil", status)
                        }
                        if !contains(err.Error(), "must be") {
                                t.Errorf("error = %q, want 'must be' message", err.Error())
                        }
                })
        }
}

// TestUpdateStatus_NotPendingApproval verifies that the application must be
// in pending_approval status before manual update is allowed.
func TestUpdateStatus_NotPendingApproval(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusApproved}
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusRejected})
        if err == nil {
                t.Fatal("expected error, got nil")
        }
        if !contains(err.Error(), "expected 'pending_approval'") {
                t.Errorf("error = %q, want pending_approval message", err.Error())
        }
}

// TestUpdateStatus_ApprovedWithoutCreditLevel verifies that approve requires
// credit_level to be set.
func TestUpdateStatus_ApprovedWithoutCreditLevel(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval}
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved})
        if err == nil {
                t.Fatal("expected error, got nil")
        }
        if !contains(err.Error(), "credit_level is required") {
                t.Errorf("error = %q, want credit_level required message", err.Error())
        }
}

// TestUpdateStatus_ApprovedWithInvalidCreditLevel verifies the IsValidCreditLevel
// validation runs.
func TestUpdateStatus_ApprovedWithInvalidCreditLevel(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval}
        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
                Status:      model.StatusApproved,
                CreditLevel: "platinum", // not a valid level
        })
        if err == nil {
                t.Fatal("expected error, got nil")
        }
        if !contains(err.Error(), "must be one of") {
                t.Errorf("error = %q, want 'must be one of' message", err.Error())
        }
}

// TestUpdateStatus_ApproveSuccess verifies the happy path of approval.
func TestUpdateStatus_ApproveSuccess(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Status: model.StatusPendingApproval,
                Amount: 500, ApprovedRate: 27.0,
        }
        // The mock's UpdateApplicationDecision will mutate appByID[1] to reflect
        // the new status, so the second GetApplicationByID (after the update)
        // returns the approved state.

        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
                Status:      model.StatusApproved,
                CreditLevel: model.CreditLevelValuable,
        })
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.Status != model.StatusApproved {
                t.Errorf("status = %q, want approved", app.Status)
        }

        // Verify decision was recorded
        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision update, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusApproved {
                t.Errorf("decision status = %q, want approved", du.Status)
        }
        if du.CreditLevel != model.CreditLevelValuable {
                t.Errorf("decision credit_level = %q, want valuable", du.CreditLevel)
        }

        // Verify credit-level history was saved (only on approval)
        if len(store.historySaves) != 1 {
                t.Fatalf("expected 1 history save, got %d", len(store.historySaves))
        }
        if store.historySaves[0].ToLevel != model.CreditLevelValuable {
                t.Errorf("history level = %q, want valuable", store.historySaves[0].ToLevel)
        }
}

// TestUpdateStatus_RejectSuccess verifies the happy path of rejection.
func TestUpdateStatus_RejectSuccess(t *testing.T) {
        ctx := context.Background()

        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID: 1, CustomerPIN: "PIN1", Status: model.StatusPendingApproval,
                CreditLevel: model.CreditLevelNew, Amount: 200, ApprovedRate: 30.0,
        }
        // The mock will mutate this on UpdateApplicationDecision.

        svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
                Status: model.StatusRejected,
                // CreditLevel intentionally omitted — should fall back to existing
        })
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected", app.Status)
        }

        // Verify decision was recorded
        if len(store.decisionUpdates) != 1 {
                t.Fatalf("expected 1 decision update, got %d", len(store.decisionUpdates))
        }
        du := store.decisionUpdates[0]
        if du.Status != model.StatusRejected {
                t.Errorf("decision status = %q, want rejected", du.Status)
        }
        if du.RejectionReason != "Manually rejected" {
                t.Errorf("rejection reason = %q, want 'Manually rejected'", du.RejectionReason)
        }
        if du.CreditLevel != model.CreditLevelNew {
                t.Errorf("decision credit_level = %q, want new (existing)", du.CreditLevel)
        }

        // Verify NO credit-level history was saved (only on approval)
        if len(store.historySaves) != 0 {
                t.Errorf("expected 0 history saves for rejection, got %d", len(store.historySaves))
        }
}

// TestUpdateStatus_InvalidID verifies ID validation.
func TestUpdateStatus_InvalidID(t *testing.T) {
        ctx := context.Background()
        svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore())

        _, err := svc.UpdateStatus(ctx, 0, &UpdateStatusRequest{Status: model.StatusApproved})
        if err == nil || !contains(err.Error(), "invalid application id") {
                t.Errorf("error = %v, want 'invalid application id'", err)
        }
}

// --- Sanity test: lw.CustomerLoan sanity check ---

// TestLWCustomerLoan_Serialization is a tiny smoke test ensuring our mock
// helper `withLoans` produces a non-nil response with the right loan count.
// It guards against future refactors that accidentally return nil.
func TestLWCustomerLoan_Serialization(t *testing.T) {
        provider := newMockLWProvider().withLoans([]lw.CustomerLoan{
                {Status: "completed", WasOnTime: true, EarlyCompletion: true},
                {Status: "completed", WasOnTime: true, EarlyCompletion: false},
        })

        resp, err := provider.GetCustomerLoans(context.Background(), "PIN1")
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if resp == nil {
                t.Fatal("expected non-nil response")
        }
        if resp.LoanCount != 2 {
                t.Errorf("LoanCount = %d, want 2", resp.LoanCount)
        }
        if !resp.HasExistingLoans {
                t.Error("HasExistingLoans = false, want true")
        }
}

// --- Helpers used in this test file ---

// Ensure strings import is used (we use strings.Contains as a fallback when
// the local contains helper is shadowed).
var _ = strings.Contains
