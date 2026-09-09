package service

import (
	"context"
	"testing"

	"rdc-source/internal/model"
)

// --- PR #441 tests: disburse_failed statusundan imtina yalnız admin ---
//
// disburse_failed (köçürmə xətası) statuslu müraciəti YALNIZ admin reject
// edə bilər. Approve bu statusda mümkün deyil (disburse retry ayrı mexanizmdir).
// İmtina səbəbi: MANUAL_DISBURSE_FAILED (migration 056, validity_days=0 —
// müştəri dərhal təkrar müraciət edə bilər).

func newDisburseFailedApp(store *mockApplicationStore) *model.LoanApplication {
	app := &model.LoanApplication{
		ID:           1,
		CustomerPIN:  "PIN1",
		Status:       model.StatusDisburseFailed,
		CreditLevel:  model.CreditLevelNew,
		Amount:       300,
		ApprovedRate: 14,
	}
	store.appByID[1] = app
	return app
}

// Admin reject edə bilər.
func TestUpdateStatus_DisburseFailed_AdminReject_Allowed(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	newDisburseFailedApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_DISBURSE_FAILED",
		IsAdmin:         true,
	})
	if err != nil {
		t.Fatalf("admin reject failed: %v", err)
	}
	if app.Status != model.StatusRejected {
		t.Errorf("status = %q, want rejected", app.Status)
	}
}

// Ekspert (admin deyil) reject edə BİLMƏZ.
func TestUpdateStatus_DisburseFailed_NonAdminReject_Blocked(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	newDisburseFailedApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_DISBURSE_FAILED",
		IsAdmin:         false,
	})
	if err == nil {
		t.Fatal("expected error (yalnız admin), got nil")
	}
	if !contains(err.Error(), "yalnız admin") {
		t.Errorf("error = %v, want 'yalnız admin imtina edə bilər'", err)
	}
	if store.appByID[1].Status != model.StatusDisburseFailed {
		t.Errorf("status = %q, want disburse_failed (dəyişməməli idi)", store.appByID[1].Status)
	}
}

// Approve bu statusda mümkün deyil — admin belə olsa.
func TestUpdateStatus_DisburseFailed_Approve_Blocked(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	newDisburseFailedApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
		Status:      model.StatusApproved,
		CreditLevel: model.CreditLevelNew,
		IsAdmin:     true,
	})
	if err == nil {
		t.Fatal("expected error (approve mümkün deyil), got nil")
	}
	if !contains(err.Error(), "yalnız imtina") {
		t.Errorf("error = %v, want 'yalnız imtina mümkündür' mesajı", err)
	}
}

// disburse_failed-dan video gate rejectə mane olmur (video onsuz da çəkilib).
func TestUpdateStatus_DisburseFailed_AdminReject_VideoGateSkipped(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	// video gate işə düşsə belə (videoRecordEnabled) — reject yolu gate-dən keçmir
	svc.videoRecordEnabled = true
	calls := 0
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		calls++
		return &videoOrderState{Exists: true, Recorded: false}, nil
	}
	newDisburseFailedApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_DISBURSE_FAILED",
		IsAdmin:         true,
	})
	if err != nil {
		t.Fatalf("admin reject failed: %v", err)
	}
	if app.Status != model.StatusRejected {
		t.Errorf("status = %q, want rejected", app.Status)
	}
	if calls != 0 {
		t.Errorf("video gate calls = %d, want 0 (reject yolu gate-dən keçmir)", calls)
	}
}
