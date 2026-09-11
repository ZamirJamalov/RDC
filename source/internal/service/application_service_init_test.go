package service

import (
	"context"
	"errors"
	"testing"
	"time"

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

// PR #496: client disconnect (context.Canceled) KYC xətası DEYİL — xəta raw
// qaytarılmalıdır ki, VerifyInitApplication errors.Is ilə tanıyıb müraciəti
// rejected ETMƏSİN (pending_customer qalır, imtina SMS-i getmir).
func TestRunAzmkKycAndPartner_ClientDisconnectNotWrapped(t *testing.T) {
	p := &fakeAzmkOnlineProvider{verifyKycErr: context.Canceled}
	svc := newCardsTestService(newCardsTestStore(), p)

	err := svc.runAzmkKycAndPartner(context.Background(), newKycTestApp())
	if err == nil {
		t.Fatal("expected error from client disconnect")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled identity, got: %v", err)
	}
	if errors.Is(err, ErrKycServiceUnavailable) {
		t.Fatalf("disconnect must NOT be wrapped as ErrKycServiceUnavailable: %v", err)
	}
}

// PR #496: KYC gözləməsi (3 san polling intervalı) zamanı disconnect olanda
// funksiya dərhal çıxmalıdır — növbəti polling-i gözləməməli, ErrKycServiceUnavailable
// ilə wrap etməməlidir.
func TestRunAzmkKycAndPartner_DisconnectDuringWaitReturnsFast(t *testing.T) {
	// VerifyKYC (false, nil) qaytarır — status hələ SENT, polling davam edərdi.
	// ctx isə artıq ləğv edilib (müştəri gedib).
	p := &fakeAzmkOnlineProvider{verifyKycNotVerified: true}
	svc := newCardsTestService(newCardsTestStore(), p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // disconnect simulyasiyası — müştəri bağlantını kəsib

	start := time.Now()
	err := svc.runAzmkKycAndPartner(ctx, newKycTestApp())
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if errors.Is(err, ErrKycServiceUnavailable) {
		t.Fatalf("disconnect must NOT be wrapped as ErrKycServiceUnavailable: %v", err)
	}
	// 3 san sleep gözləməməli — deməli < 2 san
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("disconnect detected too slowly: %v", elapsed)
	}
}
