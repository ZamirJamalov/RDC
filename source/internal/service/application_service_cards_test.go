package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// errSomeAzmkFailure simulates an AZMK outage in tests.
var errSomeAzmkFailure = errors.New("azmk: simulated outage")

// --- PR #313 tests: AZMK-da qeydiyyatda olan kartların siyahısı + seçim ---

// fakeAzmkOnlineProvider is a test-only implementation of azmk.Provider
// (OnlineLending) with configurable card list and call counters.
type fakeAzmkOnlineProvider struct {
	cards           []azmk.CardInfo
	cardsErr        error
	registerCardErr error // PR #350: yeni kart xətasını simulyasiya etmək üçün
	getCards        int
	registerCard    int
	createAppReq    *azmk.ApplicationCreateRequest // PR #353: son create request-i (assert üçün)
}

func (f *fakeAzmkOnlineProvider) KYC(context.Context, *azmk.KYCRequest) (string, error) {
	return "FAKE-KYC-1", nil
}
func (f *fakeAzmkOnlineProvider) VerifyKYC(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeAzmkOnlineProvider) RegisterPartner(context.Context, *azmk.PartnerRequest) (string, error) {
	return "FAKE-PARTNER-1", nil
}
func (f *fakeAzmkOnlineProvider) RegisterCard(context.Context, *azmk.CardRequest) (string, error) {
	f.registerCard++
	if f.registerCardErr != nil {
		return "", f.registerCardErr
	}
	return "FAKE-NEW-CARD-1", nil
}
func (f *fakeAzmkOnlineProvider) CreateApplication(_ context.Context, req *azmk.ApplicationCreateRequest) (string, error) {
	f.createAppReq = req
	return "FAKE-APP-1", nil
}
func (f *fakeAzmkOnlineProvider) GetApplicationStatus(context.Context, string) (*azmk.ApplicationStatus, error) {
	return &azmk.ApplicationStatus{LoanID: "FAKE-LOAN-1", Signed: true}, nil
}
func (f *fakeAzmkOnlineProvider) Disburse(context.Context, *azmk.DisburseRequest) error {
	return nil
}
func (f *fakeAzmkOnlineProvider) GetCards(_ context.Context, _ string) ([]azmk.CardInfo, error) {
	f.getCards++
	if f.cardsErr != nil {
		return nil, f.cardsErr
	}
	return f.cards, nil
}

// newCardsTestStore: app 1 = pending_customer (PublicID və PartnerID dolu).
// PR #313 (yenilənib): AZMK = həqiqət mənbəyi — köhnə kartların siyahısı
// lokal DB tarixçəsindən deyil, birbaşa AZMK-dan gəlir.
func newCardsTestStore() *mockApplicationStore {
	store := newConfirmStore()
	store.appByID[1].PublicID = "11111111-2222-3333-4444-555555555555"
	store.appByID[1].PartnerID = "PARTNER-1"
	return store
}

func newCardsTestService(store *mockApplicationStore, provider *fakeAzmkOnlineProvider) *ApplicationService {
	svc := newConfirmService(store, newConfirmLWProvider(), nil)
	svc.SetAzmkProvider(provider, "HO", "2030-01-01", "I10")
	return svc
}

// TestGetCustomerCards_ListsActiveCardsOnly — köhnə kart varsa AZMK-dan
// siyahı gəlir, bitmiş kartlar süzülür.
func TestGetCustomerCards_ListsActiveCardsOnly(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cards: []azmk.CardInfo{
		{ID: "CARD-1", Type: "CARD", Code: "****-****-****-5559", Expiring: "2030-01-01"},
		{ID: "CARD-EXPIRED", Type: "CARD", Code: "****-****-****-1111", Expiring: "2020-01-01"},
	}}
	svc := newCardsTestService(store, provider)

	cards, err := svc.GetCustomerCardsByPublicID(ctx, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 active card, got %d", len(cards))
	}
	if cards[0].ID != "CARD-1" || cards[0].Code != "****-****-****-5559" {
		t.Errorf("unexpected card: %+v", cards[0])
	}
	if provider.getCards != 1 {
		t.Errorf("expected 1 GetCards call, got %d", provider.getCards)
	}
}

