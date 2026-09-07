package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rdc-source/internal/model"
)

// --- PR #404 tests: LW partner phones (approve axınında, create-dən əvvəl) ---

// TestPhoneDescription_ConcatAndTruncate — qohumluq dərəcəsi + ad + qeyd
// birləşdirilir, boş hissələr ötürülür, maksimum 100 rune kəsilir.
func TestPhoneDescription_ConcatAndTruncate(t *testing.T) {
	// Tam üç hissə
	got := phoneDescription("Atası", "Zamir", "Zəng olundu, müsbət rəy")
	want := "Atası | Zamir | Zəng olundu, müsbət rəy"
	if got != want {
		t.Errorf("description = %q, want %q", got, want)
	}

	// Qeyd boşdursa separator da düşməlidir
	got = phoneDescription("Qardaşı", "Elnur", "")
	if got != "Qardaşı | Elnur" {
		t.Errorf("description without note = %q", got)
	}

	// Hamı boşdursa boş sətir
	if got := phoneDescription("  ", "", ""); got != "" {
		t.Errorf("empty description = %q, want empty", got)
	}

	// 100 rune limiti (çoxbaytlı Azərbaycan hərfləri ilə)
	long := strings.Repeat("ə", 150)
	got = phoneDescription(long, "", "")
	if r := len([]rune(got)); r != 100 {
		t.Errorf("truncated length = %d runes, want 100", r)
	}
}

// TestPartnerPhoneEntries_SkipsEmptyAndStripsSpaces — boş nömrəli kontaktlar
// siyahıya düşmür, nömrədəki boşluqlar silinir.
func TestPartnerPhoneEntries_SkipsEmptyAndStripsSpaces(t *testing.T) {
	app := &model.LoanApplication{
		Contact1Phone:    "+994 55 111 00 11",
		Contact1Relation: "Atası",
		Contact1Name:     "Zamir",
		Contact2Phone:    "", // boş — ötürülür
		Contact3Phone:    "+994551110033",
		Contact3Relation: "Dostu",
		Contact3CallNote: "çağırılmayıb",
	}

	entries := PartnerPhoneEntries(app)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (contact2 empty), got %d", len(entries))
	}
	if entries[0].Number != "+994551110011" {
		t.Errorf("entry0 number = %q, want %q (spaces stripped)", entries[0].Number, "+994551110011")
	}
	if entries[0].Description != "Atası | Zamir" {
		t.Errorf("entry0 description = %q", entries[0].Description)
	}
	if entries[1].Number != "+994551110033" {
		t.Errorf("entry1 number = %q", entries[1].Number)
	}
	if entries[1].Description != "Dostu | çağırılmayıb" {
		t.Errorf("entry1 description = %q", entries[1].Description)
	}
}

// TestPartnerPhoneEntries_AllEmpty — hamı boş olanda boş siyahı qayıdır
// (sendPartnerPhones bunu xətaya çevirir).
func TestPartnerPhoneEntries_AllEmpty(t *testing.T) {
	entries := PartnerPhoneEntries(&model.LoanApplication{})
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestUpdateStatus_PartnerPhonesError_BlocksApproval — PR #404: phones xətası
// olanda approve DB-yə yazılmır (rollback/reject YOX) — ekspert düzəlib
// yenidən təsdiq edə bilər.
func TestUpdateStatus_PartnerPhonesError_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{sendPhonesErr: errors.New("LW validation: invalid phone")}
	svc := newCardsTestService(store, provider)
	svc.SetLwPartnerPhonesEnabled(true)

	app := store.appByID[1]
	app.Status = model.StatusPendingApproval // UpdateStatus yalnız expert-review statuslarını qəbul edir
	app.CreditLevel = "new"                  // approve üçün credit_level tələb olunur
	app.Amount = 100
	app.ApprovedRate = 11
	app.Contact1Phone = "+994551110011"

	_, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: "new",
	})
	if err == nil {
		t.Fatal("expected error when partner phones call fails")
	}
	if !strings.Contains(err.Error(), "LW-yə göndərilə bilmədi") {
		t.Errorf("error should mention LW phones failure, got: %v", err)
	}

	// Status approved OLMAMALIDI (rollback yoxdur — retry mümkündür)
	updated, err := store.GetApplicationByID(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to reload app: %v", err)
	}
	if updated.Status == model.StatusApproved {
		t.Errorf("status must NOT be approved after phones failure (retryable), got %s", updated.Status)
	}
	if updated.Status == model.StatusRejected {
		t.Errorf("status must NOT be rejected after phones failure (no rollback), got %s", updated.Status)
	}
	// Create çağırılmalı DEYİL (phones create-dən əvvəl bloklayır)
	if provider.createAppReq != nil {
		t.Errorf("CreateApplication must not be called when phones fail")
	}
}

// TestUpdateStatus_PartnerPhonesSuccess_SendsBeforeCreate — uğur halında
// phones create-dən ƏVVƏL göndərilir və approve normal davam edir.
func TestUpdateStatus_PartnerPhonesSuccess_SendsBeforeCreate(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{}
	svc := newCardsTestService(store, provider)
	svc.SetLwPartnerPhonesEnabled(true)

	app := store.appByID[1]
	app.Status = model.StatusPendingApproval // UpdateStatus yalnız expert-review statuslarını qəbul edir
	app.CreditLevel = "new"                  // approve üçün credit_level tələb olunur
	app.Amount = 100
	app.ApprovedRate = 11
	app.Contact1Phone = "+994 55 111 00 22"
	app.Contact1Relation = "Anası"
	app.Contact1Name = "Sevda"

	if _, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: "new",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider.phonesReq == nil {
		t.Fatal("SendPartnerPhones was not called")
	}
	if provider.phonesPartnerID != app.PartnerID {
		t.Errorf("phones partnerID = %q, want %q", provider.phonesPartnerID, app.PartnerID)
	}
	if len(provider.phonesReq.PhoneData.Data) != 1 {
		t.Fatalf("expected 1 phone entry, got %d", len(provider.phonesReq.PhoneData.Data))
	}
	e := provider.phonesReq.PhoneData.Data[0]
	if e.Number != "+994551110022" {
		t.Errorf("number = %q, want +994551110022", e.Number)
	}
	if e.Description != "Anası | Sevda" {
		t.Errorf("description = %q, want %q", e.Description, "Anası | Sevda")
	}
	if provider.createAppReq == nil {
		t.Error("CreateApplication should proceed after successful phones send")
	}
}
