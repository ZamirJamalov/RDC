package service

import (
        "context"
        "errors"
        "testing"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
)

// --- PR #58 tests: customer-confirm flow ---

// helper: build a mock store with a pending_expert application + offer ranges
func newConfirmStore() *mockApplicationStore {
        store := newMockStore()
        store.appByID[1] = &model.LoanApplication{
                ID:             1,
                CustomerPIN:    "PIN1",
                CustomerSerial: "AA1234567",
                CustomerPhone:  "+994501234567",
                Status:         model.StatusPendingExpert,
        }
        store.commission = 30.0
        store.approvedCount = 0
        store.currentLevel = ""
        store.levelRanges = []repository.LevelRange{
                {MinAmount: 50, MaxAmount: 300, TermMonths: 3, Rate: 30, Phase: 1},
                {MinAmount: 100, MaxAmount: 500, TermMonths: 6, Rate: 30, Phase: 1},
        }
        return store
}

// helper: build a mock LW provider with PersonalInfo + AKB Score configured
func newConfirmProvider() *mockLWProvider {
        provider := newMockLWProvider()
        provider.personalInfo = &lw.PersonalInfoResponse{
                Fin:         "PIN1",
                FullName:    "Test Customer",
                DateOfBirth: "1990-01-15",
        }
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:    "PIN1",
                Return: &lw.AkbScoreReturn{Response: "", Point: 650},
        }
        return provider
}

// TestCustomerConfirm_HappyPath verifies the happy path: customer submits
// amount + card + address + checkbox, backend fills in full_name + akb_score +
// term_months from external services.
func TestCustomerConfirm_HappyPath(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı, Nizami r., Murtuza Muxtarov 12",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Verify all fields were populated correctly
        if app.CustomerFullName != "Test Customer" {
                t.Errorf("customer_full_name = %q, want 'Test Customer'", app.CustomerFullName)
        }
        if app.Amount != 200 {
                t.Errorf("amount = %v, want 200", app.Amount)
        }
        if app.TermMonths != 3 {
                t.Errorf("term_months = %d, want 3 (matched from range 50-300)", app.TermMonths)
        }
        if app.AkbScore != 650 {
                t.Errorf("akb_score = %d, want 650", app.AkbScore)
        }
        if app.CardNumber != "4169731234567890" {
                t.Errorf("card_number = %q, want 4169731234567890", app.CardNumber)
        }
        if app.ActualAddress != "Bakı, Nizami r., Murtuza Muxtarov 12" {
                t.Errorf("actual_address = %q, want address", app.ActualAddress)
        }
        if !app.CardOwnershipConfirmed {
                t.Errorf("card_ownership_confirmed = false, want true")
        }
        if app.CustomerConfirmedAt == "" {
                t.Errorf("customer_confirmed_at = empty, want timestamp")
        }
        // PR #63 (Variant B): engine runs immediately at customer-confirm.
        // Customer has no loans, AKB=650, not blacklisted → credit level "new" → pending_approval.
        // (Previously Variant A kept status as pending_expert; now the engine runs.)
        if app.Status != model.StatusPendingApproval {
                t.Errorf("status = %q, want pending_approval (engine ran at customer-confirm, Variant B)", app.Status)
        }
}

// TestCustomerConfirm_AmountMatchesSecondRange verifies that amount matching
// the second range (100-500) returns term_months = 6 (not 3).
func TestCustomerConfirm_AmountMatchesSecondRange(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 400, // matches range 100-500 → term 6
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.TermMonths != 6 {
                t.Errorf("term_months = %d, want 6 (matched from range 100-500)", app.TermMonths)
        }
}

// TestCustomerConfirm_AmountOutOfRange verifies that an amount outside all
// ranges returns an error.
func TestCustomerConfirm_AmountOutOfRange(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 9999, // outside all ranges
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        _, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err == nil {
                t.Fatal("expected error for out-of-range amount, got nil")
        }
        if !contains(err.Error(), "keçərli deyil") {
                t.Errorf("error = %q, want 'keçərli deyil' message", err.Error())
        }
}

// TestCustomerConfirm_PersonalInfoFailsFailHard verifies that when
// GetPersonalInfo returns an error, the customer sees a fail-hard error
// (business decision PR #58).
func TestCustomerConfirm_PersonalInfoFailsFailHard(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        provider.personalInfoErr = errors.New("LW router unreachable")
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        _, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err == nil {
                t.Fatal("expected fail-hard error when PersonalInfo fails, got nil")
        }
        if !contains(err.Error(), "texniki xəta") {
                t.Errorf("error = %q, want 'texniki xəta' message", err.Error())
        }
}

// TestCustomerConfirm_AkbScoreZeroFailHard verifies that when AKB returns
// Point=0 (no usable data), the customer sees a fail-hard error.
func TestCustomerConfirm_AkbScoreZeroFailHard(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:    "PIN1",
                Return: &lw.AkbScoreReturn{Response: "", Point: 0},
        }
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        _, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err == nil {
                t.Fatal("expected fail-hard error when AKB returns 0, got nil")
        }
        if !contains(err.Error(), "texniki xəta") {
                t.Errorf("error = %q, want 'texniki xəta' message", err.Error())
        }
}