// TestGetCustomerCards_EmptyFromAzmk — AZMK boş siyahı qaytararsa (yeni
// müştəri, kart heç qeyd edilməyib) UI yalnız yeni-kart daxiletmə göstərir.
// PR #313 (yenilənib): AZMK = həqiqət mənbəyi — siyahı hər zaman AZMK-dan
// sorğulanır, lokal DB ön-şərti yoxdur (drop-recreate problemi aradan qalxdı).
func TestGetCustomerCards_EmptyFromAzmk(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cards: []azmk.CardInfo{}}
	svc := newCardsTestService(store, provider)

	cards, err := svc.GetCustomerCardsByPublicID(ctx, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}
	if provider.getCards != 1 {
		t.Errorf("AZMK GetCards must be called exactly once, got %d calls", provider.getCards)
	}
}

// TestGetCustomerCards_AzmkError_FailSoft — AZMK xətası boş siyahı verir.
func TestGetCustomerCards_AzmkError_FailSoft(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cardsErr: errSomeAzmkFailure}
	svc := newCardsTestService(store, provider)

	cards, err := svc.GetCustomerCardsByPublicID(ctx, "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("fail-soft expected, got error: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards on AZMK error, got %d", len(cards))
	}
}

// TestCustomerConfirm_SelectedSavedCard — köhnə kart seçilib (PR #355):
// RegisterCard çağırılmır, seçilmiş kartın card_id-si birbaşa yazılır
// (create/disburse onu göndərir, PR #353), card_number maskalanır.
func TestCustomerConfirm_SelectedSavedCard(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cards: []azmk.CardInfo{
		{ID: "CARD-1", Type: "CARD", Code: "****-****-****-5559", Expiring: "2030-01-01"},
	}}
	svc := newCardsTestService(store, provider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		SelectedCardID:         "CARD-1",
		ActualAddress:          "Bakı, Nizami r., Murtuza Muxtarov 12",
		CardOwnershipConfirmed: true,
	}

	app, err := svc.CustomerConfirmApplication(ctx, 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.registerCard != 0 {
		t.Errorf("RegisterCard must NOT be called for a saved card (PR #355), called %d times", provider.registerCard)
	}
	if app.CardID != "CARD-1" {
		t.Errorf("CardID = %q, want %q (seçilmiş kartın ID-si birbaşa)", app.CardID, "CARD-1")
	}
	if app.CardNumber != "************5559" {
		t.Errorf("CardNumber = %q, want masked %q", app.CardNumber, "************5559")
	}
	if app.Status != model.StatusPendingExpert {
		t.Errorf("Status = %q, want pending_expert", app.Status)
	}
	// DB-yə də seçilmiş card_id yazılmalıdır (create PR #353 bunu göndərir).
	if got := store.appByID[1].CardID; got != "CARD-1" {
		t.Errorf("stored CardID = %q, want %q", got, "CARD-1")
	}
}

// TestCustomerConfirm_NewCardRegisterFails_Blocks — PR #350: yeni kartın
// qeydiyyatı xəta verərsə proses DAVAM ETMİR (fallback yoxdur) — confirm
// hard-fail olur, müştəriyə xəta qaytarılır.
func TestCustomerConfirm_NewCardRegisterFails_Blocks(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{registerCardErr: errSomeAzmkFailure}
	svc := newCardsTestService(store, provider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		CardNumber:             "4169741234567890",
		ActualAddress:          "Bakı, Nizami r., Murtuza Muxtarov 12",
		CardOwnershipConfirmed: true,
	}

	if _, err := svc.CustomerConfirmApplication(ctx, 1, req); err == nil {
		t.Fatal("expected error (process must NOT continue on card registration failure), got nil")
	}
	if provider.registerCard != 1 {
		t.Errorf("RegisterCard must be called once, called %d times", provider.registerCard)
	}
	// Müraciət dashboard-a getməməlidir: status pending_customer qalır.
	if got := store.appByID[1].Status; got != model.StatusPendingCustomer {
		t.Errorf("stored status = %q, want pending_customer (must NOT reach dashboard)", got)
	}
}

