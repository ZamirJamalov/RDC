package service

import (
	"context"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// --- PR #434 tests: AKB stop-faktor AKB_SCORE_LOW-dan ƏVVƏL yoxlanılır ---
//
// Real AZMK davranışı: stop-faktorlu müştəriyə (response ∈ {AB,NI,NU,TY})
// getMkrScore Point=1 (placeholder) qaytarır. Köhnə sıralamada AKB_SCORE_LOW
// birinci yoxlanılırdı → 0 < 1 < 200 → müştəri səhvən AKB_SCORE_LOW (2 gün)
// ilə rədd olunurdu, AKB_STOP_FACTOR (30 gün) heç vaxt yoxlanılmırdı.
// Konvensiya: credit_engine.go resolveAkbScoreAndStopFactors Point=1-i
// stop-faktor kimi qəbul edir.

// newCutoffTestService wires a service with the AZMK mock provider and
// cutoff checks enabled + stop-on-first-fail (production defaults).
func newCutoffTestService(provider *mockAzmkCustomerData, store *mockApplicationStore) *ApplicationService {
	svc := NewApplicationService(store, NewCreditEngine(newMockLWProvider(), newMockStore()), newMockCustomerStore(), NewOTPService(nil, nil))
	if provider != nil {
		svc.SetCustomerDataProvider(provider)
	}
	svc.SetCutoffChecksEnabled(true)   // production default (cfg.CutoffChecksEnabled)
	svc.SetCutoffStopOnFirstFail(true) // production default (cfg.CutoffStopOnFirstFail)
	return svc
}

func newCutoffTestApp(store *mockApplicationStore) *model.LoanApplication {
	app := &model.LoanApplication{
		ID:             1,
		CustomerPIN:    "PIN1",
		CustomerSerial: "AA1234567",
		Status:         model.StatusPendingCustomer,
	}
	store.appByID[1] = app
	return app
}

// TestRunEarlyCutoffChecks_StopFactorBeforeScoreLow — əsas regressiya testi:
// Point=1 + response="AB" → rədd səbəbi AKB_STOP_FACTOR:AB olmalıdır,
// AKB_SCORE_LOW YOX (köhnə kodda elə idi).
func TestRunEarlyCutoffChecks_StopFactorBeforeScoreLow(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{Name: "Test", Surname: "Customer", BirthDate: "1990-01-15"},
		mkrScore: &azmk.MkrScore{
			Score: azmk.MkrScoreDetail{Point: 1, Response: "AB", Calculated: true},
		},
	}
	svc := newCutoffTestService(provider, store)
	app := newCutoffTestApp(store)

	reason, err := svc.runEarlyCutoffChecks(ctx, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AKB_STOP_FACTOR:AB" {
		t.Errorf("rejection reason = %q, want %q (Point=1 stop-faktor placeholder AKB_SCORE_LOW kimi təsnif edilməməlidir)", reason, "AKB_STOP_FACTOR:AB")
	}
	// PR #434: placeholder skor DB-yə yazılmamalıdır — yoxsa GetRecentAkbScore
	// cache-i (dbScore > 0 → "stop-faktor yoxdur") sonrakı aşkarlanmanı gizlədər.
	if app.AkbScore != 0 {
		t.Errorf("app.AkbScore = %d, want 0 (Point=1 placeholder saxlanılmamalıdır)", app.AkbScore)
	}
	if updated := store.appByID[1].AkbScore; updated != 0 {
		t.Errorf("stored akb_score = %d, want 0 (placeholder UpdateAkbScore ilə yazılmamalıdır)", updated)
	}
}