// TestCustomerConfirm_AkbStopFactorRejects verifies that when AKB returns
// Point=1 (stop factor), the application is rejected immediately at the
// customer-confirm stage.
func TestCustomerConfirm_AkbStopFactorRejects(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:    "PIN1",
                Return: &lw.AkbScoreReturn{Response: "AB", Point: 1},
        }
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (AKB stop factor)", app.Status)
        }
        if !contains(app.RejectionReason, "stop factor") {
                t.Errorf("rejection_reason = %q, want stop factor message", app.RejectionReason)
        }
}

// TestCustomerConfirm_ValidationErrors verifies each input validation rule.
func TestCustomerConfirm_ValidationErrors(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        tests := []struct {
                name string
                req  *CustomerConfirmRequest
                want string
        }{
                {
                        name: "amount zero",
                        req:  &CustomerConfirmRequest{Amount: 0, CardNumber: "4169731234567890", ActualAddress: "x", CardOwnershipConfirmed: true},
                        want: "amount must be greater than zero",
                },
                {
                        name: "card too short",
                        req:  &CustomerConfirmRequest{Amount: 200, CardNumber: "123", ActualAddress: "x", CardOwnershipConfirmed: true},
                        want: "card_number must be exactly 16 digits",
                },
                {
                        name: "address empty",
                        req:  &CustomerConfirmRequest{Amount: 200, CardNumber: "4169731234567890", ActualAddress: "", CardOwnershipConfirmed: true},
                        want: "actual_address is required",
                },
                {
                        name: "card ownership not confirmed",
                        req:  &CustomerConfirmRequest{Amount: 200, CardNumber: "4169731234567890", ActualAddress: "x", CardOwnershipConfirmed: false},
                        want: "card ownership must be confirmed",
                },
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        _, err := svc.CustomerConfirmApplication(ctx, 1, tc.req)
                        if err == nil {
                                t.Fatalf("expected error containing %q, got nil", tc.want)
                        }
                        if !contains(err.Error(), tc.want) {
                                t.Errorf("error = %q, want %q", err.Error(), tc.want)
                        }
                })
        }
}

// TestCustomerConfirm_WrongStatus verifies that confirming an application not
// in pending_expert status returns an error.
func TestCustomerConfirm_WrongStatus(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        store.appByID[1].Status = model.StatusPendingCustomer // wrong status
        provider := newConfirmProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        _, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err == nil {
                t.Fatal("expected error for wrong status, got nil")
        }
        if !contains(err.Error(), "pending_expert") {
                t.Errorf("error = %q, want 'pending_expert' message", err.Error())
        }
}

// --- PR #58 tests: relaxed CompleteApplication validation ---

// TestCompleteApplication_RelaxedValidation verifies that when an application
// has already been customer-confirmed (full_name, amount, term, card, address
// already set), the expert can call CompleteApplication with ONLY contact1_phone
// and the engine is triggered.
func TestCompleteApplication_RelaxedValidation(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        // Simulate a customer-confirmed application
        store.appByID[1] = &model.LoanApplication{
                ID:                    1,
                CustomerPIN:           "PIN1",
                CustomerFullName:      "Test Customer",
                Amount:                200,
                TermMonths:            3,
                CardNumber:            "4169731234567890",
                ActualAddress:         "Bakı",
                AkbScore:              650,
                CustomerConfirmedAt:   "2026-07-22T10:00:00Z",
                CardOwnershipConfirmed: true,
                Status:                model.StatusPendingExpert,
        }
        store.commission = 30.0

        provider := newMockLWProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        // Expert provides ONLY contact1_phone — other fields are already set
        req := &CompleteApplicationRequest{
                Contact1Phone: "+994501111111",
                Contact2Phone: "+994502222222",
                Contact3Phone: "+994503333333",
        }

        app, err := svc.CompleteApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }

        // Verify existing fields preserved
        if app.CustomerFullName != "Test Customer" {
                t.Errorf("customer_full_name = %q, want preserved 'Test Customer'", app.CustomerFullName)
        }
        if app.Amount != 200 {
                t.Errorf("amount = %v, want preserved 200", app.Amount)
        }
        if app.TermMonths != 3 {
                t.Errorf("term_months = %d, want preserved 3", app.TermMonths)
        }
        // Verify contacts added
        if app.Contact1Phone != "+994501111111" {
                t.Errorf("contact1_phone = %q, want +994501111111", app.Contact1Phone)
        }
        // Status should be pending (engine will transition to checking)
        if app.Status != model.StatusPending {
                t.Errorf("status = %q, want pending", app.Status)
        }
}

