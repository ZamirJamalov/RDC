package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"rdc-source/internal/model"
)

// PR #450 (Docs/pre_referal_code_plan.md, plan R2 — owner benefit) testləri:
// kod sahibinin ÖZ aktiv referal kodu növbəti kreditinin approvu zamanı
// avtomatik tətbiq olunur; manual kod prioritetlidir; kod single-use-dur.
//
// Mock faiz: mockApplicationStore.GetCreditLevelInterestRate default 55.0%
// → interest = 300 × 55% × 3/12 = 41.25 AZN
// → 5% endirim = 2.06 AZN | 10% endirim = 4.13 AZN (round yuxarı)

// ownerReferralTestApp — approve gözləyən yeni müraciət (ID=50).
// referralTestApp()-dən fərqli olaraq pending_expert statusundadır və
// faiz hesabı üçün lazımi sahələr doludur.
func ownerReferralTestApp() *model.LoanApplication {
	app := referralTestApp()
	app.ID = 50
	app.Status = model.StatusPendingExpert
	app.CreditLevel = model.CreditLevelNew
	app.Amount = 300
	app.TermMonths = 3
	app.ApprovedRate = 11
	app.DiscountCode = ""
	return app
}

// ownActiveCode — customer-ə aid aktiv referal kodu (fromAppID = kodun
// yarandığı ƏVVƏLKİ müraciət; owner benefit məhz sonrakı müraciətdə verilir).
func ownActiveCode(codeID, customerID, fromAppID int, value float64) *model.DiscountCode {
	return &model.DiscountCode{
		ID:                      codeID,
		Code:                    fmt.Sprintf("ALPUL-OWN%d", codeID),
		IssuedToCustomerID:      customerID,
		IssuedFromApplicationID: &fromAppID,
		DiscountType:            model.DiscountTypePercent,
		DiscountValue:           value,
		Status:                  model.DiscountStatusActive,
	}
}

// TestApplyOwnerReferralDiscount_HappyPath — plan R2 əsas ssenarisi:
// müştərinin öz aktiv kodu var (əvvəlki müraciət #42-dən, disburse-da yaranıb),
// yeni müraciəti (#50) approve olunur → faizdən endirim hesablanır.
// Kodun ÖZÜ hələ used olmamalıdır — mark-used UpdateStatus-ın işidir.
func TestApplyOwnerReferralDiscount_HappyPath(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 42, 5) // 5% kod, əvvəlki müraciət #42-dən
	discountStore.codes[dc.Code] = dc

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if code != dc.Code {
		t.Fatalf("expected applied code %q, got %q", dc.Code, code)
	}
	// interest = 300 × 55% × 3/12 = 41.25 → 5% = 2.06
	if amount != 2.06 {
		t.Errorf("expected discount 2.06, got %v", amount)
	}
	// Kod bu mərhələdə aktiv qalmalıdır — used yalnız approve-da işarələnir
	if dc.Status != model.DiscountStatusActive {
		t.Errorf("code must stay active until approval marks it used, got %q", dc.Status)
	}
}

// TestApplyOwnerReferralDiscount_NoCustomer — customers row yoxdursa
// (müştərinin heç vaxt disburse olunmuş krediti olmayıb → kod da yoxdur)
// owner benefit yoxdur, xəta YOX.
func TestApplyOwnerReferralDiscount_NoCustomer(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = nil // müşteri tapılmadı

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if amount != 0 || code != "" {
		t.Errorf("expected no discount for missing customer, got (%v, %q)", amount, code)
	}
}

// TestApplyOwnerReferralDiscount_NoCode — müşteri var, amma kodu yoxdur →
// normal hal, (0, "").
func TestApplyOwnerReferralDiscount_NoCode(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	// discountStore boşdur

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if amount != 0 || code != "" {
		t.Errorf("expected no discount without codes, got (%v, %q)", amount, code)
	}
}

// TestApplyOwnerReferralDiscount_UsedCode — kod artıq istifadə olunubsa
// (redeemer tərəfindən xərclənibsə) owner benefit yoxdur — single-use.
func TestApplyOwnerReferralDiscount_UsedCode(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 42, 5)
	dc.Status = model.DiscountStatusUsed // dostu artıq istifadə edib
	discountStore.codes[dc.Code] = dc

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if amount != 0 || code != "" {
		t.Errorf("expected no discount for used code, got (%v, %q)", amount, code)
	}
}

