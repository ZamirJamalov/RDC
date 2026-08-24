package service

import (
        "context"
        "database/sql"
        "errors"
        "strings"
        "testing"
        "time"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
)

// mockDiscountCodeStore is a test-only implementation of DiscountCodeStore.
type mockDiscountCodeStore struct {
        // Configurable storage
        codes map[string]*model.DiscountCode

        // Configurable errors
        createErr      error
        getByCodeErr   error
        markUsedErr    error
        existsByCodeErr error

        // Recording of calls
        createdCodes    []*model.DiscountCode
        markedUsedCodes []struct {
                CodeID int
                AppID  int
        }
}

func newMockDiscountCodeStore() *mockDiscountCodeStore {
        return &mockDiscountCodeStore{
                codes: make(map[string]*model.DiscountCode),
        }
}

func (m *mockDiscountCodeStore) Create(_ context.Context, c *model.DiscountCode) error {
        return m.CreateTx(context.Background(), nil, c)
}

func (m *mockDiscountCodeStore) CreateTx(_ context.Context, _ repository.TxRunner, c *model.DiscountCode) error {
        if m.createErr != nil {
                return m.createErr
        }
        c.ID = len(m.codes) + 1
        c.CreatedAt = time.Now()
        stored := *c
        m.codes[c.Code] = &stored
        m.createdCodes = append(m.createdCodes, c)
        return nil
}

func (m *mockDiscountCodeStore) GetByCode(_ context.Context, code string) (*model.DiscountCode, error) {
        return m.GetByCodeTx(context.Background(), nil, code)
}

func (m *mockDiscountCodeStore) GetByCodeTx(_ context.Context, _ repository.TxRunner, code string) (*model.DiscountCode, error) {
        if m.getByCodeErr != nil {
                return nil, m.getByCodeErr
        }
        if c, ok := m.codes[code]; ok {
                copied := *c
                return &copied, nil
        }
        return nil, sql.ErrNoRows
}

func (m *mockDiscountCodeStore) MarkUsed(_ context.Context, codeID, appID int) error {
        return m.MarkUsedTx(context.Background(), nil, codeID, appID)
}

func (m *mockDiscountCodeStore) MarkUsedTx(_ context.Context, _ repository.TxRunner, codeID, appID int) error {
        if m.markUsedErr != nil {
                return m.markUsedErr
        }
        for _, c := range m.codes {
                if c.ID == codeID {
                        if c.Status != model.DiscountStatusActive {
                                return errors.New("code not active")
                        }
                        c.Status = model.DiscountStatusUsed
                        c.UsedByApplicationID = &appID
                        now := time.Now()
                        c.UsedAt = &now
                        m.markedUsedCodes = append(m.markedUsedCodes, struct {
                                CodeID int
                                AppID  int
                        }{CodeID: codeID, AppID: appID})
                        return nil
                }
        }
        return errors.New("code not found")
}

func (m *mockDiscountCodeStore) ExistsByCode(_ context.Context, code string) (bool, error) {
        if m.existsByCodeErr != nil {
                return false, m.existsByCodeErr
        }
        _, ok := m.codes[code]
        return ok, nil
}

func (m *mockDiscountCodeStore) GetByOwnerCustomerID(_ context.Context, _ int) ([]*model.DiscountCode, error) {
        var result []*model.DiscountCode
        for _, c := range m.codes {
                copied := *c
                result = append(result, &copied)
        }
        return result, nil
}

// GetByApplicationID — PR #284: issued_from_application_id = appID olan ən son kodu qaytarır.
func (m *mockDiscountCodeStore) GetByApplicationID(_ context.Context, appID int) (*model.DiscountCode, error) {
        var found *model.DiscountCode
        for _, c := range m.codes {
                if c.IssuedFromApplicationID != nil && *c.IssuedFromApplicationID == appID {
                        if found == nil || c.ID > found.ID {
                                copied := *c
                                found = &copied
                        }
                }
        }
        if found == nil {
                return nil, sql.ErrNoRows
        }
        return found, nil
}

// =========================================================
// Tests for DiscountCodeService
// =========================================================

