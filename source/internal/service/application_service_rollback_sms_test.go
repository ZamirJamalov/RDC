package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
)

// TestUpdateStatus_AzmkCreateRollback_NoRejectionSMS — PR #421: AZMK create
// texniki xətası (PR #283 rollback) müştəriyə "imtina" SMS-i GÖNDƏRMƏMƏLİDİR.
// Bu, Günel hadisəsindəki bug idi: amount limiti xətası → rollback →
// "təsdiq olunmadı" SMS-i — halbuki müraciət yenidən təsdiq edilə bilərdi.
func TestUpdateStatus_AzmkCreateRollback_NoRejectionSMS(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{
		createAppErr: errors.New("azmk: /application/create returned HTTP 500: limit"),
	}
	svc := newCardsTestService(store, provider)
	sms := &recordingSMSProvider{}
	svc.smsProvider = sms

	app := store.appByID[1]
	app.Status = model.StatusPendingApproval
	app.CreditLevel = "new"
	app.Amount = 500
	app.ApprovedRate = 11
	app.CustomerPhone = "+994501234567"
	app.CustomerFullName = "Test Müştəri"

	_, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: "new",
	})
	if err == nil {
		t.Fatal("expected error (AZMK create failure must surface to expert)")
	}

	// ƏSAS ASSERT: rollback texniki xətadır — imtina SMS-i GETMƏMƏLİDİR
	if len(sms.sends) != 0 {
		t.Errorf("PR #421: technical rollback must NOT send rejection SMS, got %d SMS", len(sms.sends))
	}

	// Rollback statusu rejected olmalıdır (PR #283 davranışı qorunur)
	updated, getErr := store.GetApplicationByID(ctx, app.ID)
	if getErr != nil {
		t.Fatalf("failed to reload app: %v", getErr)
	}
	if updated.Status != model.StatusRejected {
		t.Errorf("rollback status = %s, want rejected (PR #283 preserved)", updated.Status)
	}
}

// TestUpdateStatus_ExpertReject_RejectionSMSSent — müqayisə üçün: HƏQİQİ
// ekspert imtinasında SMS GETMƏLİDİR (PR #362 davranışı qorunur).
func TestUpdateStatus_ExpertReject_RejectionSMSSent(t *testing.T) {
	ctx := context.Background()
	store := newCardsTestStore()
	provider := &fakeAzmkOnlineProvider{}
	svc := newCardsTestService(store, provider)
	sms := &recordingSMSProvider{}
	svc.smsProvider = sms

	app := store.appByID[1]
	app.Status = model.StatusPendingApproval
	app.CustomerPhone = "+994501234567"

	_, err := svc.UpdateStatus(ctx, app.ID, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_DOCS",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sms.sends) != 1 {
		t.Errorf("expert reject must send rejection SMS (PR #362), got %d", len(sms.sends))
	}
	if len(sms.sends) == 1 && sms.sends[0].Phone != "+994501234567" {
		t.Errorf("SMS phone = %s, want +994501234567", sms.sends[0].Phone)
	}
}
