package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// PR #245: UpdateActualAddress — ekspert faktiki ünvan redaktəsi.

func newAddressTestService(store *mockApplicationStore) *ApplicationService {
	return NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
}

func TestUpdateActualAddress_Valid(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval, ActualAddress: "köhnə ünvan"}
	svc := newAddressTestService(store)

	app, err := svc.UpdateActualAddress(context.Background(), 1, "  BAKI ŞƏHƏRİ, NİZAMİ RAYONU  ")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	// Trim tətbiq olunmalıdır
	if app.ActualAddress != "BAKI ŞƏHƏRİ, NİZAMİ RAYONU" {
		t.Fatalf("expected trimmed address, got %q", app.ActualAddress)
	}
	if got := store.appByID[1].ActualAddress; got != "BAKI ŞƏHƏRİ, NİZAMİ RAYONU" {
		t.Fatalf("expected persisted address, got %q", got)
	}
}

func TestUpdateActualAddress_Empty(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval}
	svc := newAddressTestService(store)

	if _, err := svc.UpdateActualAddress(context.Background(), 1, "   "); err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestUpdateActualAddress_TooLong(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusPendingApproval}
	svc := newAddressTestService(store)

	if _, err := svc.UpdateActualAddress(context.Background(), 1, strings.Repeat("a", 501)); err == nil {
		t.Fatal("expected error for >500 chars")
	}
	if _, err := svc.UpdateActualAddress(context.Background(), 1, strings.Repeat("a", 500)); err != nil {
		t.Fatalf("500 chars should be allowed, got: %v", err)
	}
}

func TestUpdateActualAddress_FinalStatus(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, Status: model.StatusApproved, ActualAddress: "köhnə"}
	svc := newAddressTestService(store)

	if _, err := svc.UpdateActualAddress(context.Background(), 1, "yeni"); err == nil {
		t.Fatal("expected error for approved application")
	}
	store.appByID[1].Status = model.StatusRejected
	if _, err := svc.UpdateActualAddress(context.Background(), 1, "yeni"); err == nil {
		t.Fatal("expected error for rejected application")
	}
	// Dəyişməməlidir
	if got := store.appByID[1].ActualAddress; got != "köhnə" {
		t.Fatalf("address must not change after final decision, got %q", got)
	}
}

func TestUpdateActualAddress_NotFound(t *testing.T) {
	svc := newAddressTestService(newMockStore())
	if _, err := svc.UpdateActualAddress(context.Background(), 999, "yeni"); err == nil {
		t.Fatal("expected error for missing application")
	}
}

// PR #245: BackfillRegistrationAddress — köhnə müraciət üçün AZMK-dən doldurma.

func TestBackfillRegistrationAddress_FromAzmk(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123", CustomerSerial: "AA12345", CustomerFullName: ""}

	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{
			Surname:             "Əliyev",
			Name:                "Əli",
			Patronymic:          "Əli oğlu",
			RegistrationAddress: "BAKI ŞƏHƏRİ, XƏTAİ RAYONU, TUSİ KÜÇƏSİ, EV 55, MƏNZİL 3",
		},
	}
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	app, err := svc.BackfillRegistrationAddress(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if app.RegistrationAddress != "BAKI ŞƏHƏRİ, XƏTAİ RAYONU, TUSİ KÜÇƏSİ, EV 55, MƏNZİL 3" {
		t.Fatalf("expected registration address from AZMK, got %q", app.RegistrationAddress)
	}
	if got := store.appByID[1].RegistrationAddress; got != app.RegistrationAddress {
		t.Fatalf("expected persisted registration address, got %q", got)
	}
	// Bir AZMK çağırışı — dublikat yox (PR #243 prinsipi)
	if provider.personalCalls != 1 {
		t.Fatalf("expected exactly 1 GetPersonalInfo call, got %d", provider.personalCalls)
	}
}

func TestBackfillRegistrationAddress_AlreadySet_NoAzmkCall(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123", RegistrationAddress: "artıq var"}

	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{RegistrationAddress: "dəyişik olmamalı"},
	}
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	app, err := svc.BackfillRegistrationAddress(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if app.RegistrationAddress != "artıq var" {
		t.Fatalf("expected existing address unchanged, got %q", app.RegistrationAddress)
	}
	// Artıq saxlanılıbsa AZMK çağırılmamalıdır
	if provider.personalCalls != 0 {
		t.Fatalf("expected 0 GetPersonalInfo calls, got %d", provider.personalCalls)
	}
}

func TestBackfillRegistrationAddress_AzmkError_FailSoft(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123"}

	provider := &mockAzmkCustomerData{personalErr: errors.New("AZMK CustomerDataService unreachable")}
	svc := newAddressTestService(store)
	svc.SetCustomerDataProvider(provider)

	app, err := svc.BackfillRegistrationAddress(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected fail-soft success, got error: %v", err)
	}
	if app.RegistrationAddress != "" {
		t.Fatalf("expected empty address on AZMK error, got %q", app.RegistrationAddress)
	}
}

func TestBackfillRegistrationAddress_NoProvider(t *testing.T) {
	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{ID: 1, CustomerPIN: "1ABC123"}
	svc := newAddressTestService(store) // customerDataProvider nil

	app, err := svc.BackfillRegistrationAddress(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success (fail-soft), got error: %v", err)
	}
	if app.RegistrationAddress != "" {
		t.Fatalf("expected empty address (no provider), got %q", app.RegistrationAddress)
	}
}