// TestApplyOwnerReferralDiscount_ExpiredCode — status hələ 'active' olsa
// belə valid_until keçibsə kod keçərsizdir.
func TestApplyOwnerReferralDiscount_ExpiredCode(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 42, 5)
	past := time.Now().Add(-24 * time.Hour)
	dc.ValidUntil = &past
	discountStore.codes[dc.Code] = dc

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if amount != 0 || code != "" {
		t.Errorf("expected no discount for expired code, got (%v, %q)", amount, code)
	}
}

// TestApplyOwnerReferralDiscount_SameApplication — kod məhz bu müraciətin
// özündən yaranıbsa tətbiq edilmir (defensive — praktikada generasiya
// disburse-da, approve-dan SONRA olur, ona görə bu hal mümkün deyil).
func TestApplyOwnerReferralDiscount_SameApplication(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp() // ID = 50
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 50, 5) // fromAppID == app.ID
	discountStore.codes[dc.Code] = dc

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if amount != 0 || code != "" {
		t.Errorf("expected no discount for same-application code, got (%v, %q)", amount, code)
	}
}

// TestApplyOwnerReferralDiscount_NewestActiveWins — bir neçə aktiv kod
// varsa (müşterinin 2 disburse olunmuş krediti var) ən YENİSİ seçilir.
func TestApplyOwnerReferralDiscount_NewestActiveWins(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()
	svc := newReferralTestService(discountStore, customerStore, &recordingSMSProvider{}, 5)

	app := ownerReferralTestApp()
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}

	old := ownActiveCode(1, 100, 42, 3) // köhnə kod: 3%
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	newer := ownActiveCode(2, 100, 43, 6) // yeni kod: 6%
	newer.CreatedAt = time.Now().Add(-1 * time.Hour)
	discountStore.codes[old.Code] = old
	discountStore.codes[newer.Code] = newer

	amount, code := svc.applyOwnerReferralDiscount(ctx, app)

	if code != newer.Code {
		t.Fatalf("expected newest code %q, got %q", newer.Code, code)
	}
	// interest = 41.25 → 6% = 2.475 → round → 2.48
	if amount != 2.48 {
		t.Errorf("expected discount 2.48 (6%% of newest code), got %v", amount)
	}
}

// newOwnerReferralUpdateStatusService — UpdateStatus səviyyəli testlər üçün
// servis qurur və mock store-ə reference qaytarır (appByID assertion üçün).
func newOwnerReferralUpdateStatusService(t *testing.T, discountStore *mockDiscountCodeStore, customerStore *mockCustomerStore, app *model.LoanApplication) (*ApplicationService, *mockApplicationStore) {
	t.Helper()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), customerStore, NewOTPService(nil, nil))
	svc.SetDiscountService(NewDiscountCodeService(discountStore))
	svc.smsProvider = &recordingSMSProvider{}
	store.appByID[app.ID] = app
	return svc, store
}

// TestUpdateStatus_Approve_OwnerReferralAutoApplied — plan R2 qəbul meyarı:
// "Owner növbəti kreditində endirim alır". Manual kod yazılmayıb, owner-ın
// öz aktiv kodu var → approve zamanı endirim AVTOMATİK tətbiq olunur,
// discount_amount DB-yə yazılır, kod used olur (single-use).
func TestUpdateStatus_Approve_OwnerReferralAutoApplied(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()

	app := ownerReferralTestApp()
	app.DiscountCode = "" // manual kod YOXDUR → R2 avtomatik yolu
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 42, 5)
	discountStore.codes[dc.Code] = dc

	svc, store := newOwnerReferralUpdateStatusService(t, discountStore, customerStore, app)

	updated, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: model.CreditLevelNew,
	})
	if err != nil {
		t.Fatalf("UpdateStatus approve failed: %v", err)
	}
	if updated.Status != model.StatusApproved {
		t.Fatalf("expected status approved, got %q", updated.Status)
	}

	// Endirim müraciətə yazıldı — kod kimi owner-ın ÖZ kodu qeyd olunur
	stored := store.appByID[app.ID]
	if stored.DiscountCode != dc.Code {
		t.Errorf("expected stored discount_code %q (owner's own), got %q", dc.Code, stored.DiscountCode)
	}
	if stored.DiscountAmount == nil || *stored.DiscountAmount != 2.06 {
		t.Errorf("expected stored discount_amount 2.06, got %v", stored.DiscountAmount)
	}

	// Kod single-use: approve-la birlikdə bağlandı
	if dc.Status != model.DiscountStatusUsed {
		t.Errorf("expected owner's code marked used, got %q", dc.Status)
	}
	if dc.UsedByApplicationID == nil || *dc.UsedByApplicationID != app.ID {
		t.Errorf("expected used_by_application_id=%d, got %v", app.ID, dc.UsedByApplicationID)
	}
}

