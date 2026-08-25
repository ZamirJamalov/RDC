package service

import (
	"context"
	"fmt"
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
// NOTE (cüt disburse qorunması): AZMK təkrar disburse sorğusunu idempotent
// rədd etmir — ona görə claim protokolu istifadə olunur:
//   1. pending_signature → disbursing (atomik claim, ClaimForDisburse)
//   2. disburse sorğusu YALNIZ claim-i qazanan icra edir
//   3. uğur → disbursed; xəta → disburse_failed (auto-retry YOXDUR)
// Proses disbursing-də qalsa (crash) — nəticə naməlumdur, növbəti tick-də
// disburse_failed edilir və manual yoxlama tələb olunur.
// Beləliklə disburse sorğusu sistemdən MAKSİMUM 1 dəfə çıxır.

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

// pollPendingSignatures processes one tick: stuck disbursing sweep + all
// pending_signature apps.
//
// TICK SIRASI: (1) disbursing sweep ƏVVƏL işlədilir — worker tək goroutine-də
// ardıcıl işləyir, ona görə tick-in başında görülən hər hansı disbursing
// statusu keçmiş prosesdən qalmışdır (crash mid-disburse) və nəticəsi
// naməlumdur → disburse_failed (auto-retry YOXDUR, manual review).
// NOTE: bu fərziyyə tək-install deployment üçün doğrudur (systemd rdc.service).
func (s *ApplicationService) pollPendingSignatures(ctx context.Context, timeoutS int) {
	if s.azmkProvider == nil {
		return
	}

	// 0) Keçmiş prosesdən qalan disbursing — nəticə naməlum, disburse_failed et.
	stuck, err := s.repo.ListByStatus(ctx, model.StatusDisbursing)
	if err != nil {
		slog.Warn("PR #312: sign worker — failed to list disbursing",
			"error", err)
		return
	}
	for i := range stuck {
		id := stuck[i].ID
		reason := "AZMK disburse nəticəsi naməlum (proses disburse zamanı yenidən başladı) — manual yoxlama lazımdır"
		applied, markErr := s.repo.MarkDisburseFailed(ctx, id, reason)
		if markErr != nil {
			slog.Error("PR #312: sign worker — failed to mark stuck disbursing as disburse_failed",
				"application_id", id, "error", markErr)
			continue
		}
		if applied {
			slog.Error("PR #312: stuck disbursing detected — marked disburse_failed, manual review required",
				"application_id", id,
				"lw_application_id", stuck[i].LwApplicationID,
				"note", "AZMK tərəfindən pulun keçib-keçmədiyi yoxlanmalıdır")
		}
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

	// İmzalandı → disburse. PR #312 claim protokolu (cüt disburse qorunması):
	//   1) pending_signature → disbursing (atomik claim)
	//   2) AZMK disburse sorğusu yalnız claim qazanılıbsa göndərilir
	//   3) uğur → disbursed; xəta → disburse_failed (auto-retry YOXDUR)
	claimed, err := s.repo.ClaimForDisburse(ctx, app.ID)
	if err != nil {
		slog.Warn("PR #312: sign worker — claim for disburse failed (will retry next tick)",
			"application_id", id, "error", err)
		return
	}
	if !claimed {
		slog.Info("PR #312: sign worker — disburse already claimed by another process, skipping",
			"application_id", id)
		return
	}

	disburseReq := &azmk.DisburseRequest{
		LoanData: azmk.LoanData{
			ApplicationID: app.LwApplicationID, // AZMK təsdiqi: disburse applicationId istifadə edir
			CardID:        app.CardID,
		},
	}
	if err := s.azmkProvider.Disburse(ctx, disburseReq); err != nil {
		// Xətanın nəticəsi naməlum ola bilər (timeout = AZMK icra etmiş ola bilər)
		// → auto-retry YOXDUR, manual review üçün disburse_failed et.
		reason := fmt.Sprintf("AZMK disburse xətası: %v", err)
		applied, markErr := s.repo.MarkDisburseFailed(ctx, app.ID, reason)
		if markErr != nil {
			slog.Error("PR #312: sign worker — disburse failed AND marking failed (stuck in disbursing, next tick will mark disburse_failed)",
				"application_id", id, "disburse_error", err, "mark_error", markErr)
			return
		}
		slog.Error("PR #312: disburse failed — marked disburse_failed, manual review required (NO auto-retry)",
			"application_id", id,
			"lw_application_id", app.LwApplicationID,
			"card_id", app.CardID,
			"disburse_error", err,
			"marked", applied)
		return
	}

	// Guarded update: disbursing → disbursed.
	applied, err := s.repo.MarkDisbursedFromDisbursing(ctx, app.ID)
	if err != nil {
		// Disburse UĞURLU amma status yazıla bilmədi — status disbursing qalır,
		// növbəti tick-də stuck-sweep onu disburse_failed edəcək (nəticə naməlum
		// kimi) → admin AZMK-da yoxlayıb manual disbursed edər. Təkrar disburse YOX.
		slog.Error("PR #312: sign worker — disburse OK ama status yazıla bilmədi (next tick marks disburse_failed, manual fix needed)",
			"application_id", id, "error", err)
		return
	}
	if !applied {
		slog.Warn("PR #312: sign worker — status dəyişib, disbursed yazılmadı",
			"application_id", id)
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
