package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/azmk"
	"rdc-source/pkg/lw"
	"rdc-source/pkg/mygov"
)

// --- PR #227 tests: customer-confirm flow (AZMK-first, fail-soft, no LW router) ---

// mockAzmkCustomerData is a test-only implementation of azmk.CustomerDataProvider
// with configurable return values, so tests can simulate AZMK successes, errors
// and stop-factor responses without the scenario-map logic of the built-in mock.
type mockAzmkCustomerData struct {
	personalData *azmk.CustomerData
	personalErr  error

	ownerData *azmk.OwnerData
	ownerErr  error

	mkrScore *azmk.MkrScore
	mkrErr   error

	history *azmk.CreditHistory
	histErr error

	// PR #240: GetEmployeeInfoByPin — iş yeri məlumatları mock-u.
	// customer-confirm flow bu metodu çağırmır, amma interfeysi satmaq
	// üçün impl obliged-dır. Default: nil, nil (fail-soft).
	employeeData *mygov.EmployeeInfoResponse
	employeeErr  error
}

func (m *mockAzmkCustomerData) GetPersonalInfo(_ context.Context, _, _ string) (*azmk.CustomerData, error) {
	if m.personalErr != nil {
		return nil, m.personalErr
	}
	return m.personalData, nil
}

func (m *mockAzmkCustomerData) GetOwnerData(_ context.Context, _, _ string) (*azmk.OwnerData, error) {
	if m.ownerErr != nil {
		return nil, m.ownerErr
	}
	return m.ownerData, nil
}

func (m *mockAzmkCustomerData) GetMkrScore(_ context.Context, _, _ string) (*azmk.MkrScore, error) {
	if m.mkrErr != nil {
		return nil, m.mkrErr
	}
	return m.mkrScore, nil
}

func (m *mockAzmkCustomerData) InquireByIdCard(_ context.Context, _, _ string) (*azmk.CreditHistory, error) {
	if m.histErr != nil {
		return nil, m.histErr
	}
	return m.history, nil
}

// GetEmployeeInfoByPin — PR #240: mock implementasiyası. Customer-confirm
// flow bu metodu çağırmır (iş yeri sorğusu dashboard-dan ayrıca vurur),
// amma azmk.CustomerDataProvider interfeysi tələb etdiyi üçün əlavə olundu.
func (m *mockAzmkCustomerData) GetEmployeeInfoByPin(_ context.Context, _, _ string) (*mygov.EmployeeInfoResponse, error) {
	if m.employeeErr != nil {
		return nil, m.employeeErr
	}
	return m.employeeData, nil
}

// newConfirmStore builds a mock store with a pending_customer application + offer ranges.
// PR #221: customer-confirm requires pending_customer (OTP verified, cutoffs passed).
func newConfirmStore() *mockApplicationStore {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{
		ID:             1,
		CustomerPIN:    "PIN1",
		CustomerSerial: "AA1234567",
		CustomerPhone:  "+994501234567",
		Status:         model.StatusPendingCustomer,
	}
	store.commission = 30.0
	store.approvedCount = 0
	store.currentLevel = ""
	store.levelRanges = []repository.LevelRange{
		{MinAmount: 50, MaxAmount: 300, TermMonths: 3, Commission: 30, Phase: 1},
		{MinAmount: 100, MaxAmount: 500, TermMonths: 6, Commission: 30, Phase: 1},
	}
	return store
}

// newConfirmAzmkProvider returns an AZMK mock with a happy-path configuration:
// personal info (Test Customer) + mkr score 650, no stop factor.
func newConfirmAzmkProvider() *mockAzmkCustomerData {
	return &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			Name:      "Test",
			Surname:   "Customer",
			BirthDate: "1990-01-15",
		},
		mkrScore: &azmk.MkrScore{
			Score: azmk.MkrScoreDetail{Point: 650, Response: "B", Calculated: true},
		},
	}
}

// newConfirmLWProvider returns an LW mock with an AKB score configured.
// PR #227: customer-confirm no longer calls the LW router directly, but GetOffer
// still resolves the score fail-soft (LW first, request fallback).
func newConfirmLWProvider() *mockLWProvider {
	provider := newMockLWProvider()
	provider.akbScore = &lw.AkbScoreResponse{
		Fin:    "PIN1",
		Return: &lw.AkbScoreReturn{Response: "", Point: 650},
	}
	return provider
}