// TestCompleteApplication_Contact1Required verifies that contact1_phone is
// required even when all other fields are set (expert must collect at least
// 1 contact).
func TestCompleteApplication_Contact1Required(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        store.appByID[1] = &model.LoanApplication{
                ID:               1,
                CustomerPIN:      "PIN1",
                CustomerFullName: "Test Customer",
                Amount:           200,
                TermMonths:       3,
                CardNumber:       "4169731234567890",
                ActualAddress:    "Bakı",
                AkbScore:         650,
                Status:           model.StatusPendingExpert,
        }
        store.commission = 30.0

        provider := newMockLWProvider()
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        // Expert provides NO contacts — should fail
        req := &CompleteApplicationRequest{}

        _, err := svc.CompleteApplication(ctx, 1, req)
        if err == nil {
                t.Fatal("expected error for missing contact1_phone, got nil")
        }
        if !contains(err.Error(), "contact1_phone is required") {
                t.Errorf("error = %q, want 'contact1_phone is required'", err.Error())
        }
}

// --- PR #63 (Variant B) tests ---

// TestCustomerConfirm_VariantB_RejectedByEngine verifies that when the credit
// engine rejects the application at customer-confirm (e.g. AKB stop factor),
// the customer-confirm response carries status=rejected.
func TestCustomerConfirm_VariantB_RejectedByEngine(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        // AKB stop factor → engine rejects
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:    "PIN1",
                Return: &lw.AkbScoreReturn{Response: "AB", Point: 1},
        }
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.Status != model.StatusRejected {
                t.Errorf("status = %q, want rejected (engine ran at customer-confirm)", app.Status)
        }
        if !contains(app.RejectionReason, "stop factor") {
                t.Errorf("rejection_reason = %q, want stop factor message", app.RejectionReason)
        }
}

// TestCustomerConfirm_VariantB_EliteDowngraded verifies that when the engine
// auto-approves an elite customer, the status is downgraded to pending_approval
// (Variant B: expert must still verify employment/pension + collect contacts).
func TestCustomerConfirm_VariantB_EliteDowngraded(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        // Simulate elite customer: 2 completed on-time loans at valuable level
        store.appByID[1] = &model.LoanApplication{
                ID:             1,
                CustomerPIN:    "PIN1",
                CustomerSerial: "AA1234567",
                CustomerPhone:  "+994501234567",
                Status:         model.StatusPendingExpert,
        }
        store.commission = 20.0 // elite rate
        store.approvedCount = 2
        store.currentLevel = model.CreditLevelValuable
        store.levelRanges = []repository.LevelRange{
                {MinAmount: 50, MaxAmount: 300, TermMonths: 3, Rate: 20, Phase: 2},
        }

        provider := newConfirmProvider()
        // AKB 750 → valuable override (but already valuable from loan history)
        provider.akbScore = &lw.AkbScoreResponse{
                Fin:    "PIN1",
                Return: &lw.AkbScoreReturn{Response: "", Point: 750},
        }
        // 2 completed valuable loans → elite
        provider.loans = &lw.CustomerLoansResponse{
                CustomerPIN:      "PIN1",
                HasExistingLoans: true,
                LoanCount:        2,
                Loans: []lw.CustomerLoan{
                        {ID: 1, CustomerPIN: "PIN1", Status: "completed", Amount: 300, TermMonths: 2, WasOnTime: true, DelayDays: 0, LevelAtClose: "valuable"},
                        {ID: 2, CustomerPIN: "PIN1", Status: "completed", Amount: 300, TermMonths: 2, WasOnTime: true, DelayDays: 0, LevelAtClose: "valuable"},
                },
        }

        svc := NewApplicationService(store, NewCreditEngine(provider, store), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        // Engine would auto-approve elite, but Variant B downgrades to pending_approval
        if app.Status != model.StatusPendingApproval {
                t.Errorf("status = %q, want pending_approval (elite downgraded, Variant B)", app.Status)
        }
        if app.CreditLevel != model.CreditLevelElite {
                t.Errorf("credit_level = %q, want elite (engine determined elite, just downgraded status)", app.CreditLevel)
        }
}

// TestCustomerConfirm_VariantB_PendingApprovalForNewCustomer verifies the
// standard new-customer flow: no loans, AKB 650 → credit level "new" →
// pending_approval (expert review needed).
func TestCustomerConfirm_VariantB_PendingApprovalForNewCustomer(t *testing.T) {
        ctx := context.Background()

        store := newConfirmStore()
        provider := newConfirmProvider()
        // Default: AKB 650, no loans, not blacklisted → "new" level → pending_approval
        svc := NewApplicationService(store, NewCreditEngine(provider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

        req := &CustomerConfirmRequest{
                Amount:                 200,
                CardNumber:             "4169731234567890",
                ActualAddress:          "Bakı",
                CardOwnershipConfirmed: true,
        }

        app, err := svc.CustomerConfirmApplication(ctx, 1, req)
        if err != nil {
                t.Fatalf("unexpected error: %v", err)
        }
        if app.Status != model.StatusPendingApproval {
                t.Errorf("status = %q, want pending_approval (new customer, Variant B)", app.Status)
        }
        if app.CreditLevel != model.CreditLevelNew {
                t.Errorf("credit_level = %q, want new", app.CreditLevel)
        }
}