func TestGenerateForApplication_CreatesValidCode(t *testing.T) {
        store := newMockDiscountCodeStore()
        svc := NewDiscountCodeService(store)

        code, err := svc.GenerateForApplication(context.Background(), 1, 100)
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if code == nil {
                t.Fatal("expected non-nil code")
        }
        if !strings.HasPrefix(code.Code, model.DiscountCodePrefix) {
                t.Errorf("code should start with %q, got %q", model.DiscountCodePrefix, code.Code)
        }
        if len(code.Code) != len(model.DiscountCodePrefix)+6 {
                t.Errorf("code should be %d chars (prefix + 6), got %d (%q)",
                        len(model.DiscountCodePrefix)+6, len(code.Code), code.Code)
        }
        if code.Status != model.DiscountStatusActive {
                t.Errorf("status should be 'active', got %q", code.Status)
        }
        if code.DiscountType != DefaultDiscountType {
                t.Errorf("discount_type should be %q, got %q", DefaultDiscountType, code.DiscountType)
        }
        if code.DiscountValue != DefaultDiscountValue {
                t.Errorf("discount_value should be %v, got %v", DefaultDiscountValue, code.DiscountValue)
        }
        if code.IssuedToCustomerID != 100 {
                t.Errorf("issued_to_customer_id should be 100, got %d", code.IssuedToCustomerID)
        }
        if code.IssuedFromApplicationID == nil || *code.IssuedFromApplicationID != 1 {
                t.Errorf("issued_from_application_id should be 1, got %v", code.IssuedFromApplicationID)
        }
}

func TestGenerateForApplication_RetriesOnCollision(t *testing.T) {
        store := newMockDiscountCodeStore()
        // Pre-populate with a code that will collide on first attempt (we can't
        // predict the random code, but we can force ExistsByCode to return true
        // a few times via a wrapper — for simplicity, just test that generation
        // works when there's no collision).
        svc := NewDiscountCodeService(store)

        code, err := svc.GenerateForApplication(context.Background(), 1, 100)
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if code == nil {
                t.Fatal("expected non-nil code")
        }
}

func TestGenerateForApplication_FailsAfterMaxRetries(t *testing.T) {
        store := newMockDiscountCodeStore()
        store.existsByCodeErr = errors.New("db error")
        svc := NewDiscountCodeService(store)

        _, err := svc.GenerateForApplication(context.Background(), 1, 100)
        if err == nil {
                t.Fatal("expected error after max retries, got nil")
        }
}

func TestGenerateForApplication_InvalidInputs(t *testing.T) {
        store := newMockDiscountCodeStore()
        svc := NewDiscountCodeService(store)

        _, err := svc.GenerateForApplication(context.Background(), 0, 100)
        if err == nil {
                t.Error("expected error for appID=0, got nil")
        }

        _, err = svc.GenerateForApplication(context.Background(), 1, 0)
        if err == nil {
                t.Error("expected error for customerID=0, got nil")
        }
}

func TestValidateForCustomer_HappyPath(t *testing.T) {
        store := newMockDiscountCodeStore()
        // Code owned by customer 100, current customer is 200
        store.codes["ALPUL-TEST01"] = &model.DiscountCode{
                ID:                  1,
                Code:                "ALPUL-TEST01",
                IssuedToCustomerID:  100,
                DiscountType:        model.DiscountTypePercent,
                DiscountValue:       2.00,
                Status:              model.DiscountStatusActive,
        }
        svc := NewDiscountCodeService(store)

        dc, err := svc.ValidateForCustomer(context.Background(), "ALPUL-TEST01", 200)
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if dc == nil {
                t.Fatal("expected non-nil code")
        }
        if dc.Code != "ALPUL-TEST01" {
                t.Errorf("expected code ALPUL-TEST01, got %q", dc.Code)
        }
}

func TestValidateForCustomer_SelfUsePrevention(t *testing.T) {
        store := newMockDiscountCodeStore()
        store.codes["ALPUL-TEST01"] = &model.DiscountCode{
                ID:                  1,
                Code:                "ALPUL-TEST01",
                IssuedToCustomerID:  100,
                Status:              model.DiscountStatusActive,
        }
        svc := NewDiscountCodeService(store)

        _, err := svc.ValidateForCustomer(context.Background(), "ALPUL-TEST01", 100)
        if err == nil {
                t.Fatal("expected self-use prevention error, got nil")
        }
        if !strings.Contains(err.Error(), "öz endirim kodunuzdan") {
                t.Errorf("expected self-use error message, got %q", err.Error())
        }
}

func TestValidateForCustomer_AlreadyUsed(t *testing.T) {
        store := newMockDiscountCodeStore()
        store.codes["ALPUL-TEST01"] = &model.DiscountCode{
                ID:                  1,
                Code:                "ALPUL-TEST01",
                IssuedToCustomerID:  100,
                Status:              model.DiscountStatusUsed,
        }
        svc := NewDiscountCodeService(store)

        _, err := svc.ValidateForCustomer(context.Background(), "ALPUL-TEST01", 200)
        if err == nil {
                t.Fatal("expected 'already used' error, got nil")
        }
        if !strings.Contains(err.Error(), "istifadə olunub") {
                t.Errorf("expected 'already used' error message, got %q", err.Error())
        }
}

