package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// PR #487: runIdentityGate — identiklik qapısı testləri.
// Qapı OTP-dən sonra, KYC-dən əvvəl işləyir: serial + yaş (18-69) yoxlaması.

var errSomeGate = errors.New("azmk unavailable")

func newGateTestService(store *mockApplicationStore) *ApplicationService {
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	svc.SetCutoffChecksEnabled(true) // qapı cutoff flag-i altındadır
	return svc
}

func newGateApp() *model.LoanApplication {
	return &model.LoanApplication{
		ID:             1,
		CustomerPIN:    "1ABC123",
		CustomerSerial: "AZE1234567",
		Status:         model.StatusPendingCustomer,
	}
}

// Uyğun seriya + normal yaş → qapı keçilir, ad/ünvan saxlanılır.
func TestIdentityGate_Pass(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			Surname:             "Əliyev",
			Name:                "Əli",
			DocumentSeriaNumber: "AZE1234567",
			BirthDate:           "1993-08-09",
			RegistrationAddress: "BAKI ŞƏHƏRİ, XƏTAİ RAYONU",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected gate pass, got rejection %q", reason)
	}
	// PR #243/#245: ad və ünvan qapıda saxlanılmalıdır
	if got := store.appByID[1].CustomerFullName; got == "" {
		t.Fatal("expected customer full name to be saved at the gate")
	}
	if got := store.appByID[1].RegistrationAddress; got != "BAKI ŞƏHƏRİ, XƏTAİ RAYONU" {
		t.Fatalf("expected registration address saved, got %q", got)
	}
}

// Sehv seriya → SERIAL_MISMATCH rəddi.
func TestIdentityGate_SerialMismatch(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "AZE7654321", // daxil ediləndən fərqli
			BirthDate:           "1993-08-09",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "SERIAL_MISMATCH" {
		t.Fatalf("expected SERIAL_MISMATCH, got %q", reason)
	}
}

// Serial boş olanda mismatch yoxlaması skip (mövcud PR #486 semantikası).
func TestIdentityGate_EmptySerialSkip(t *testing.T) {
	store := newMockStore()
	app := newGateApp()
	app.CustomerSerial = ""
	store.appByID[1] = app
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "AZE7654321",
			BirthDate:           "1993-08-09",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected pass with empty serial, got %q", reason)
	}
}

// Yaş 17 → AGE_UNDER_18.
func TestIdentityGate_AgeUnder18(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "AZE1234567",
			BirthDate:           "2010-01-01",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AGE_UNDER_18" {
		t.Fatalf("expected AGE_UNDER_18, got %q", reason)
	}
}

// Yaş 70 → AGE_OVER_69.
func TestIdentityGate_AgeOver69(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "AZE1234567",
			BirthDate:           "1950-01-01",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AGE_OVER_69" {
		t.Fatalf("expected AGE_OVER_69, got %q", reason)
	}
}

// Boş/xətalı BirthDate → Age()=0 → AGE_UNDER_18 kimi rədd (strict).
func TestIdentityGate_EmptyBirthDateRejected(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "AZE1234567",
			BirthDate:           "", // boş
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AGE_UNDER_18" {
		t.Fatalf("expected AGE_UNDER_18 for empty birthdate, got %q", reason)
	}
}

// AZMK texniki xətası (data=nil) → fail-soft, qapı keçilir.
func TestIdentityGate_ServiceErrorFailSoft(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalErr: errSomeGate,
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected fail-soft pass on service error, got %q", reason)
	}
}

// Provider nil → qapı skip.
func TestIdentityGate_ProviderNilSkip(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := newGateTestService(store) // provider set olunmur

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected skip with nil provider, got %q", reason)
	}
}

// Cutoff-lar deaktiv → qapı skip.
func TestIdentityGate_CutoffsDisabledSkip(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = newGateApp()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	svc.SetCutoffChecksEnabled(false)
	svc.SetCustomerDataProvider(&mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			DocumentSeriaNumber: "TOTALLY-DIFFERENT",
			BirthDate:           "1950-01-01",
		},
	})

	reason, err := svc.runIdentityGate(context.Background(), newGateApp())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("expected skip when cutoffs disabled, got %q", reason)
	}
}
