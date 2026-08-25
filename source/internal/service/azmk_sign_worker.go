package service

import (
	"context"
	"log/slog"
	"time"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
)

// PR #312: AZMK imza gözləmə worker-ı.
//
// Ekspert approve edib AZMK /application/create uğurlu olanda müraciət
// pending_signature statusuna keçir (azmkCreateApplication). Müştəriyə
// müqaviləni imzalamaq üçün 3 saat verilir (AZMK tərəfindən limit).
//
// Bu worker hər intervalS saniyədə:
//   1. Bütün pending_signature müraciətləri siyahıya alır
//   2. Hər biri üçün GET /application/{id}/status çağırır
//        - loanId gəlirsə → azmk_loan_id saxlanılır (audit)
//        - signed=true → POST /application/disburse → status=disbursed
//        - signed=false və vaxt bitibsə → status=rejected (imza vaxtı bitdi)
//
// Bütün xətalar non-fatal: log lanır, növbəti tick-də təkrar cəhd olunur.
// interval və timeout parametri rdc.env üzərindən qurulur:
//
//	AZMK_SIGN_POLL_INTERVAL_S (default 300 = 5 dəq)
//	AZMK_SIGN_TIMEOUT_S       (default 10800 = 3 saat)
//
// NOTE: disburse uğurlu olanda status yazılışı guarded update-dir
// (MarkDisbursedIfPendingSignature) — eyni müraciət iki dəfə disbursed
// ola bilməz. Disburse uğurlayıb status yazılışı alınmazsa AZMK tərəfində
// təkrar disburse cəhdi idempotent rədd olunmalıdır (AZMK tövsiyəsi ilə
// təsdiqlənməlidir).

// StartAzmkSignWorker launches the background sign-polling daemon.
// Non-blocking: dərhal qayıdır, worker ayrıca goroutine-da işləyir.
func (s *ApplicationService) StartAzmkSignWorker(intervalS, timeoutS int) {
	if intervalS <= 0 {
		intervalS = 300
	}
	if timeoutS <= 0 {
		timeoutS = 10800
	}

	go func() {
		ticker := time.NewTicker(time.Duration(intervalS) * time.Second)
		defer ticker.Stop()
		slog.Info("PR #312: AZMK sign worker started",
			"poll_interval_s", intervalS,
			"sign_timeout_s", timeoutS)
		for range ticker.C {
			s.pollPendingSignatures(context.Background(), timeoutS)
		}
	}()
}

// pollPendingSignatures processes one tick: all pending_signature apps.
func (s *ApplicationService) pollPendingSignatures(ctx context.Context, timeoutS int) {
	if s.azmkProvider == nil {
		return
	}

	apps, err := s.repo.ListByStatus(ctx, model.StatusPendingSignature)
	if err != nil {
		slog.Warn("PR #312: sign worker — failed to list pending_signature",
			"error", err)
		return
	}
	if len(apps) == 0 {
		return
	}

	for i := range apps {
		s.pollOneSignature(ctx, apps[i].ID, timeoutS)
	}
}

// pollOneSignature polls a single pending_signature application.
// Bütün xətalar yalnız log lanır — app növbəti tick-də yenidən yoxlanılır.
func (s *ApplicationService) pollOneSignature(ctx context.Context, id, timeoutS int) {
	app, err := s.repo.GetApplicationByID(ctx, id)
	if err != nil {
		slog.Warn("PR #312: sign worker — failed to fetch application",
			"application_id", id, "error", err)
		return
	}
	if app.Status != model.StatusPendingSignature {
		return // bu tick arasında status dəyişib (məs: manual) — skip
	}
	if app.LwApplicationID == "" {
		slog.Warn("PR #312: sign worker — pending_signature app without lw_application_id (data integrity)",
			"application_id", id)
		return
	}

	st, err := s.azmkProvider.GetApplicationStatus(ctx, app.LwApplicationID)
	if err != nil {
		slog.Warn("PR #312: sign worker — status poll failed (will retry next tick)",
			"application_id", id,
			"lw_application_id", app.LwApplicationID,
			"error", err)
		// Status sorğusu alınmasa da vaxt bitibsə müraciəti bağıla bilərik —
		// amma AZMK müvəqqəti xəta verə bilər, ona görə yalnız log.
		return
	}

	// loanId gəlibsə saxla (audit) — AZMK kredit hesab nömrəsi
	if st.LoanID != "" && st.LoanID != app.AzmkLoanID {
		if err := s.repo.UpdateAzmkLoanID(ctx, app.ID, st.LoanID); err != nil {
			slog.Warn("PR #312: sign worker — failed to save azmk_loan_id (non-fatal)",
				"application_id", id, "loan_id", st.LoanID, "error", err)
		} else {
			app.AzmkLoanID = st.LoanID
		}
	}

	if !st.Signed {
		// Hələ imzalanmayıb — vaxt bitibsə rejected et (SQL tərəfində atomik).
		expired, err := s.repo.ExpireAzmkSignIfTimeout(ctx, app.ID, timeoutS)
		if err != nil {
			slog.Warn("PR #312: sign worker — expire check failed",
				"application_id", id, "error", err)
			return
		}
		if expired {
			slog.Info("PR #312: sign timeout — application rejected",
				"application_id", id,
				"lw_application_id", app.LwApplicationID,
				"timeout_s", timeoutS)
		}
		return
	}

	// İmzalandı → disburse et.
	disburseReq := &azmk.DisburseRequest{
		LoanData: azmk.LoanData{
			ApplicationID: app.LwApplicationID,
			CardID:        app.CardID,
		},
	}
	if err := s.azmkProvider.Disburse(ctx, disburseReq); err != nil {
		slog.Error("PR #312: sign worker — disburse failed (will retry next tick)",
			"application_id", id,
			"lw_application_id", app.LwApplicationID,
			"card_id", app.CardID,
			"error", err)
		return
	}

	// Guarded update: yalnız hələ pending_signature-dursa disbursed et.
	applied, err := s.repo.MarkDisbursedIfPendingSignature(ctx, app.ID)
	if err != nil {
		slog.Error("PR #312: sign worker — disbursed amma status yazıla bilmədi (DB xətası)",
			"application_id", id, "error", err)
		return
	}
	if !applied {
		slog.Warn("PR #312: sign worker — status dəyişib, disbursed yazılmadı",
			"application_id", id, "current_status", app.Status)
		return
	}

	slog.Info("PR #312: customer signed — loan disbursed automatically",
		"application_id", id,
		"lw_application_id", app.LwApplicationID,
		"azmk_loan_id", app.AzmkLoanID,
		"amount", app.TotalAmount,
		"card_id", app.CardID)

	// PR #284: disburse success — müştəriyə referal kodu SMS-i (non-fatal)
	s.sendReferralSMSOnDisburse(ctx, app)
}