// newConfirmService wires a service with the given LW + AZMK mocks.
func newConfirmService(store *mockApplicationStore, lwProvider *mockLWProvider, azmkProvider *mockAzmkCustomerData) *ApplicationService {
	svc := NewApplicationService(store, NewCreditEngine(lwProvider, newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	if azmkProvider != nil {
		svc.SetCustomerDataProvider(azmkProvider)
	}
	return svc
}

// TestCustomerConfirm_HappyPath verifies the happy path: customer submits
// amount + card + address + checkbox, backend fills in full_name + akb_score
// from AZMK (PR #227) and the application moves to pending_expert.
func TestCustomerConfirm_HappyPath(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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
		t.Errorf("akb_score = %d, want 650 (AZMK getMkrScore)", app.AkbScore)
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
	// PR #221: engine does NOT run at customer-confirm — the application
	// transitions to pending_expert and waits for the expert in the RDC dashboard.
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (no engine at customer-confirm, PR #221)", app.Status)
	}
}

// TestCustomerConfirm_AmountMatchesSecondRange verifies that amount matching
// the second range (100-500) returns term_months = 6 (not 3).
func TestCustomerConfirm_AmountMatchesSecondRange(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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

// TestCustomerConfirm_PersonalInfoFailsFailSoft verifies that when AZMK
// GetPersonalInfo returns an error, the customer can STILL confirm (PR #227):
// the name is left empty and the application proceeds to pending_expert.
// This is the old fail-hard LW behavior being intentionally relaxed.
func TestCustomerConfirm_PersonalInfoFailsFailSoft(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	azmkProvider := newConfirmAzmkProvider()
	azmkProvider.personalErr = errors.New("AZMK CustomerDataService unreachable")
	svc := newConfirmService(store, newConfirmLWProvider(), azmkProvider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		CardNumber:             "4169731234567890",
		ActualAddress:          "Bakı",
		CardOwnershipConfirmed: true,
	}

	app, err := svc.CustomerConfirmApplication(ctx, 1, req)
	if err != nil {
		t.Fatalf("unexpected error (personal info must be fail-soft now, PR #227): %v", err)
	}
	if app.CustomerFullName != "" {
		t.Errorf("customer_full_name = %q, want empty (AZMK failed)", app.CustomerFullName)
	}
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (confirm proceeds despite AZMK failure)", app.Status)
	}
}

// TestCustomerConfirm_AkbScoreUnavailableFailSoft verifies that when AZMK
// getMkrScore fails (or returns Point=0), the customer can STILL confirm:
// akb_score stays 0 and the application proceeds to pending_expert.
func TestCustomerConfirm_AkbScoreUnavailableFailSoft(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	azmkProvider := newConfirmAzmkProvider()
	azmkProvider.mkrErr = errors.New("AZMK getMkrScore unreachable")
	svc := newConfirmService(store, newConfirmLWProvider(), azmkProvider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		CardNumber:             "4169731234567890",
		ActualAddress:          "Bakı",
		CardOwnershipConfirmed: true,
	}

	app, err := svc.CustomerConfirmApplication(ctx, 1, req)
	if err != nil {
		t.Fatalf("unexpected error (akb score must be fail-soft now, PR #227): %v", err)
	}
	if app.AkbScore != 0 {
		t.Errorf("akb_score = %d, want 0 (AZMK failed)", app.AkbScore)
	}
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (confirm proceeds despite AZMK failure)", app.Status)
	}
}

// TestCustomerConfirm_AkbStopFactorRejects verifies that when AZMK getMkrScore
// returns a stop-factor response (AB/NI/NU/TY), the application is rejected
// immediately at the customer-confirm stage.
func TestCustomerConfirm_AkbStopFactorRejects(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	azmkProvider := newConfirmAzmkProvider()
	azmkProvider.mkrScore = &azmk.MkrScore{
		Score: azmk.MkrScoreDetail{Point: 500, Response: "AB", Calculated: true},
	}
	svc := newConfirmService(store, newConfirmLWProvider(), azmkProvider)

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
	if !contains(app.RejectionReason, "AKB_STOP_FACTOR") {
		t.Errorf("rejection_reason = %q, want AKB_STOP_FACTOR", app.RejectionReason)
	}
}

// TestCustomerConfirm_NoAzmkProvider verifies the nil-guard: when no AZMK
// customer-data provider is wired (e.g. older tests), customer-confirm still
// works — name and score are simply left empty/zero (fail-soft).
func TestCustomerConfirm_NoAzmkProvider(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	svc := newConfirmService(store, newConfirmLWProvider(), nil)

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
	if app.CustomerFullName != "" {
		t.Errorf("customer_full_name = %q, want empty (no provider)", app.CustomerFullName)
	}
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert", app.Status)
	}
}

