package service

import (
	"context"
	"errors"
	"testing"

	"rdc-source/internal/model"
)

// --- PR #436 tests: approve video gate ---
//
// Yeni video order göndərildikdən sonra (son video_records sətri recorded=0)
// müraciət təsdiqə göndərilə BİLMƏZ — müştəri yeni video çəkənə qədər.
// VideoRecordRepo konkret tip olduğundan gate test üçün videoIsRecordedFn
// seam-i ilə əvəz olunur (prod-da repo.IsRecorded-dir).

// newVideoGateService — videoRecordEnabled=true + fərqli recorded nəticələri
// üçün konfiqurasiya olunan service. calls — seam neçə dəfə çağırıldı.
func newVideoGateService(store *mockApplicationStore, recorded bool, recordErr error) (*ApplicationService, *int) {
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	svc.videoRecordEnabled = true
	calls := 0
	svc.videoIsRecordedFn = func(_ context.Context, _ int) (bool, error) {
		calls++
		return recorded, recordErr
	}
	return svc, &calls
}

func newVideoGateApp(store *mockApplicationStore) *model.LoanApplication {
	app := &model.LoanApplication{
		ID:           1,
		CustomerPIN:  "PIN1",
		Status:       model.StatusPendingExpert,
		CreditLevel:  model.CreditLevelNew,
		Amount:       300,
		ApprovedRate: 14,
	}
	store.appByID[1] = app
	return app
}

// Yeni order gözləyir (recorded=false) → approve bloklanır.
func TestUpdateStatus_VideoGate_NewOrderPending_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, calls := newVideoGateService(store, false, nil)
	newVideoGateApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err == nil {
		t.Fatal("expected error (yeni video hələ çəkilməyib), got nil")
	}
	if !contains(err.Error(), "yeni video") {
		t.Errorf("error = %v, want 'yeni video hələ çəkilməyib' mesajı", err)
	}
	if *calls != 1 {
		t.Errorf("gate calls = %d, want 1", *calls)
	}
	if store.appByID[1].Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (approve keçməməli idi)", store.appByID[1].Status)
	}
}

// Video çəkilib (recorded=true) → approve keçir.
func TestUpdateStatus_VideoGate_Recorded_AllowsApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateService(store, true, nil)
	newVideoGateApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if app.Status != model.StatusApproved {
		t.Errorf("status = %q, want approved", app.Status)
	}
}

// Status yoxlana bilmədi (DB xətası) → fail-closed: approve bloklanır.
func TestUpdateStatus_VideoGate_StatusCheckError_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateService(store, false, errors.New("db down"))
	newVideoGateApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err == nil {
		t.Fatal("expected error (status yoxlana bilmədi), got nil")
	}
	if !contains(err.Error(), "video status yoxlana bilmədi") {
		t.Errorf("error = %v, want 'video status yoxlana bilmədi'", err)
	}
}

// Video deaktiv → gate işə düşmür (geri uyğunluq).
func TestUpdateStatus_VideoGate_Disabled_SkipsCheck(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	// videoRecordEnabled=false → videoIsRecordedFn çağırılmamalıdır
	calls := 0
	svc.videoIsRecordedFn = func(_ context.Context, _ int) (bool, error) {
		calls++
		return false, nil
	}
	newVideoGateApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err != nil {
		t.Fatalf("approve failed (video deaktiv — gate skip olmalı idi): %v", err)
	}
	if app.Status != model.StatusApproved {
		t.Errorf("status = %q, want approved", app.Status)
	}
	if calls != 0 {
		t.Errorf("gate calls = %d, want 0 (video deaktiv)", calls)
	}
}

// Gate yalnız approve-a aiddir — rejectə təsir etmir.
func TestUpdateStatus_VideoGate_RejectNotAffected(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, calls := newVideoGateService(store, false, nil)
	newVideoGateApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{
		Status:          model.StatusRejected,
		RejectionReason: "MANUAL_VIDEO_MISMATCH",
	})
	if err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if app.Status != model.StatusRejected {
		t.Errorf("status = %q, want rejected", app.Status)
	}
	if *calls != 0 {
		t.Errorf("gate calls = %d, want 0 (reject yolu gate-dən keçmir)", *calls)
	}
}