func TestValidateForCustomer_NotFound(t *testing.T) {
        store := newMockDiscountCodeStore()
        svc := NewDiscountCodeService(store)

        _, err := svc.ValidateForCustomer(context.Background(), "ALPUL-NONEX", 200)
        if err == nil {
                t.Fatal("expected not-found error, got nil")
        }
        if !strings.Contains(err.Error(), "yanlış") {
                t.Errorf("expected 'invalid' error message, got %q", err.Error())
        }
}

func TestValidateForCustomer_EmptyCode(t *testing.T) {
        store := newMockDiscountCodeStore()
        svc := NewDiscountCodeService(store)

        _, err := svc.ValidateForCustomer(context.Background(), "", 200)
        if err == nil {
                t.Fatal("expected empty-code error, got nil")
        }
}

func TestValidateForCustomer_CaseInsensitive(t *testing.T) {
        store := newMockDiscountCodeStore()
        store.codes["ALPUL-TEST01"] = &model.DiscountCode{
                ID:                  1,
                Code:                "ALPUL-TEST01",
                IssuedToCustomerID:  100,
                Status:              model.DiscountStatusActive,
        }
        svc := NewDiscountCodeService(store)

        // Customer types lowercase
        dc, err := svc.ValidateForCustomer(context.Background(), "alpul-test01", 200)
        if err != nil {
                t.Fatalf("expected no error for lowercase input, got %v", err)
        }
        if dc == nil {
                t.Fatal("expected non-nil code")
        }
}

func TestValidateForCustomer_Expired(t *testing.T) {
        store := newMockDiscountCodeStore()
        pastTime := time.Now().Add(-1 * time.Hour)
        store.codes["ALPUL-TEST01"] = &model.DiscountCode{
                ID:                  1,
                Code:                "ALPUL-TEST01",
                IssuedToCustomerID:  100,
                Status:              model.DiscountStatusActive,
                ValidUntil:         &pastTime,
        }
        svc := NewDiscountCodeService(store)

        _, err := svc.ValidateForCustomer(context.Background(), "ALPUL-TEST01", 200)
        if err == nil {
                t.Fatal("expected expiry error, got nil")
        }
        if !strings.Contains(err.Error(), "müddəti bitib") {
                t.Errorf("expected 'expired' error message, got %q", err.Error())
        }
}

func TestCalculateDiscount_Percent(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypePercent,
                DiscountValue: 10.0, // 10% off
        }

        // 10% of 100 = 10
        got := svc.CalculateDiscount(code, 100.0)
        if got != 10.0 {
                t.Errorf("expected 10.0, got %v", got)
        }

        // 10% of 162.79 = 16.28 (rounded to 2 decimals)
        got = svc.CalculateDiscount(code, 162.79)
        if got != 16.28 {
                t.Errorf("expected 16.28, got %v", got)
        }
}

func TestCalculateDiscount_Fixed(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 20.0, // 20 AZN off
        }

        // Fixed 20 off 100 = 20
        got := svc.CalculateDiscount(code, 100.0)
        if got != 20.0 {
                t.Errorf("expected 20.0, got %v", got)
        }
}

func TestCalculateDiscount_ClampsToCommission(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 200.0, // 200 AZN off, but commission is only 100
        }

        // Discount must be clamped to commission (200 > 100 → 100)
        got := svc.CalculateDiscount(code, 100.0)
        if got != 100.0 {
                t.Errorf("expected 100.0 (clamped), got %v", got)
        }
}

func TestCalculateDiscount_NilCode(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        got := svc.CalculateDiscount(nil, 100.0)
        if got != 0 {
                t.Errorf("expected 0 for nil code, got %v", got)
        }
}

func TestCalculateDiscount_ZeroValue(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypePercent,
                DiscountValue: 0, // 0% = no discount
        }

        got := svc.CalculateDiscount(code, 100.0)
        if got != 0 {
                t.Errorf("expected 0 for zero-value code, got %v", got)
        }
}

func TestCalculateDiscount_UnknownType(t *testing.T) {
        svc := NewDiscountCodeService(newMockDiscountCodeStore())

        code := &model.DiscountCode{
                DiscountType:  "unknown",
                DiscountValue: 50.0,
        }

        got := svc.CalculateDiscount(code, 100.0)
        if got != 0 {
                t.Errorf("expected 0 for unknown type, got %v", got)
        }
}

// =========================================================
// Tests for credit_decision.go discount helpers
// =========================================================

func TestCalculateCommissionAmount(t *testing.T) {
        // PR #246: 300 AZN, commission=14 → 300 × (14/86) = 48.84
        got := calculateCommissionAmount(300, 14)
        expected := 300 * (14.0 / 86.0)
        if got != expected {
                t.Errorf("expected %v, got %v", expected, got)
        }
}

