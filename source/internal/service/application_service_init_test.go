package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
)

// PR #477: KYC servis xətaları (AZMK down/timeout/5xx) ErrKycServiceUnavailable
// ilə wrap olunmalıdır — VerifyInitApplication bunu görüb imtina SMS-ini skip edir.
// Müştərinin 3 dəqiqə ərzində KYC-i təsdiq etməməsi sentinel ilə wrap OL MUR
// (müştəri tərəflidir, SMS gedir) — amma o yol 60×3san polling gözləyir və
// unit test-də praktik deyil (180 san), ona görə yalnız wrap-lanan yollar test edilir.

func newKycTestApp() *model.LoanApplication {
	return &model.LoanApplication{
		ID:             1,
		CustomerPIN:    "PIN1",
		CustomerSerial: "AA1234567",
		CustomerPhone:  "+994501234567",
		Status:         model.StatusPendingCustomer,
	}
}

// TestRunAzmkKycAndPartner_KycCreateErrorWrapped — KYC session yaradıla
// bilmədsə xəta sentinel ilə wrap olunmalıdır.
func TestRunAzmkKycAndPartner_KycCreateErrorWrapped(t *testing.T) {
	p := &fakeAzmkOnlineProvider{kycErr: errSomeAzmkFailure}
	svc := newCardsTestService(newCardsTestStore(), p)

	err := svc.runAzmkKycAndPartner(context.Background(), newKycTestApp())
	if err == nil {
		t.Fatal("expected error from KYC creation failure")
	}
	if !errors.Is(err, ErrKycServiceUnavailable) {
		t.Fatalf("expected ErrKycServiceUnavailable wrap, got: %v", err)
	}
}

// TestRunAzmkKycAndPartner_VerifyCallErrorWrapped — VerifyKYC çağırışı xəta
// qaytarsa (invalid ID / şəbəkə) xəta sentinel ilə wrap olunmalıdır.
func TestRunAzmkKycAndPartner_VerifyCallErrorWrapped(t *testing.T) {
	p := &fakeAzmkOnlineProvider{verifyKycErr: errSomeAzmkFailure}
	svc := newCardsTestService(newCardsTestStore(), p)

	err := svc.runAzmkKycAndPartner(context.Background(), newKycTestApp())
	if err == nil {
		t.Fatal("expected error from KYC verify call failure")
	}
	if !errors.Is(err, ErrKycServiceUnavailable) {
		t.Fatalf("expected ErrKycServiceUnavailable wrap, got: %v", err)
	}
}

// TestRunAzmkKycAndPartner_PartnerRegisterErrorWrapped — Partner qeydiyyatı
// xətası da sentinel ilə wrap olunmalıdır (texniki xəta → SMS yox).
func TestRunAzmkKycAndPartner_PartnerRegisterErrorWrapped(t *testing.T) {
	p := &fakeAzmkOnlineProvider{registerPartnerErr: errSomeAzmkFailure}
	svc := newCardsTestService(newCardsTestStore(), p)

	err := svc.runAzmkKycAndPartner(context.Background(), newKycTestApp())
	if err == nil {
		t.Fatal("expected error from partner registration failure")
	}
	if !errors.Is(err, ErrKycServiceUnavailable) {
		t.Fatalf("expected ErrKycServiceUnavailable wrap, got: %v", err)
	}
}