// TestCustomerConfirm_SelectedCardNotInList — siyahıda olmayan kart ID-si
// rədd edilməlidir (başqa partnerin kartı ilə disburse bloklanır).
func TestCustomerConfirm_SelectedCardNotInList(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cards: []azmk.CardInfo{
		{ID: "CARD-1", Type: "CARD", Code: "****-****-****-5559", Expiring: "2030-01-01"},
	}}
	svc := newCardsTestService(store, provider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		SelectedCardID:         "FOREIGN-CARD-9",
		ActualAddress:          "Bakı, Nizami r., Murtuza Muxtarov 12",
		CardOwnershipConfirmed: true,
	}

	if _, err := svc.CustomerConfirmApplication(ctx, 1, req); err == nil {
		t.Fatal("expected error for foreign card id, got nil")
	}
}

// TestCustomerConfirm_SelectedCardExpired — bitmiş kart seçilə bilməz.
func TestCustomerConfirm_SelectedCardExpired(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{cards: []azmk.CardInfo{
		{ID: "CARD-EXPIRED", Type: "CARD", Code: "****-****-****-1111", Expiring: "2020-01-01"},
	}}
	svc := newCardsTestService(store, provider)

	req := &CustomerConfirmRequest{
		Amount:                 200,
		SelectedCardID:         "CARD-EXPIRED",
		ActualAddress:          "Bakı, Nizami r., Murtuza Muxtarov 12",
		CardOwnershipConfirmed: true,
	}

	if _, err := svc.CustomerConfirmApplication(ctx, 1, req); err == nil {
		t.Fatal("expected error for expired card, got nil")
	}
}

// TestMaskCardCode — AZMK maskalı kodu 16 simvola sığır.
func TestMaskCardCode(t *testing.T) {
	got := maskCardCode("****-****-****-5559")
	if got != "************5559" {
		t.Errorf("maskCardCode = %q, want %q", got, "************5559")
	}
	if len(got) != 16 {
		t.Errorf("len = %d, want 16 (card_number VARCHAR(16) limiti)", len(got))
	}
}

// TestAzmkCreateApplication_SendsCardId — PR #353: application/create
// request-i cardId daşımalıdır (seçilmiş köhnə YA yeni register olunmuş kartın
// ID-si — hər halda app.CardID). Address (PR #348) və disbursementFee (PR #349)
// də yoxlanır.
func TestAzmkCreateApplication_SendsCardId(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{}
	svc := newCardsTestService(store, provider)

	app := store.appByID[1]
	app.CardID = "2093427826F74596A244DEF468068900"
	app.ApprovedRate = 11
	app.ActualAddress = "Bakı, Nizami r., Murtuza Muxtarov 12"

	if err := svc.azmkCreateApplication(ctx, app, 12.36); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.createAppReq == nil {
		t.Fatal("CreateApplication was not called")
	}
	if got := provider.createAppReq.LoanData.CardID; got != "2093427826F74596A244DEF468068900" {
		t.Errorf("cardId = %q, want %q", got, "2093427826F74596A244DEF468068900")
	}
	if got := provider.createAppReq.LoanData.Address; got != app.ActualAddress {
		t.Errorf("address = %q, want %q", got, app.ActualAddress)
	}
	if got := provider.createAppReq.LoanData.DisbursementFee; got != 0.11 {
		t.Errorf("disbursementFee = %v, want 0.11 (ApprovedRate/100)", got)
	}
}

// TestIsCardExpired — bitmə gününün özündə kart hələ etibarlıdır.
func TestIsCardExpired(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local)
	if isCardExpired("2030-01-01", now) {
		t.Error("future card must not be expired")
	}
	if !isCardExpired("2020-01-01", now) {
		t.Error("past card must be expired")
	}
	if isCardExpired("2026-08-26", now) {
		t.Error("card expiring today must still be valid")
	}
	if isCardExpired("bad-date", now) {
		t.Error("unparseable date must be treated as valid")
	}
}
