package service

import (
	"context"
	"strings"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/pkg/otp"
)

// recordingSMSProvider — test-only otp.Provider: Send çağırışlarını qeyd edir.
type recordingSMSProvider struct {
	sends []struct {
		Phone   string
		Message string
	}
	sendErr error
}

func (p *recordingSMSProvider) Send(_ context.Context, phone, message string) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.sends = append(p.sends, struct {
		Phone   string
		Message string
	}{Phone: phone, Message: message})
	return nil
}

func (p *recordingSMSProvider) Name() string { return "recording" }

// newReferralTestService — PR #319 testləri üçün ApplicationService qurur.
func newReferralTestService(discountStore *mockDiscountCodeStore, customerStore *mockCustomerStore, sms *recordingSMSProvider, percent int) *ApplicationService {
	svc := NewApplicationService(newMockStore(), NewCreditEngine(newMockLWProvider(), newMockStore()), customerStore, NewOTPService(nil, nil))
	svc.SetDiscountService(NewDiscountCodeService(discountStore))
	svc.SetReferralDiscountPercent(percent)
	svc.smsProvider = sms
	return svc
}

func referralTestApp() *model.LoanApplication {
	return &model.LoanApplication{
		ID:               42,
		CustomerPIN:      "1SBK08P",
		CustomerFullName: "Zamir Camalov Zeynal oğlu",
		CustomerPhone:    "+994552077228",
		Status:           model.StatusDisbursed,
	}
}

// TestReferralOnDisburseSuccess_HappyPath — plan R1+R4:
// disburse success → customers row yaranır (GetOrCreate), kod generasiya olunur
// və store edilir (discount_value = REFERRAL_DISCOUNT_PERCENT), SMS gedir və
// SMS-in içində generasiya olunmuş kod var (sıralama R1 → R4).
func TestReferralOnDisburseSuccess_HappyPath(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	sms := &recordingSMSProvider{}

	svc := newReferralTestService(discountStore, customerStore, sms, 7)
	app := referralTestApp()
	svc.referralOnDisburseSuccess(ctx, app)

	// R1: customers row yarıadıldı (prerequisite fix — init axını yaratmır)
	if len(customerStore.createdCustomers) != 1 {
		t.Fatalf("expected 1 GetOrCreate call, got %d", len(customerStore.createdCustomers))
	}
	created := customerStore.createdCustomers[0]
	if created.CustomerPIN != app.CustomerPIN || created.FullName != app.CustomerFullName || created.Phone != app.CustomerPhone {
		t.Errorf("customer created with wrong fields: %+v", created)
	}

	// R1: kod store edildi və discount_value = referralDiscountPercent
	// (mock GetOrCreate ID-ni len(createdCustomers) kimi təyin edir → 1)
	if len(discountStore.createdCodes) != 1 {
		t.Fatalf("expected 1 generated code, got %d", len(discountStore.createdCodes))
	}
	code := discountStore.createdCodes[0]
	if code.IssuedFromApplicationID == nil || *code.IssuedFromApplicationID != app.ID {
		t.Errorf("code not bound to application %d: %+v", app.ID, code)
	}
	if code.IssuedToCustomerID != len(customerStore.createdCustomers) {
		t.Errorf("code owner %d != created customer id %d", code.IssuedToCustomerID, len(customerStore.createdCustomers))
	}
	if code.DiscountType != model.DiscountTypePercent || code.DiscountValue != 7 {
		t.Errorf("expected percent code with value 7, got type=%s value=%v", code.DiscountType, code.DiscountValue)
	}

	// R4: SMS getdi, içində kod var, faiz kodun dəyərindəndir
	if len(sms.sends) != 1 {
		t.Fatalf("expected 1 SMS, got %d", len(sms.sends))
	}
	if sms.sends[0].Phone != app.CustomerPhone {
		t.Errorf("SMS sent to wrong phone: %s", sms.sends[0].Phone)
	}
	if !strings.Contains(sms.sends[0].Message, code.Code) {
		t.Errorf("SMS does not contain generated code %q: %q", code.Code, sms.sends[0].Message)
	}
	if !strings.Contains(sms.sends[0].Message, "7%") {
		t.Errorf("SMS does not contain 7%%: %q", sms.sends[0].Message)
	}
}