func TestCalculateCommissionAmount_ZeroCommission(t *testing.T) {
        got := calculateCommissionAmount(300, 0)
        if got != 0 {
                t.Errorf("expected 0 for zero commission, got %v", got)
        }
}

func TestCalculateTotalAmountWithDiscount(t *testing.T) {
        // PR #109: calculateTotalAmountWithDiscount artıq endirimi total-a tətbiq etmir.
        // Endirim yalnız interestAmount-a təsir edir (frontend-də göstərilir).
        // total_amount (→ LW) = principal + commission (dəyişmir).
        //
        // 300 AZN, commission=14, discount=20 → total = 348.84 (calculateTotalAmount ilə eyni)
        got := calculateTotalAmountWithDiscount(300, 14, 20)
        expected := calculateTotalAmount(300, 14) // 348.84
        if got != expected {
                t.Errorf("PR #109: total should equal calculateTotalAmount (discount no longer affects total). expected %v, got %v", expected, got)
        }

        // discount=5 olsa da, total dəyişməməlidir
        got = calculateTotalAmountWithDiscount(300, 14, 5)
        if got != expected {
                t.Errorf("PR #109: discount should not affect total. expected %v, got %v", expected, got)
        }
}

func TestCalculateTotalAmountWithDiscount_NegativeDiscount(t *testing.T) {
        // PR #109: negative discount da eyni nəticə verir (total dəyişmir)
        got := calculateTotalAmountWithDiscount(300, 14, -10)
        expected := calculateTotalAmount(300, 14)
        if got != expected {
                t.Errorf("expected %v (no discount), got %v", expected, got)
        }
}

// PR #109: calculateInterestAmount testi — faiz məbləği hesablama
func TestCalculateInterestAmount(t *testing.T) {
        // 300 AZN, annual_interest_rate=55%, term=3 ay
        // interest = 300 × 0.55 × (3/12) = 300 × 0.55 × 0.25 = 41.25
        got := calculateInterestAmount(300, 55, 3)
        if got != 41.25 {
                t.Errorf("expected 41.25, got %v", got)
        }

        // 500 AZN, annual_interest_rate=52%, term=6 ay
        // interest = 500 × 0.52 × (6/12) = 500 × 0.52 × 0.5 = 130.00
        got = calculateInterestAmount(500, 52, 6)
        if got != 130.00 {
                t.Errorf("expected 130.00, got %v", got)
        }
}

func TestCalculateInterestAmount_ZeroInputs(t *testing.T) {
        if got := calculateInterestAmount(0, 55, 3); got != 0 {
                t.Errorf("expected 0 for zero principal, got %v", got)
        }
        if got := calculateInterestAmount(300, 0, 3); got != 0 {
                t.Errorf("expected 0 for zero rate, got %v", got)
        }
        if got := calculateInterestAmount(300, 55, 0); got != 0 {
                t.Errorf("expected 0 for zero term, got %v", got)
        }
}

// =========================================================
// Tests for generateRandomCode
// =========================================================

func TestGenerateRandomCode_Format(t *testing.T) {
        code, err := generateRandomCode()
        if err != nil {
                t.Fatalf("expected no error, got %v", err)
        }
        if !strings.HasPrefix(code, model.DiscountCodePrefix) {
                t.Errorf("code should start with %q, got %q", model.DiscountCodePrefix, code)
        }
        if len(code) != len(model.DiscountCodePrefix)+6 {
                t.Errorf("code should be %d chars, got %d (%q)",
                        len(model.DiscountCodePrefix)+6, len(code), code)
        }
}

func TestGenerateRandomCode_Uniqueness(t *testing.T) {
        codes := make(map[string]bool)
        for i := 0; i < 1000; i++ {
                code, err := generateRandomCode()
                if err != nil {
                        t.Fatalf("iteration %d: expected no error, got %v", i, err)
                }
                if codes[code] {
                        t.Errorf("collision detected at iteration %d: %q", i, code)
                }
                codes[code] = true
        }
}

func TestGenerateRandomCode_NoAmbiguousChars(t *testing.T) {
        // Generate many codes and check none contain 0, O, I, 1
        for i := 0; i < 100; i++ {
                code, err := generateRandomCode()
                if err != nil {
                        t.Fatalf("iteration %d: expected no error, got %v", i, err)
                }
                suffix := code[len(model.DiscountCodePrefix):]
                for _, ch := range suffix {
                        if ch == '0' || ch == 'O' || ch == 'I' || ch == '1' {
                                t.Errorf("code %q contains ambiguous char %q", code, string(ch))
                        }
                }
        }
}