// TestRunEarlyCutoffChecks_StopFactorAllResponses — bütün stop-faktor
// kodları (AB/NI/NU/TY) düzgün suffiks ilə rədd olunmalıdır.
func TestRunEarlyCutoffChecks_StopFactorAllResponses(t *testing.T) {
	for _, resp := range []string{"AB", "NI", "NU", "TY"} {
		ctx := context.Background()
		store := newMockStore()
		provider := &mockAzmkCustomerData{
			personalData: &azmk.CustomerData{Name: "Test", Surname: "Customer", BirthDate: "1990-01-15"},
			mkrScore: &azmk.MkrScore{
				Score: azmk.MkrScoreDetail{Point: 1, Response: resp, Calculated: true},
			},
		}
		svc := newCutoffTestService(provider, store)
		app := newCutoffTestApp(store)

		reason, err := svc.runEarlyCutoffChecks(ctx, app)
		if err != nil {
			t.Fatalf("response %s: unexpected error: %v", resp, err)
		}
		want := "AKB_STOP_FACTOR:" + resp
		if reason != want {
			t.Errorf("response %s: rejection reason = %q, want %q", resp, reason, want)
		}
	}
}

// TestRunEarlyCutoffChecks_LowScoreNoStopFactor — sıralama dəyişsə də
// adi aşağı skor (Point=150, stop-faktor yox) AKB_SCORE_LOW ilə rədd olunmalıdır.
func TestRunEarlyCutoffChecks_LowScoreNoStopFactor(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{Name: "Test", Surname: "Customer", BirthDate: "1990-01-15"},
		mkrScore: &azmk.MkrScore{
			Score: azmk.MkrScoreDetail{Point: 150, Response: "C", Calculated: true},
		},
	}
	svc := newCutoffTestService(provider, store)
	app := newCutoffTestApp(store)

	reason, err := svc.runEarlyCutoffChecks(ctx, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AKB_SCORE_LOW" {
		t.Errorf("rejection reason = %q, want %q", reason, "AKB_SCORE_LOW")
	}
}

// TestRunEarlyCutoffChecks_GoodScorePasses — yaxşı skor keçir, real skor
// (Point > 1) isə PR #228 üzrə saxlanılır.
func TestRunEarlyCutoffChecks_GoodScorePasses(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{Name: "Test", Surname: "Customer", BirthDate: "1990-01-15"},
		mkrScore: &azmk.MkrScore{
			Score: azmk.MkrScoreDetail{Point: 650, Response: "B", Calculated: true},
		},
	}
	svc := newCutoffTestService(provider, store)
	app := newCutoffTestApp(store)

	reason, err := svc.runEarlyCutoffChecks(ctx, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Errorf("rejection reason = %q, want empty (good score passes)", reason)
	}
	if app.AkbScore != 650 {
		t.Errorf("app.AkbScore = %d, want 650 (real skor saxlanılmalıdır)", app.AkbScore)
	}
}

// TestRunEarlyCutoffChecks_StopFactorPrecedenceWithoutStop —
// cutoffStopOnFirstFail=false olanda hər iki kəsim işə düşür, amma ilk
// rədd (firstRejection) AKB_STOP_FACTOR olmalıdır — 30 günlük blok üstünlük təşkil edir.
func TestRunEarlyCutoffChecks_StopFactorPrecedenceWithoutStop(t *testing.T) {
	ctx := context.Background()

	store := newMockStore()
	provider := &mockAzmkCustomerData{
		personalData: &azmk.CustomerData{Name: "Test", Surname: "Customer", BirthDate: "1990-01-15"},
		mkrScore: &azmk.MkrScore{
			Score: azmk.MkrScoreDetail{Point: 1, Response: "NI", Calculated: true},
		},
	}
	svc := newCutoffTestService(provider, store)
	svc.SetCutoffStopOnFirstFail(false) // bütün kəsimlər yoxlanılır
	app := newCutoffTestApp(store)

	reason, err := svc.runEarlyCutoffChecks(ctx, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "AKB_STOP_FACTOR:NI" {
		t.Errorf("rejection reason = %q, want %q (stop-faktor üstünlük təşkil etməlidir)", reason, "AKB_STOP_FACTOR:NI")
	}
}