// TestReferralOnDisburseSuccess_Idempotent — plan R1 idempotentlik:
// eyni application üçün təkrar çağırış yeni kod YARATMIR, mövcud kodu reuse edir.
func TestReferralOnDisburseSuccess_Idempotent(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	svc := newReferralTestService(discountStore, newMockCustomerStore(), &recordingSMSProvider{}, 5)
	app := referralTestApp()

	svc.referralOnDisburseSuccess(ctx, app)
	svc.referralOnDisburseSuccess(ctx, app)

	if len(discountStore.createdCodes) != 1 {
		t.Fatalf("expected exactly 1 generated code after 2 calls, got %d", len(discountStore.createdCodes))
	}
}

// TestReferralOnDisburseSuccess_DiscountSvcNil — discountSvc nil olanda:
// panik yoxdur, kod/SMS yoxdur (non-fatal).
func TestReferralOnDisburseSuccess_DiscountSvcNil(t *testing.T) {
	ctx := context.Background()
	sms := &recordingSMSProvider{}
	svc := newReferralTestService(newMockDiscountCodeStore(), newMockCustomerStore(), sms, 5)
	svc.discountSvc = nil

	svc.referralOnDisburseSuccess(ctx, referralTestApp())

	if len(sms.sends) != 0 {
		t.Errorf("expected no SMS when discountSvc is nil, got %d", len(sms.sends))
	}
}

// TestReferralOnDisburseSuccess_SMSFailureNonFatal — SMS xətası disburse-u
// pozmur (funksiya paniksız qayıdır; kod artıq store olunub).
func TestReferralOnDisburseSuccess_SMSFailureNonFatal(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	sms := &recordingSMSProvider{sendErr: context.DeadlineExceeded}
	svc := newReferralTestService(discountStore, newMockCustomerStore(), sms, 5)

	svc.referralOnDisburseSuccess(ctx, referralTestApp())

	if len(discountStore.createdCodes) != 1 {
		t.Errorf("expected code to be stored despite SMS failure, got %d", len(discountStore.createdCodes))
	}
}

// TestGenerateForApplicationWithValue — discount_value parametri düzgün yazılır;
// <= 0 olduqda DefaultDiscountValue fallback.
func TestGenerateForApplicationWithValue(t *testing.T) {
	ctx := context.Background()
	store := newMockDiscountCodeStore()
	svc := NewDiscountCodeService(store)

	code, err := svc.GenerateForApplicationWithValue(ctx, 1, 100, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code.DiscountValue != 9 {
		t.Errorf("expected discount_value 9, got %v", code.DiscountValue)
	}

	code2, err := svc.GenerateForApplicationWithValue(ctx, 2, 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code2.DiscountValue != DefaultDiscountValue {
		t.Errorf("expected fallback to DefaultDiscountValue (%v), got %v", DefaultDiscountValue, code2.DiscountValue)
	}
}

// compile-time check: recordingSMSProvider otp.Provider interfeysini realizə edir.
var _ otp.Provider = (*recordingSMSProvider)(nil)

// TestSendRejectionSMS — PR #362: reject zamanı müştəriyə imtina SMS-i gedir.
func TestSendRejectionSMS(t *testing.T) {
	ctx := context.Background()
	sms := &recordingSMSProvider{}
	svc := newReferralTestService(newMockDiscountCodeStore(), newMockCustomerStore(), sms, 5)

	app := referralTestApp()
	svc.sendRejectionSMS(ctx, app)

	if len(sms.sends) != 1 {
		t.Fatalf("expected 1 rejection SMS, got %d", len(sms.sends))
	}
	if sms.sends[0].Phone != app.CustomerPhone {
		t.Errorf("phone = %q, want %q", sms.sends[0].Phone, app.CustomerPhone)
	}
	if !strings.Contains(sms.sends[0].Message, "təsdiq olunmadı") {
		t.Errorf("message must contain rejection text, got: %s", sms.sends[0].Message)
	}
}

// TestSendRejectionSMS_NoPhone — PR #362: telefon boşdursa SMS getmir (non-fatal).
func TestSendRejectionSMS_NoPhone(t *testing.T) {
	ctx := context.Background()
	sms := &recordingSMSProvider{}
	svc := newReferralTestService(newMockDiscountCodeStore(), newMockCustomerStore(), sms, 5)

	app := referralTestApp()
	app.CustomerPhone = ""
	svc.sendRejectionSMS(ctx, app)

	if len(sms.sends) != 0 {
		t.Errorf("expected no SMS when phone is empty, got %d", len(sms.sends))
	}
}
