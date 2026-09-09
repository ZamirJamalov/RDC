package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rdc-source/internal/model"
)

// --- PR #436/#438 tests: approve video gate ---
//
// Yeni video order göndərildikdən sonra (son video_records sətri recorded=0)
// müraciət təsdiqə göndərilə BİLMƏZ — müştəri yeni video çəkənə qədər.
// PR #438: əlavə olaraq order-dan 60 san keçməyibsə də blok (min-gözləmə —
// video service-in köhnə statusu tez qaytarması hallarına qarşı).
// VideoRecordRepo konkret tip olduğundan gate test üçün videoOrderStateFn
// seam-i ilə əvəz olunur (prod-da GetByApplication-dən qurulur).

// newVideoGateServiceWithState — videoRecordEnabled=true + sabit state qaytaran
// seam. calls — seam neçə dəfə çağırıldı.
func newVideoGateServiceWithState(store *mockApplicationStore, state *videoOrderState, stateErr error) (*ApplicationService, *int) {
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	svc.videoRecordEnabled = true
	calls := 0
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		calls++
		return state, stateErr
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
	svc, calls := newVideoGateServiceWithState(store, &videoOrderState{Exists: true, Recorded: false, CreatedAt: time.Now().Add(-5 * time.Minute)}, nil)
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

// Video çəkilib (recorded=true) və order köhnədir (>60 san) → approve keçir.
func TestUpdateStatus_VideoGate_RecordedOldOrder_AllowsApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateServiceWithState(store, &videoOrderState{Exists: true, Recorded: true, CreatedAt: time.Now().Add(-5 * time.Minute)}, nil)
	newVideoGateApp(store)

	app, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if app.Status != model.StatusApproved {
		t.Errorf("status = %q, want approved", app.Status)
	}
}

// PR #438: order CAVDIR (<60 san) — recorded=true olsa belə bloklanır
// (min-gözləmə: müştəri real olaraq video çəkməyə vaxt tapmamışdır).
func TestUpdateStatus_VideoGate_FreshOrder_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateServiceWithState(store, &videoOrderState{Exists: true, Recorded: true, CreatedAt: time.Now().Add(-10 * time.Second)}, nil)
	newVideoGateApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err == nil {
		t.Fatal("expected error (1 dəqiqə gözləmə), got nil")
	}
	if !contains(err.Error(), "1 dəqiqə") {
		t.Errorf("error = %v, want 'ən azı 1 dəqiqə gözləyin' mesajı", err)
	}
	if store.appByID[1].Status != model.StatusPendingExpert {
		t.Errorf("status = %q, want pending_expert (approve keçməməli idi)", store.appByID[1].Status)
	}
}

// Order yoxdur (Exists=false) → blok, aşkar mesaj ilə.
func TestUpdateStatus_VideoGate_NoOrder_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateServiceWithState(store, &videoOrderState{Exists: false}, nil)
	newVideoGateApp(store)

	_, err := svc.UpdateStatus(ctx, 1, &UpdateStatusRequest{Status: model.StatusApproved, CreditLevel: model.CreditLevelNew})
	if err == nil {
		t.Fatal("expected error (video order tapılmadı), got nil")
	}
	if !contains(err.Error(), "video order tapılmadı") {
		t.Errorf("error = %v, want 'video order tapılmadı'", err)
	}
}

// Status yoxlana bilmədi (DB xətası) → fail-closed: approve bloklanır.
func TestUpdateStatus_VideoGate_StatusCheckError_BlocksApproval(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc, _ := newVideoGateServiceWithState(store, nil, errors.New("db down"))
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
	// videoRecordEnabled=false → videoOrderStateFn çağırılmamalıdır
	calls := 0
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		calls++
		return &videoOrderState{Exists: true, Recorded: false}, nil
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
	svc, calls := newVideoGateServiceWithState(store, &videoOrderState{Exists: true, Recorded: false}, nil)
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

// PR #438: videoOrderGate — birbaşa helper səviyyəsində sərhəd testləri.
func TestVideoOrderGate_Boundaries(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))

	// 59 san → blok
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		return &videoOrderState{Exists: true, Recorded: true, CreatedAt: time.Now().Add(-59 * time.Second)}, nil
	}
	if err := svc.videoOrderGate(ctx, 1); err == nil {
		t.Error("59 san: expected block, got nil")
	}

	// 61 san → keçir
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		return &videoOrderState{Exists: true, Recorded: true, CreatedAt: time.Now().Add(-61 * time.Second)}, nil
	}
	if err := svc.videoOrderGate(ctx, 1); err != nil {
		t.Errorf("61 san: expected pass, got %v", err)
	}

	// nil state (seam nil qaytardı) → Exists=false kimi blok
	svc.videoOrderStateFn = func(_ context.Context, _ int) (*videoOrderState, error) {
		return nil, nil
	}
	if err := svc.videoOrderGate(ctx, 1); err == nil {
		t.Error("nil state: expected block, got nil")
	}
}