// TestCustomerConfirm_LwDownStillConfirms simulates the reported production
// issue: the LW router is completely unreachable (connection refused), but the
// customer must still be able to confirm. AZMK supplies the personal data and
// the score; GetOffer degrades fail-soft (empty loans, fallback score).
func TestCustomerConfirm_LwDownStillConfirms(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	lwProvider := newConfirmLWProvider()
	lwProvider.loansErr = errors.New("dial tcp 127.0.0.1:8080: connect: connection refused")
	lwProvider.akbScoreErr = errors.New("dial tcp 127.0.0.1:8080: connect: connection refused")
	azmkProvider := newConfirmAzmkProvider()
	azmkProvider.mkrScore = &azmk.MkrScore{
		Score: azmk.MkrScoreDetail{Point: 750, Response: "B", Calculated: true},
	}
	svc := newConfirmService(store, lwProvider, azmkProvider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		CardNumber:             "4169731234567890",
		ActualAddress:          "Bakı",
		CardOwnershipConfirmed: true,
	}

	app, err := svc.CustomerConfirmApplication(ctx, 1, req)
	if err != nil {
		t.Fatalf("unexpected error (LW router down must not block confirm, PR #227): %v", err)
	}
	if app.CustomerFullName != "Test Customer" {
		t.Errorf("customer_full_name = %q, want 'Test Customer' (from AZMK)", app.CustomerFullName)
	}
	if app.AkbScore != 750 {
		t.Errorf("akb_score = %d, want 750 (from AZMK getMkrScore)", app.AkbScore)
	}
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert", app.Status)
	}
}

// TestCustomerConfirm_ValidationErrors verifies each input validation rule.
func TestCustomerConfirm_ValidationErrors(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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
// in pending_customer status returns an error.
func TestCustomerConfirm_WrongStatus(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	store.appByID[1].Status = model.StatusPendingExpert // wrong status for confirm
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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
		t.Errorf("error = %q, want current status in message", err.Error())
	}
}

// TestCustomerConfirm_NewCustomerGoesPendingExpert verifies the standard
// new-customer flow: no loans, AKB 650 → credit level "new" → pending_expert
// (no engine at customer-confirm, PR #221).
func TestCustomerConfirm_NewCustomerGoesPendingExpert(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	svc := newConfirmService(store, newConfirmLWProvider(), newConfirmAzmkProvider())

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
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (new customer, no engine at confirm)", app.Status)
	}
	if app.CreditLevel != model.CreditLevelNew {
		t.Errorf("credit_level = %q, want new", app.CreditLevel)
	}
}

// TestCustomerConfirm_EliteCustomerGoesPendingExpert verifies that even an
// elite customer (2 completed valuable loans, AKB 750) ends up in
// pending_expert with credit_level populated from the offer — the expert
// still reviews the application in the RDC dashboard.
func TestCustomerConfirm_EliteCustomerGoesPendingExpert(t *testing.T) {
	ctx := context.Background()

	store := newConfirmStore()
	store.commission = 20.0 // elite rate
	store.approvedCount = 2
	store.currentLevel = model.CreditLevelValuable
	store.levelRanges = []repository.LevelRange{
		{MinAmount: 50, MaxAmount: 300, TermMonths: 3, Commission: 20, Phase: 2},
	}

	lwProvider := newConfirmLWProvider()
	// 2 completed valuable loans → elite
	lwProvider.loans = &lw.CustomerLoansResponse{
		CustomerPIN:      "PIN1",
		HasExistingLoans: true,
		LoanCount:        2,
		Loans: []lw.CustomerLoan{
			{ID: 1, CustomerPIN: "PIN1", Status: "completed", Amount: 300, TermMonths: 2, WasOnTime: true, DelayDays: 0, LevelAtClose: "valuable"},
			{ID: 2, CustomerPIN: "PIN1", Status: "completed", Amount: 300, TermMonths: 2, WasOnTime: true, DelayDays: 0, LevelAtClose: "valuable"},
		},
	}
	lwProvider.akbScore = &lw.AkbScoreResponse{
		Fin:    "PIN1",
		Return: &lw.AkbScoreReturn{Response: "", Point: 650}, // < 700: no override → promotion applies
	}

	azmkProvider := newConfirmAzmkProvider()
	azmkProvider.mkrScore = &azmk.MkrScore{
		Score: azmk.MkrScoreDetail{Point: 650, Response: "B", Calculated: true},
	}
	svc := newConfirmService(store, lwProvider, azmkProvider)

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
	if app.Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (expert still reviews, PR #221)", app.Status)
	}
	if app.CreditLevel != model.CreditLevelElite {
		t.Errorf("credit_level = %q, want elite (determined from offer)", app.CreditLevel)
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
		ID:                     1,
		CustomerPIN:            "PIN1",
		CustomerFullName:       "Test Customer",
		Amount:                 200,
		TermMonths:             3,
		CardNumber:             "4169731234567890",
		ActualAddress:          "Bakı",
		AkbScore:               650,
		CustomerConfirmedAt:    "2026-07-22T10:00:00Z",
		CardOwnershipConfirmed: true,
		Status:                 model.StatusPendingExpert,
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