// TestUpdateStatus_Approve_ManualCodePrecedence — bir müraciətə yalnız BİR
// endirim: müştəri apply-də başqasının kodunu yazıbsa manual kod prioritetlidir,
// owner-ın öz kodu AVTOMATİK tətbiq OLUNMUR və aktiv qalır (gələcək kredit
// üçün və ya dostu üçün saxlanılır).
func TestUpdateStatus_Approve_ManualCodePrecedence(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()

	app := ownerReferralTestApp()
	app.DiscountCode = "ALPUL-FRIEND" // manual: dostunun kodu (10%)
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}

	friend := ownActiveCode(9, 200, 30, 10) // owner: customer 200
	friend.Code = "ALPUL-FRIEND"
	own := ownActiveCode(1, 100, 42, 5) // owner-ın öz kodu (5%)
	discountStore.codes[friend.Code] = friend
	discountStore.codes[own.Code] = own

	svc, store := newOwnerReferralUpdateStatusService(t, discountStore, customerStore, app)

	_, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: model.CreditLevelNew,
	})
	if err != nil {
		t.Fatalf("UpdateStatus approve failed: %v", err)
	}

	// Manual kod tətbiq olundu: 41.25 × 10% = 4.125 → 4.13
	stored := store.appByID[app.ID]
	if stored.DiscountCode != "ALPUL-FRIEND" {
		t.Errorf("expected stored discount_code ALPUL-FRIEND (manual wins), got %q", stored.DiscountCode)
	}
	if stored.DiscountAmount == nil || *stored.DiscountAmount != 4.13 {
		t.Errorf("expected stored discount_amount 4.13, got %v", stored.DiscountAmount)
	}

	// Dostun kodu istifadə olundu, owner-ın öz kodu TOXUNULMAZ qaldı
	if friend.Status != model.DiscountStatusUsed {
		t.Errorf("expected friend's code used, got %q", friend.Status)
	}
	if own.Status != model.DiscountStatusActive {
		t.Errorf("owner's own code must stay active (manual precedence), got %q", own.Status)
	}
}

// TestUpdateStatus_Reject_DoesNotConsumeOwnerCode — imtina olunan müraciət
// kodu yandırmır: reject zamanı nə endirim yazılır, nə də kod used olur.
// Müştəri yenidən müraciət etdikdə owner benefit yenidən işləyəcək.
func TestUpdateStatus_Reject_DoesNotConsumeOwnerCode(t *testing.T) {
	ctx := context.Background()
	discountStore := newMockDiscountCodeStore()
	customerStore := newMockCustomerStore()

	app := ownerReferralTestApp()
	app.DiscountCode = ""
	customerStore.getByPINCustomer = &model.Customer{ID: 100, CustomerPIN: app.CustomerPIN}
	dc := ownActiveCode(1, 100, 42, 5)
	discountStore.codes[dc.Code] = dc

	svc, store := newOwnerReferralUpdateStatusService(t, discountStore, customerStore, app)

	_, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_TEST: imtina",
	})
	if err != nil {
		t.Fatalf("UpdateStatus reject failed: %v", err)
	}

	stored := store.appByID[app.ID]
	if stored.DiscountCode != "" {
		t.Errorf("expected no discount_code on rejection, got %q", stored.DiscountCode)
	}
	if stored.DiscountAmount != nil {
		t.Errorf("expected no discount_amount on rejection, got %v", *stored.DiscountAmount)
	}
	if dc.Status != model.DiscountStatusActive {
		t.Errorf("owner's code must stay active after rejection, got %q", dc.Status)
	}
}
