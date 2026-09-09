package service

import (
	"context"
	"testing"

	"rdc-source/internal/model"
)

// --- PR #437 tests: video müraciət status qadağası ---
//
// "Video müraciət göndər" (SendVideoRecordSMS) yalnız "Ekspert gözləyir"
// (pending_expert) statusunda işləyir. Dashboard düyməsi də eyni şərtlə
// deaktiv göstərilir; API səviyyəsindəki qadağa birbaşa çağırışları da əhatə edir.
// Status yoxlaması video-enabled check-dən ƏVVƏLDIR — testlərdə video deaktiv
// olsa belə status qapısı yoxlanıla bilir.

// Yanlış status (pending_approval və s.) → rədd, status mesajı ilə.
func TestSendVideoRecordSMS_WrongStatus_Rejected(t *testing.T) {
	ctx := context.Background()

	wrongStatuses := []string{
		model.StatusPendingApproval,
		model.StatusPendingCustomer,
		model.StatusApproved,
		model.StatusRejected,
	}
	for _, status := range wrongStatuses {
		store := newMockStore()
		store.appByID[1] = &model.LoanApplication{
			ID:            1,
			CustomerPIN:   "PIN1",
			CustomerPhone: "+994501234567",
			Status:        status,
		}
		svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

		_, err := svc.SendVideoRecordSMS(ctx, 1)
		if err == nil {
			t.Fatalf("status %s: expected error, got nil", status)
		}
		if !contains(err.Error(), "yalnız 'Ekspert gözləyir'") {
			t.Errorf("status %s: error = %v, want status qadağası mesajı", status, err)
		}
	}
}

// Düzgün status (pending_expert) → status qapısından keçir (növbəti qapıya
// çatır — video deaktiv olduğu üçün "video record deaktiv" xətası gözlənilir;
// bu, yoxlamanın statusdan SONRA işlədiyini sübut edir).
func TestSendVideoRecordSMS_PendingExpert_PassesStatusGate(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	store.appByID[1] = &model.LoanApplication{
		ID:            1,
		CustomerPIN:   "PIN1",
		CustomerPhone: "+994501234567",
		Status:        model.StatusPendingExpert,
	}
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

	_, err := svc.SendVideoRecordSMS(ctx, 1)
	if err == nil {
		t.Fatal("expected 'video record deaktiv' error (test wiring), got nil")
	}
	if !contains(err.Error(), "video record deaktiv") {
		t.Errorf("error = %v, want 'video record deaktiv' (status qapısından keçməli idi)", err)
	}
}

// Tapılmayan müraciət → status yoxlamasına çatmadan xəta.
func TestSendVideoRecordSMS_AppNotFound(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

	_, err := svc.SendVideoRecordSMS(ctx, 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "application not found") {
		t.Errorf("error = %v, want 'application not found'", err)
	}
}
