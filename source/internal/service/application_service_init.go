package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"rdc-source/internal/model"
	"rdc-source/pkg/azmk"
	"rdc-source/pkg/otp"
)

// InitApplicationRequest is the body for POST /api/applications/init.
// This is what the customer fills in on the public website.
type InitApplicationRequest struct {
	CustomerPIN    string `json:"customer_pin"`
	CustomerSerial string `json:"customer_serial"`
	CustomerPhone  string `json:"customer_phone"`
}

// InitApplication creates a new application with minimal info (PIN, serial, phone)
// and sends an OTP to the customer's phone. The application starts in
// pending_customer status. Cutoff checks (AKB, blacklist) will be added later.
//
// PR #217: Idempotent — əgər son 5 dəqiqə ərzində eyni PIN+phone ilə
// pending_customer app varsa, onu reuse et (yeni yaratma). Bu, yetim
// müraciətlərin qarşısını alır və istifadəçinin eyni OTP-ni istifadə etməsinə imkan verir.
func (s *ApplicationService) InitApplication(ctx context.Context, req *InitApplicationRequest) (*model.LoanApplication, error) {
	if req.CustomerPIN == "" {
		return nil, fmt.Errorf("customer_pin is required")
	}
	if req.CustomerPhone == "" {
		return nil, fmt.Errorf("customer_phone is required")
	}

	// PR #505: yan-təsirsiz yoxlamalar helper-lərdədir — PreflightInitApplication
	// eyni məntiqi işlədir (preflight nə deyirsə, init o qərarı verir).
	recentApp, err := s.findReusableApplication(ctx, req.CustomerPIN, req.CustomerPhone)
	if err != nil {
		return nil, err
	}
	if recentApp != nil {
		// Mövcud pending_customer app var — reuse et (yeni yaratma)
		slog.Info("application reused (recent pending_customer exists)",
			"application_id", recentApp.ID,
			"public_id", recentApp.PublicID,
			"customer_pin", req.CustomerPIN,
			"phone", req.CustomerPhone)

		// Aktiv OTP var? → yeni OTP göndərmə, OTP input göstər
		if s.otpService.HasActiveOTP(ctx, req.CustomerPhone) {
			slog.Info("reused application, active OTP already exists",
				"application_id", recentApp.ID,
				"phone", req.CustomerPhone)
			return recentApp, nil
		}

		// Aktiv OTP yoxdur və ya expiry olub → yeni OTP yarat və göndər
		s.otpService.ExpireOldCodes(ctx)
		otpResp, err := s.otpService.SendOTP(ctx, req.CustomerPhone)
		if err != nil {
			return nil, fmt.Errorf("failed to send OTP: %w", err)
		}
		if !otpResp.Sent {
			slog.Error("OTP rate limited — blocking OTP input (reused app)",
				"application_id", recentApp.ID,
				"phone", req.CustomerPhone,
				"retry_after_s", otpResp.RetryAfterS)
			return nil, fmt.Errorf("OTP göndürmək üçün %d saniyə gözləməlisiniz", otpResp.RetryAfterS)
		}

		slog.Info("reused application, new OTP sent",
			"application_id", recentApp.ID,
			"phone", req.CustomerPhone)
		return recentApp, nil
	}

	// PR #505: blok yoxlaması (dublikat / rejection cooldown / serial-mismatch) helper-də.
	if err := s.initBlockReason(ctx, req.CustomerPIN); err != nil {
		return nil, err
	}

	// Yeni app yarat
	app := &model.LoanApplication{
		CustomerPIN:    req.CustomerPIN,
		CustomerSerial: req.CustomerSerial,
		CustomerPhone:  req.CustomerPhone,
		Status:         model.StatusPendingCustomer,
	}

	if err := s.repo.CreateApplication(ctx, app); err != nil {
		return nil, fmt.Errorf("failed to create application: %w", err)
	}

	// PR #209/#210/#212/#214: aktiv OTP yoxla (UTC expiry)
	if s.otpService.HasActiveOTP(ctx, req.CustomerPhone) {
		slog.Info("application initialized, active OTP already exists (not sending new)",
			"application_id", app.ID,
			"customer_pin", req.CustomerPIN,
			"phone", req.CustomerPhone)
		return app, nil
	}

	// Aktiv OTP yoxdur və ya expiry olub → yeni OTP yarat və göndər
	s.otpService.ExpireOldCodes(ctx)

	otpResp, err := s.otpService.SendOTP(ctx, req.CustomerPhone)
	if err != nil {
		return nil, fmt.Errorf("failed to send OTP: %w", err)
	}
	if !otpResp.Sent {
		// PR #215: rate limit olanda xəta qaytar — OTP daxil etmə bloklansın
		slog.Error("OTP rate limited — blocking OTP input",
			"application_id", app.ID,
			"phone", req.CustomerPhone,
			"retry_after_s", otpResp.RetryAfterS)
		return nil, fmt.Errorf("OTP göndürmək üçün %d saniyə gözləməlisiniz", otpResp.RetryAfterS)
	}

	slog.Info("application initialized, OTP sent",
		"application_id", app.ID,
		"customer_pin", req.CustomerPIN,
		"phone", req.CustomerPhone)

	return app, nil
}

// findReusableApplication — PR #505: init/preflight paylaşan reuse yoxlaması.
// Son 10 dəqiqə ərzində eyni PIN+phone ilə pending_customer app varsa onu qaytarır
// (init bu app-i davam etdirir, yenisini yaratmır — yetim müraciətlərin qarşısı).
func (s *ApplicationService) findReusableApplication(ctx context.Context, pin, phone string) (*model.LoanApplication, error) {
	// PR #217/#221: Idempotent — window 5→10 dəq (cutoff-lar OTP verify-də işləyir,
	// vaxt ala bilər).
	recentApp, err := s.repo.GetRecentPendingApplication(ctx, pin, phone, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to check recent applications: %w", err)
	}
	return recentApp, nil
}

// initBlockReason — PR #505: init/preflight paylaşan blok yoxlamaları.
// nil → yeni müraciət icazəlidir; error → blok səbəbi (istifadəçiyə göstərilən
// mesaj və ya BLOCKED_REJECTION_* protokol prefiksi — frontend parse edir).
func (s *ApplicationService) initBlockReason(ctx context.Context, pin string) error {
	// PR #70: Check for duplicate — customer must not have an existing non-final application.
	// PR #208: pending_customer blocklanmır (yarımçıq müraciət → təkrar cəhd mümkün).
	// PR #247: pending_expert blocklanır — ekspert təsdiqində olan FIN-yə yeni müraciət yox
	// (fərqli mobil nömrədən gəlsə belə).
	// PR #256: HasPendingApplication daysRemaining də qaytarır.
	existingID, existingStatus, daysRemaining, err := s.repo.HasPendingApplication(ctx, pin)
	if err != nil {
		return fmt.Errorf("failed to check existing applications: %w", err)
	}
	if existingID > 0 {
		// PR #256: blocked rejection halında fərqli error mesajı (frontend parse edir)
		if existingStatus == "rejected" {
			if daysRemaining == 0 {
				return fmt.Errorf("BLOCKED_REJECTION_PERMANENT")
			}
			return fmt.Errorf("BLOCKED_REJECTION_DAYS:%d", daysRemaining)
		}
		return fmt.Errorf("Sizin artıq işlənməkdə olan müraciətiniz var (№%d, status: %s). Bu müraciət həll olunana qədər yeni müraciət edə bilməzsiniz", existingID, existingStatus)
	}

	// PR #487 (PR #490: pəncərə 24h → 1h): anti-enumeration — FIN başına
	// son 1 saatda 3+ SERIAL_MISMATCH rəddi varsa yeni müraciət yaradılmır
	// (robotlarla seriya combination axtarışına qarşı; typo edən maksimum 1 saat gözləyir).
	mismatchCount, err := s.repo.CountRecentSerialMismatches(ctx, pin, 1)
	if err != nil {
		slog.Warn("PR #487: failed to count serial mismatches — fail-soft (allowing)",
			"customer_pin", pin, "error", err)
	} else if mismatchCount >= s.serialMismatchBlockLimit {
		slog.Warn("PR #487: serial mismatch limit reached — new application blocked",
			"customer_pin", pin,
			"mismatch_count_1h", mismatchCount,
			"limit", s.serialMismatchBlockLimit)
		return fmt.Errorf("Çoxlu yanlış cəhd qeydə alınıb. Zəhmət olmasa 1 saat sonra yenidən cəhd edin")
	}
	return nil
}

// InitPreflightResponse — POST /api/applications/init/preflight cavabı (PR #505).
type InitPreflightResponse struct {
	OK        bool   `json:"ok"`
	Reusable  bool   `json:"reusable,omitempty"` // son 10 dəq pending_customer app var — init reuse edəcək
	ErrorCode string `json:"code,omitempty"`     // ACTIVE_APPLICATION | SERIAL_MISMATCH_LIMIT | BLOCKED_REJECTION_PERMANENT | BLOCKED_REJECTION_DAYS
	Error     string `json:"error,omitempty"`    // istifadəçiyə göstəriləcək mesaj (BLOCKED_REJECTION_* üçün boş — frontend öz UI-ı var)
	RetryDays int    `json:"retry_days,omitempty"`
}

// PreflightInitApplication — PR #505: init-in yan-təsirsiz (side-effect-free)
// versiyası. Heç bir müraciət yaratmır, OTP göndərmir; yalnız init-in blok
// yoxlamalarını işlədib nəticəni qaytarır ki frontend “OTP Göndər” klikinə
// qədər xəbərdarlıq göstərə bilsin. Əsl icazə hər zaman init-in özündə verilir.
func (s *ApplicationService) PreflightInitApplication(ctx context.Context, req *InitApplicationRequest) (*InitPreflightResponse, error) {
	if req.CustomerPIN == "" || req.CustomerPhone == "" {
		return nil, fmt.Errorf("customer_pin and customer_phone are required")
	}

	// 1) Reuse: son 10 dəqiqədə yarımçıq (pending_customer) müraciət varsa blok deyil
	recentApp, err := s.findReusableApplication(ctx, req.CustomerPIN, req.CustomerPhone)
	if err != nil {
		return nil, err
	}
	if recentApp != nil {
		return &InitPreflightResponse{OK: true, Reusable: true}, nil
	}

	// 2) Bloklar: aktiv müraciət / rejection cooldown / serial-mismatch limiti
	blockErr := s.initBlockReason(ctx, req.CustomerPIN)
	if blockErr == nil {
		return &InitPreflightResponse{OK: true}, nil
	}
	msg := blockErr.Error()
	switch {
	case msg == "BLOCKED_REJECTION_PERMANENT":
		return &InitPreflightResponse{OK: false, ErrorCode: "BLOCKED_REJECTION_PERMANENT"}, nil
	case strings.HasPrefix(msg, "BLOCKED_REJECTION_DAYS:"):
		days, _ := strconv.Atoi(strings.TrimPrefix(msg, "BLOCKED_REJECTION_DAYS:"))
		return &InitPreflightResponse{OK: false, ErrorCode: "BLOCKED_REJECTION_DAYS", RetryDays: days}, nil
	case strings.HasPrefix(msg, "Çoxlu yanlış"):
		return &InitPreflightResponse{OK: false, ErrorCode: "SERIAL_MISMATCH_LIMIT", Error: msg}, nil
	default:
		// Aktiv müraciət bloku — mesaj istifadəçiyə göstərilə bilər (init ilə eyni)
		return &InitPreflightResponse{OK: false, ErrorCode: "ACTIVE_APPLICATION", Error: msg}, nil
	}
}

// VerifyInitApplicationRequest is the body for POST /api/applications/init/verify.
// PR #191: ApplicationPublicID UUID qəbul edir (ApplicationID INT backward-compat üçün qalır).
type VerifyInitApplicationRequest struct {
	ApplicationID       int    `json:"application_id"`        // backward-compat (köhnə frontend)
	ApplicationPublicID string `json:"application_public_id"` // PR #191: UUID
	Phone               string `json:"phone"`
	OTPCode             string `json:"otp_code"`
}

// VerifyResult — init/verify və KYC polling-in ortaq cavabı (PR #507).
// KycPending=true → KYC hələ təsdiq olunmayıb, frontend qısa sorğularla
// (GET /api/applications/{id}/kyc-status) gözləməyə davam edir.
type VerifyResult struct {
	App        *model.LoanApplication
	KycPending bool
}

// VerifyInitApplication verifies the OTP code and transitions the application
// PR #221: cutoff-lar OTP verify-də işləyir, amma status pending_customer qalır.
// Customer-confirm-də pending_expert-ə keçir.
//
// PR #507: KYC gözləməsi artıq BU requestdə 180s uzanmır (proxy-lər ~30s-də
// kəsir — LiteSpeed). Burada yalnız QISA pəncərə (~15s) gözlənilir; təsdiq
// gəlməsə KycPending=true qaytarılır və frontend qısa polling-lə davam edir.
//
// PR #117: AZMK KYC və Partner registration OTP-dən SONRA, cutoff-dan ƏVVƏL baş verir.
// PR #112: AUTO cutoff yoxlamaları KYC-dən sonra, kredit təklifindən ƏVVƏL baş verir.
// PR #191: application_public_id UUID ilə axtarır (application_id INT backward-compat).
//
// Flow:
//  1. OTP verify
//  2. AZMK KYC (create session → verify → get kyc_id)
//  3. AZMK Partner registration (get partner_id)
//  4. AUTO cutoff checks (AKB, blacklist, age, delay, active loan, etc.)
//  5. If all pass → pending_customer qalır (PR #221 — customer-confirm-də pending_expert-ə keçəcək)
func (s *ApplicationService) VerifyInitApplication(ctx context.Context, req *VerifyInitApplicationRequest) (*VerifyResult, error) {
	if req.ApplicationPublicID == "" && req.ApplicationID <= 0 {
		return nil, fmt.Errorf("application_id is required")
	}
	if req.Phone == "" || req.OTPCode == "" {
		return nil, fmt.Errorf("phone and otp_code are required")
	}

	// 1. Verify OTP
	verifyResp, err := s.otpService.VerifyOTP(ctx, req.Phone, req.OTPCode)
	if err != nil {
		return nil, fmt.Errorf("OTP verification failed: %w", err)
	}
	if !verifyResp.Valid {
		// PR #496: aktiv OTP yoxdursa (kod artıq istehlak edilib / vaxtı bitib /
		// heç göndərilməyib) "yanlış kod" demək yanlışdır — müştəri eyni kodu
		// təkrar daxil edib (məs. bağlantı kəsilib, cavab görə bilməyib).
		// Aydın mesaj: yeni kod istəsin.
		if verifyResp.NotFound {
			return nil, fmt.Errorf("OTP kodu artıq istifadə olunub və ya vaxtı bitib. Zəhmət olmasa yeni kod istəyin.")
		}
		return nil, fmt.Errorf("invalid OTP code, %d attempts remaining", verifyResp.Attempts)
	}

	// 2. Fetch application — PR #191: public_id UUID ilə (fallback: INT id)
	// PR #193: əgər application_public_id UUID formatında deyilsə, INT fallback et
	var app *model.LoanApplication
	if req.ApplicationPublicID != "" {
		if _, parseErr := uuid.Parse(req.ApplicationPublicID); parseErr == nil {
			// Valid UUID — use public_id lookup
			app, err = s.repo.GetApplicationByPublicID(ctx, req.ApplicationPublicID)
		} else if req.ApplicationID > 0 {
			// Not a valid UUID — fallback to INT
			app, err = s.repo.GetApplicationByID(ctx, req.ApplicationID)
		} else {
			return nil, fmt.Errorf("müraciət tapılmadı")
		}
	} else if req.ApplicationID > 0 {
		app, err = s.repo.GetApplicationByID(ctx, req.ApplicationID)
	} else {
		return nil, fmt.Errorf("müraciət tapılmadı")
	}
	if err != nil {
		return nil, fmt.Errorf("müraciət tapılmadı")
	}
	if app == nil {
		return nil, fmt.Errorf("müraciət tapılmadı")
	}
	if app.Status != model.StatusPendingCustomer {
		return nil, fmt.Errorf("application is not in pending_customer status (current: %s)", app.Status)
	}

	// 3. PR #487: İdentiklik qapısı — KYC-dən ƏVVƏL.
	// Cheap-first: AZMK_GET_PERSONAL_INFO (ən ucuz) serial+yaş yoxlayır; yalnız
	// keçən müraciət bahalı KYC/Partner/AKB mərhələsinə düşür.
	// PR #259: SetAuditAppID race — əvəzinə context value (yuxarıda set olunub).
	appID := app.ID
	ctx = azmk.WithAppID(ctx, &appID)

	// PR #516: verify sinxron pəncərəsinə zaman büdcəsi (VERIFY_SYNC_BUDGET_S,
	// default 20s). Proxy (LiteSpeed) ~30s-də cavabı kəsib HTML error səhifəsi
	// qaytarır → frontend xam JSON xətası görürdü. Büdcə həmişə limitdən tez bitir:
	//   - gate zamanı bitərsə  → fail-soft skip (mövcud davranış, PR #488)
	//   - KYC create bitərsə   → SERVICE_ERROR reject (PR #477 kanalı, SMS yox)
	//   - poll bitərsə         → kyc_pending (frontend qısa polling-lə davam edir)
	// finishPostKyc (partner+cutoff-lar) büdcə XARİCİNDƏ, orijinal ctx ilə işləyir —
	// KYC təsdiqli müştərini büdcə səbəbiylə rədd etmək olmaz.
	reqCtx := ctx
	if s.verifySyncBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.verifySyncBudget)
		defer cancel()
	}

	gateRejection, err := s.runIdentityGate(ctx, app)
	if err != nil {
		// Texniki xəta — fail-soft: qapı skip, axın davam edir (köhnə davranış)
		slog.Error("PR #487: identity gate technical error — proceeding (fail-soft)",
			"application_id", app.ID, "error", err)
	} else if gateRejection != "" {
		app.Status = model.StatusRejected
		app.RejectionReason = gateRejection
		if err := s.repo.UpdateApplicationDecision(ctx, app.ID,
			app.Status, "", gateRejection, 0, 0, 0); err != nil {
			return nil, fmt.Errorf("failed to save gate rejection: %w", err)
		}
		slog.Info("PR #487: identity gate rejected",
			"application_id", app.ID,
			"customer_pin", app.CustomerPIN,
			"rejection_reason", gateRejection)
		// PR #487: SERIAL_MISMATCH (typo) və SERIAL_MISMATCH_BLOCKED üçün SMS YOX —
		// müştəri ekrandadır, UI mesajı kifayətdir. AGE_* üçün SMS gedir
		// (mövcud cutoff rəddləri ilə uyğun davranış, PR #362).
		if gateRejection != "SERIAL_MISMATCH" && gateRejection != "SERIAL_MISMATCH_BLOCKED" {
			s.sendRejectionSMS(ctx, app)
		}
		return &VerifyResult{App: app}, nil
	}

	// 4. PR #117/#507: AZMK KYC — sessiyanı yaradıb QISA pəncərədə gözlə.
	// Müştəri kimliyini təsdiq etmədən cutoff yoxlamaq mənasızdır.
	// (appID ctx-ə yuxarıda — identity gate-dən əvvəl — set olunub, PR #487.)
	// PR #170: KYC verify toggle — əgər enabled=false isə KYC skip olunur
	if s.azmkProvider != nil && s.kycVerifyEnabled {
		slog.Info("AZMK KYC verify enabled — starting KYC + Partner registration",
			"application_id", app.ID)
		kycID, err := s.azmkKycCreate(ctx, app)
		if err != nil {
			// PR #516: büdcə KYC create zamanı bitdi — rədd yazısı büdcəsiz reqCtx
			// ilə yazılmalıdır (ölü ctx DB yazısını da işə salmazdı).
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				slog.Warn("PR #516: verify sync budget expired during KYC create",
					"application_id", app.ID,
					"budget_s", s.verifySyncBudget.Seconds())
			}
			return s.rejectOnKycError(reqCtx, app, err)
		}
		// PR #507: yalnız 6 cəhd (~15s) gözlə — 30s proxy limitindən uzaq.
		// Təsdif gəlməsə cavab kyc_pending qayıdır, gözləməyə frontend davam edir.
		verified, err := s.azmkKycPoll(ctx, app, kycID, 6)
		if err != nil {
			// PR #516: büdcə (DeadlineExceeded) poll zamanı bitdi — KYC sessiyası
			// YAŞAYIR, sadəcə vaxt çatmadı. Rədd yox: kyc_pending qaytarırıq, frontend
			// qısa polling-lə davam edir (PR #507 axını). Fiziki AZMK xətaları — reject.
			// (context.Canceled — PR #496 client disconnect; reject yazmırıq, amma
			// burada da pending qaytarmaq daha düzgündür: cavab müştəriyə çatmasa da,
			// müraciət üçün ən təhlükəsiz status preservasiyadır.)
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				slog.Warn("PR #516: verify sync budget expired during KYC poll — kyc_pending",
					"application_id", app.ID,
					"kyc_id", kycID,
					"budget_s", s.verifySyncBudget.Seconds())
				return &VerifyResult{App: app, KycPending: true}, nil
			}
			return s.rejectOnKycError(reqCtx, app, err)
		}
		if !verified {
			slog.Info("PR #507: KYC pending — frontend polling-ə keçir",
				"application_id", app.ID,
				"kyc_id", kycID)
			return &VerifyResult{App: app, KycPending: true}, nil
		}
		// PR #516: fast-path — KYC büdcə pəncərəsində təsdiqləndi; partner+cutoff-lar
		// (finishPostKyc) büdcəsiz reqCtx ilə işləyir.
		return s.finishPostKyc(reqCtx, app)
	} else if s.azmkProvider != nil && !s.kycVerifyEnabled {
		slog.Info("AZMK KYC verify DISABLED — skipping KYC + Partner registration",
			"application_id", app.ID)
	}

	return s.finishPostKyc(reqCtx, app)
}

// ErrKycServiceUnavailable — AZMK KYC/Partner xidmətinin TEXNİKİ xətasıdır
// (şəbəkə, timeout, 5xx). PR #477: belə xətalarda müraciət texniki səbəbdən
// rejected olur, amma müştəriyə imtina SMS-i GEDİRMİR — reject müştərinin
// özündən asılı deyil (PR #421 pattern-i: AZMK create rollback-da da SMS yoxdur).
// Müştərinin özündən asılı olan KYC imtinası (3 dəqiqə ərzində şəxsiyyətini
// təsdiq etməməsi) sentinel ilə wrap OL MUR — SMS gedir (imza timeout kimi).
var ErrKycServiceUnavailable = errors.New("kyc service unavailable")

// buildPartnerData — AZMK PartnerData-nı müraciət məlumatlarından qurur
// (KYC create və Partner register eyni datanı istifadə edir). PR #507.
func (s *ApplicationService) buildPartnerData(app *model.LoanApplication) azmk.PartnerData {
	// AZMK expects phone without +994 prefix (məs. "513153393")
	phone := strings.TrimPrefix(app.CustomerPhone, "+994")
	return azmk.PartnerData{
		AsanFinanceEmployeeInfo: false,
		AsanFinancePersonalInfo: false,
		FirstName:               "-", // müştəri info hələ yoxdur
		LastName:                "-",
		Mkr:                     false,
		Mobile:                  phone,
		Pin:                     app.CustomerPIN,
		BranchCode:              s.azmkBranch,
		Passport:                app.CustomerSerial,
		HomeAddress:             "-",
	}
}

// azmkKycCreate — AZMK KYC sessiyasını yaradır və kyc_id-ni DƏRHAL DB-yə yazır
// (PR #507: kyc-status polling endpoint-i app.KycID üzərindən işləyir; əvvəl
// kyc_id yalnız partner qeydiyyatından SONRA saxlanırdı).
func (s *ApplicationService) azmkKycCreate(ctx context.Context, app *model.LoanApplication) (string, error) {
	kycID, err := s.azmkProvider.KYC(ctx, &azmk.KYCRequest{PartnerData: s.buildPartnerData(app)})
	if err != nil {
		slog.Error("AZMK KYC creation failed",
			"application_id", app.ID,
			"customer_pin", app.CustomerPIN,
			"error", err)
		return "", fmt.Errorf("KYC yaradıla bilmədi: %w: %w", err, ErrKycServiceUnavailable)
	}
	slog.Info("PR #281: step 2 — AZMK KYC session created",
		"step", "2.kyc_create",
		"application_id", app.ID,
		"kyc_id", kycID)

	app.KycID = kycID
	if err := s.repo.UpdateApplicationDetails(ctx, app.ID, app); err != nil {
		// Non-fatal: ID yaddaşda da var, növbəti update-də yazılacaq
		slog.Warn("PR #507: failed to save kyc_id immediately (non-fatal)",
			"application_id", app.ID,
			"error", err)
	}
	return kycID, nil
}

// azmkKycPoll — KYC statusunu AZMK-dan maxAttempts dəfə (3s aralıqla) yoxlayır.
// PR #507: verify yalnız QISA pəncərə (~15s, 6 cəhd) üçün istifadə edir —
// uzun gözləmə (180s) proxy-lərdə kəsildiyindən frontend polling ilə aparılır.
// Cəhdlər bitib təsdiq gəlməsə (false, nil) — bu XƏTA deyil, pending deməkdir.
func (s *ApplicationService) azmkKycPoll(ctx context.Context, app *model.LoanApplication, kycID string, maxAttempts int) (bool, error) {
	const kycPollInterval = 3 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		verified, err := s.azmkProvider.VerifyKYC(ctx, kycID)
		if err != nil {
			// PR #496: client disconnect — servis xətası DEYİL. Raw qaytarılır ki,
			// çağıran tərəf errors.Is(err, context.Canceled) ilə tanısın və reject YAZMASIN.
			if errors.Is(err, context.Canceled) {
				slog.Warn("PR #496: client disconnected during KYC polling",
					"application_id", app.ID,
					"kyc_id", kycID,
					"attempt", attempt)
				return false, err
			}
			slog.Error("AZMK KYC verify failed — invalid ID",
				"application_id", app.ID,
				"kyc_id", kycID,
				"attempt", attempt,
				"error", err)
			return false, fmt.Errorf("KYC yoxlanıla bilmədi: %w: %w", err, ErrKycServiceUnavailable)
		}
		if verified {
			slog.Info("AZMK KYC verified",
				"application_id", app.ID,
				"kyc_id", kycID,
				"attempt", attempt)
			return true, nil
		}
		slog.Info("AZMK KYC not verified yet, polling...",
			"application_id", app.ID,
			"kyc_id", kycID,
			"attempt", attempt,
			"max_attempts", maxAttempts)
		if attempt < maxAttempts {
			// PR #496: ctx-aware gözləmə — disconnect olanda dərhal çıxırıq.
			select {
			case <-ctx.Done():
				slog.Warn("PR #496: client disconnected during KYC polling (wait)",
					"application_id", app.ID,
					"kyc_id", kycID,
					"attempt", attempt)
				return false, ctx.Err()
			case <-time.After(kycPollInterval):
			}
		}
	}
	return false, nil
}

// azmkPartnerRegister — AZMK Partner qeydiyyatı (kycId ilə) + partner_id-nin
// DB-yə yazılması. PR #507: runAzmkKycAndPartner-dan ayrılıb.
func (s *ApplicationService) azmkPartnerRegister(ctx context.Context, app *model.LoanApplication) error {
	pd := s.buildPartnerData(app)
	pd.KycID = app.KycID
	partnerID, err := s.azmkProvider.RegisterPartner(ctx, &azmk.PartnerRequest{PartnerData: pd})
	if err != nil {
		slog.Error("AZMK Partner registration failed",
			"application_id", app.ID,
			"kyc_id", app.KycID,
			"error", err)
		return fmt.Errorf("Partner qeydiyyatı uğursuz: %w: %w", err, ErrKycServiceUnavailable)
	}
	slog.Info("PR #281: step 3 — AZMK Partner registered",
		"step", "3.partner_register",
		"application_id", app.ID,
		"kyc_id", app.KycID,
		"partner_id", partnerID)

	app.PartnerID = partnerID
	if err := s.repo.UpdateApplicationDetails(ctx, app.ID, app); err != nil {
		slog.Error("AZMK: failed to save kyc_id/partner_id",
			"application_id", app.ID,
			"error", err)
		// Non-fatal: IDs are in memory, will be saved on next UpdateApplicationDetails call
	}
	return nil
}

// rejectOnKycError — KYC/Partner xətasında müraciəti rejected edir.
// PR #496: client disconnect (context.Canceled) → reject YOX — xəta yuxarı
// ötürülür, müraciət pending_customer qalır.
// PR #477: texniki servis xətası (ErrKycServiceUnavailable) → imtina SMS-i YOX.
// PR #362: müştərinin özündən asılı imtina → SMS gedir.
func (s *ApplicationService) rejectOnKycError(ctx context.Context, app *model.LoanApplication, kycErr error) (*VerifyResult, error) {
	if errors.Is(kycErr, context.Canceled) {
		slog.Warn("PR #496: client disconnected during KYC — application stays pending_customer",
			"application_id", app.ID,
			"customer_pin", app.CustomerPIN)
		return nil, kycErr
	}
	app.Status = model.StatusRejected
	app.RejectionReason = kycErr.Error()
	if err := s.repo.UpdateApplicationDecision(ctx, app.ID,
		app.Status, "", app.RejectionReason, 0, 0, 0); err != nil {
		return nil, fmt.Errorf("failed to save KYC rejection: %w", err)
	}
	slog.Info("AZMK KYC: application rejected",
		"application_id", app.ID,
		"customer_pin", app.CustomerPIN,
		"reason", app.RejectionReason)
	if errors.Is(kycErr, ErrKycServiceUnavailable) {
		slog.Warn("PR #477: KYC service error — rejection SMS skipped",
			"application_id", app.ID,
			"error", kycErr)
	} else {
		// PR #362: KYC reject — müştəriyə imtina SMS-i (non-fatal)
		s.sendRejectionSMS(ctx, app)
	}
	return &VerifyResult{App: app}, nil
}

// finishPostKyc — KYC təsdiqləndikdən SONRAKI addımlar (PR #507): Partner
// qeydiyyatı → early cutoff-lar → final status. Həm verify-nin fast-path-i,
// həm kyc-status polling-i eyni məntiqi işlədir (single source of truth).
// Eyni app üzrə paralel çağırış TryLock ilə bloklanır (partner double-register qarşısı).
func (s *ApplicationService) finishPostKyc(ctx context.Context, app *model.LoanApplication) (*VerifyResult, error) {
	muAny, _ := s.kycFinishLocks.LoadOrStore(app.ID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	if !mu.TryLock() {
		slog.Info("PR #507: finishPostKyc already in progress — returning pending",
			"application_id", app.ID)
		return &VerifyResult{App: app, KycPending: true}, nil
	}
	defer mu.Unlock()

	// Partner qeydiyyatı — KYC disabled → skip; əvvəldən keçibsə → skip (idempotensiya)
	if s.azmkProvider != nil && s.kycVerifyEnabled && app.PartnerID == "" {
		if err := s.azmkPartnerRegister(ctx, app); err != nil {
			return s.rejectOnKycError(ctx, app, err)
		}
	}

	// PR #112: Early AUTO cutoff yoxlamaları — müştəri kredit təklifi görməzdən əvvəl.
	rejectionReason, err := s.runEarlyCutoffChecks(ctx, app)
	if err != nil {
		slog.Error("early cutoff checks failed — proceeding to pending_expert (fail-soft)",
			"application_id", app.ID,
			"customer_pin", app.CustomerPIN,
			"error", err)
		// Fail-soft: cutoff xətası olanda müştərini bloklamırıq — normal flow davam edir
	} else if rejectionReason != "" {
		// Cutoff rədd etdi — statusu rejected et və səbəci yaz
		// PR #168: cutoff nəticəsini log et
		s.logCutoff(ctx, app.ID, rejectionReason, rejectionReason, "", true, false, "", "", "Müraciət bu kesim nöqtəsinə görə rədd edildi")
		app.Status = model.StatusRejected
		app.RejectionReason = rejectionReason
		if err := s.repo.UpdateApplicationDecision(ctx, app.ID,
			app.Status, "", rejectionReason, 0, 0, 0); err != nil {
			return nil, fmt.Errorf("failed to save rejection: %w", err)
		}
		slog.Info("early cutoff: application rejected before offer",
			"application_id", app.ID,
			"customer_pin", app.CustomerPIN,
			"rejection_reason", rejectionReason)
		// PR #362: early cutoff reject — müştəriyə imtina SMS-i (non-fatal)
		s.sendRejectionSMS(ctx, app)
		return &VerifyResult{App: app}, nil
	}

	// Cutoff keçdi — PR #221: status pending_customer qalır (pending_expert-ə keçmə)
	// Cutoff-lar işlədi, amma istifadəçi hələ "Təsdiq edirəm" düyməsini klikləməyib.
	// Customer-confirm-də pending_expert-ə keçəcək (RDC dashboard-a göndərilir).
	slog.Info("PR #281: step 1-4 completed — OTP verified, KYC passed, partner registered, cutoffs passed",
		"step", "1.otp_verify",
		"application_id", app.ID,
		"customer_pin", app.CustomerPIN,
		"kyc_id", app.KycID,
		"partner_id", app.PartnerID)

	return &VerifyResult{App: app}, nil
}

// PollKycStatus — GET /api/applications/{id}/kyc-status (PR #507).
// Frontend KYC gözləyərkən hər 3s çağırır; hər sorğu QISADIR (bir AZMK status
// çeki, ~100ms) — heç bir proxy timeout-u vurmur. KYC təsdiqlənəndə
// partner+cutoff-lar BURADA bitirilir və final cavab qayıdır.
func (s *ApplicationService) PollKycStatus(ctx context.Context, publicID string) (*VerifyResult, error) {
	if publicID == "" {
		return nil, fmt.Errorf("invalid public id")
	}
	app, err := s.repo.GetApplicationByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("müraciət tapılmadı")
	}

	// Final statuslar — olduğu kimi qaytar (rejected → frontend rədd ekranı göstərir)
	if app.Status != model.StatusPendingCustomer {
		return &VerifyResult{App: app}, nil
	}
	// Partner onsuz da qeydiyyatdan keçibsə bu app artıq finish edilib (idempotensiya)
	if app.PartnerID != "" {
		return &VerifyResult{App: app}, nil
	}
	if app.KycID == "" {
		return nil, fmt.Errorf("KYC sessiyası tapılmadı")
	}
	if s.azmkProvider == nil || !s.kycVerifyEnabled {
		return nil, fmt.Errorf("KYC verify deaktivdir")
	}

	verified, err := s.azmkProvider.VerifyKYC(ctx, app.KycID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		// Transient AZMK xətası — reject ETMƏ: növbəti poll təkrar yoxlayacaq
		// (countdown 3 dəqiqə ilə sərhədlənir).
		slog.Warn("PR #507: KYC status poll failed — will retry on next poll",
			"application_id", app.ID,
			"kyc_id", app.KycID,
			"error", err)
		return &VerifyResult{App: app, KycPending: true}, nil
	}
	if !verified {
		return &VerifyResult{App: app, KycPending: true}, nil
	}
	slog.Info("PR #507: KYC verified via polling — finishing application",
		"application_id", app.ID,
		"kyc_id", app.KycID)
	return s.finishPostKyc(ctx, app)
}

// serialMatches compares the AZMK document seria (məs. "AZE1234567") with the
// serial entered by the customer (prefix + 7 rəqəm). PR #486.
// Case-insensitive, boşluq və defislər nəzərə alınmır.
func serialMatches(azmkSeria, enteredSerial string) bool {
	norm := func(s string) string {
		s = strings.ToUpper(strings.TrimSpace(s))
		s = strings.ReplaceAll(s, " ", "")
		s = strings.ReplaceAll(s, "-", "")
		return s
	}
	return norm(azmkSeria) == norm(enteredSerial)
}

// runIdentityGate — PR #487: identiklik qapısı. OTP verify-dən DƏRHAL sonra,
// KYC-dən ƏVVƏL işə düşür (KYC/Partner/AKB bahalıdır — identiklik ən ucuz
// servis olan AZMK_GET_PERSONAL_INFO (10 qəpik) ilə yoxlanılır).
//
// Yoxlamalar (sıra ilə):
//  1. Anti-enumeration hard-stop: son 1 saatda (PR #490: 24h → 1h) 3+ SERIAL_MISMATCH
//     varsa — AZMK
//     çağrılmadan rədd (pulu yığılır). Limit serialMismatchBlockLimit (default 3).
//  2. AZMK_GET_PERSONAL_INFO (cache-first, PR #486 açarı: PIN+serial):
//     - SERIAL_MISMATCH: cavabdakı DocumentSeriaNumber ≠ daxil edilən seriya → rədd.
//     SMS GEDİRMİR (typo halı — müştəri ekrandadır, UI mesajı kifayətdir).
//     - AGE_UNDER_18 / AGE_OVER_69: yaş 18-69 aralığında deyilsə → rədd (SMS gedir —
//     mövcud cutoff davranışı ilə uyğun). Bozuk BirthDate → Age()=0 → 18-dən kiçik kimi rədd.
//
// Fail-soft: provider nil, cutoff-lar deaktiv və ya AZMK texniki xətası (data=nil)
// olanda qapı skip olunur — müştəri bloklanmır (köhnə davranış).
//
// Returns: rejection reason ("" = keçdi) və ya texniki error.
func (s *ApplicationService) runIdentityGate(ctx context.Context, app *model.LoanApplication) (string, error) {
	// Cutoff-lar deaktivdirsə qapı da deaktiv (dev/test rejimi)
	if !s.cutoffChecksEnabled {
		slog.Info("PR #487: identity gate skipped — cutoff checks disabled",
			"application_id", app.ID)
		return "", nil
	}
	// Provider yoxdursa yaş/seriya mənbəyi yoxdur — skip
	if s.customerDataProvider == nil {
		slog.Info("PR #487: identity gate skipped — customerDataProvider is nil",
			"application_id", app.ID)
		return "", nil
	}

	appID := app.ID
	customerPIN := app.CustomerPIN
	serial := app.CustomerSerial

	// 1. Anti-enumeration hard-stop — AZMK çağrılmadan əvvəl.
	if s.serialMismatchBlockLimit > 0 {
		mismatchCount, err := s.repo.CountRecentSerialMismatches(ctx, customerPIN, 1)
		if err != nil {
			slog.Warn("PR #487: gate — mismatch count failed — fail-soft (continuing)",
				"application_id", appID, "error", err)
		} else if mismatchCount >= s.serialMismatchBlockLimit {
			slog.Warn("PR #487: gate — serial mismatch limit reached — rejecting without AZMK call",
				"application_id", appID,
				"customer_pin", customerPIN,
				"mismatch_count_1h", mismatchCount,
				"limit", s.serialMismatchBlockLimit)
			s.logCutoff(ctx, appID, "SERIAL_MISMATCH_BLOCKED", "Seriya cəhd limiti (1 saatda 3+)", "IDENTITY_GATE", false, false,
				fmt.Sprintf("mismatches_1h = %d, limit = %d", mismatchCount, s.serialMismatchBlockLimit), "attempts < limit", "")
			return "SERIAL_MISMATCH_BLOCKED", nil
		}
	}

	// 2. PERSONAL_INFO — cache-first (PR #486: açar PIN + serial).
	var data *azmk.CustomerData
	if cached, ok := s.GetCachedServiceResponse(ctx, &appID, "AZMK_GET_PERSONAL_INFO", customerPIN, serial); ok {
		data = customerDataFromCache(cached)
	}
	if data == nil {
		// PR #488: birbaşa provider çağırışı — fetchCustomerDataFromAzmk error-i
		// udur (fail-soft), amma qapı üçün error TİPİ kritikdir:
		// ErrCustomerNotFound (result=0 "tapılmadı") = definitiv mənfi cavab → rədd;
		// digər xətalar = texniki → fail-soft skip.
		pd, perr := s.customerDataProvider.GetPersonalInfo(ctx, customerPIN, serial)
		if perr != nil {
			if errors.Is(perr, azmk.ErrCustomerNotFound) {
				slog.Warn("PR #488: gate — AZMK result=0 (FIN+seriya mövcud deyil) — SERIAL_MISMATCH",
					"application_id", appID, "customer_pin", customerPIN,
					"serial", serial, "azmk_error", perr.Error())
				s.logCutoff(ctx, appID, "SERIAL_MISMATCH", "FIN+seriya kombinasiyası AZMK-da tapılmadı (result=0)", "AZMK_GET_PERSONAL_INFO", true, false,
					"result=0: Sorğuya uyğun nəticə tapılmadı", "kombinasiya mövcuddur", perr.Error())
				return "SERIAL_MISMATCH", nil
			}
			// Texniki xəta — fail-soft skip (müştərini bloklamırıq)
			slog.Warn("PR #488: gate — PERSONAL_INFO technical error — fail-soft skip",
				"application_id", appID, "customer_pin", customerPIN, "error", perr)
			s.logCutoff(ctx, appID, "IDENTITY_GATE_SKIPPED", "Identiklik qapısı — servis xətası, skip", "AZMK_GET_PERSONAL_INFO", false, true, "service error / empty", "serial+age checked", perr.Error())
			return "", nil
		}
		data = pd
	}
	// data nil halı (mock və ya gözlənilməz) — fail-soft skip
	if data == nil {
		slog.Warn("PR #487: gate — no personal data — fail-soft skip",
			"application_id", appID, "customer_pin", customerPIN)
		s.logCutoff(ctx, appID, "IDENTITY_GATE_SKIPPED", "Identiklik qapısı — data yoxdur, skip", "AZMK_GET_PERSONAL_INFO", false, true, "empty data", "serial+age checked", "")
		return "", nil
	}

	// 2a. SERIAL_MISMATCH — identiklik yoxlaması.
	if serial != "" && data.DocumentSeriaNumber != "" && !serialMatches(data.DocumentSeriaNumber, serial) {
		slog.Error("PR #487: gate — SERIAL_MISMATCH",
			"application_id", appID, "customer_pin", customerPIN,
			"azmk_seria", data.DocumentSeriaNumber, "entered_serial", serial)
		s.logCutoff(ctx, appID, "SERIAL_MISMATCH", "Sənəd seriyası FIN kodu ilə uyğun gəlmir", "AZMK_GET_PERSONAL_INFO", true, false,
			fmt.Sprintf("azmk = %s, entered = %s", data.DocumentSeriaNumber, serial), "seriya uyğun", "")
		return "SERIAL_MISMATCH", nil
	}

	// 2b. Yaş qapısı — 18 ≤ yaş ≤ 69 (PR #487: AGE_UNDER_18 əlavə olundu).
	age := data.Age()
	if age < 18 {
		slog.Info("PR #487: gate — AGE_UNDER_18",
			"application_id", appID, "customer_pin", customerPIN, "age", age)
		s.logCutoff(ctx, appID, "AGE_UNDER_18", "Yaşı 18-dən aşağı olduqda imtina", "AZMK_GET_PERSONAL_INFO", true, false,
			fmt.Sprintf("age = %d", age), "18 <= age", "")
		return "AGE_UNDER_18", nil
	}
	if age > 69 {
		slog.Info("PR #487: gate — AGE_OVER_69",
			"application_id", appID, "customer_pin", customerPIN, "age", age)
		s.logCutoff(ctx, appID, "AGE_OVER_69", "Yaşı 69+ olduqda imtina", "AZMK_GET_PERSONAL_INFO", true, false,
			fmt.Sprintf("age = %d", age), "age <= 69", "")
		return "AGE_OVER_69", nil
	}

	// PR #243/#245: adı və qeydiyyat ünvanını DƏRHAL saxla (KYC-dən əvvəl —
	// dashboard-da adı tez göstərilir; cutoff zənciri sonra cache-HIT ilə oxuyur).
	if fullName := data.FullName(); fullName != "" && app.CustomerFullName == "" {
		app.CustomerFullName = fullName
		if err := s.repo.UpdateCustomerFullName(ctx, appID, fullName); err != nil {
			slog.Warn("failed to save customer full name to DB", "error", err)
		}
	}
	if data.RegistrationAddress != "" && app.RegistrationAddress == "" {
		app.RegistrationAddress = data.RegistrationAddress
		if err := s.repo.UpdateRegistrationAddress(ctx, appID, data.RegistrationAddress); err != nil {
			slog.Warn("failed to save registration address to DB", "error", err)
		}
	}

	slog.Info("PR #487: identity gate passed",
		"application_id", appID, "customer_pin", customerPIN, "age", age)
	return "", nil
}

// runEarlyCutoffChecks performs AUTO cutoff checks after OTP verification,
// before the customer sees the credit offer.
//
// PR #112: bu yoxlamalar məbləğdən asılı deyil — AKB, blacklist, yaş, gecikmə.
// Məbləğdən asılı olanlar (NO_COMMISSION_FOUND) customer-confirm-da yoxlanılır.
//
// Returns:
//   - ("", nil) — bütün yoxlamalar keçdi
//   - ("RULE_CODE", nil) — rədd səbəbi (məs. "AKB_SCORE_LOW")
//   - ("", error) — texniki xəta (fail-soft — müştərini bloklamırıq)
func (s *ApplicationService) runEarlyCutoffChecks(ctx context.Context, app *model.LoanApplication) (string, error) {
	if s.creditEngine == nil {
		slog.Warn("early cutoff: creditEngine is nil — skipping checks")
		return "", nil
	}

	customerPIN := app.CustomerPIN
	serial := app.CustomerSerial
	appID := app.ID
	// PR #171/#172: firstRejection — ilk rədd səbəbi.
	// cutoffStopOnFirstFail=true olanda ilk rədd-də dayanır.
	// cutoffStopOnFirstFail=false olanda bütün kesimlər yoxlanılır.
	// PR #278: CutoffChecksEnabled=false olanda cutoff-lar TAMAMƏN skip olunur.
	// Heç bir kesim yoxlanılmır — müraciət birbaşa pending_customer qalır.
	if !s.cutoffChecksEnabled {
		slog.Info("PR #281: step 4 — cutoff checks DISABLED — skipping all cutoffs", "step", "4.cutoff", "application_id", app.ID, "cutoff_enabled", false)
		return "", nil
	}

	firstRejection := ""
	// PR #198: hasServiceError — hər hansı xarici servis xətası olub?
	// Əgər varsa, ALL_CHECKS_PASSED true olmamalıdır (check edilməyib).
	hasServiceError := false

	// Helper: rədd olanda ya dərhal return, ya da davam et
	shouldReturn := func() bool {
		return firstRejection != "" && s.cutoffStopOnFirstFail
	}

	// Helper: rədd qeyd et (yalnız ilk rədd-i saxla)
	setRejection := func(reason string) {
		if firstRejection == "" {
			firstRejection = reason
		}
	}

	// 1. Qara siyahı və aktiv kredit yoxlaması (AZMK getOwnerData)
	if s.customerDataProvider != nil {
		// PR #205: cache yoxlaması (PR #379: appID — cache HIT marker row üçün)
		cachedResp, cacheHit := s.GetCachedServiceResponse(ctx, &appID, "AZMK_GET_OWNER_DATA", customerPIN, serial)
		if cacheHit {
			slog.Info("early cutoff: AZMK getOwnerData — using cached response", "application_id", appID, "customer_pin", customerPIN)
			// Cache-dən gələn response-u parse et
			var ownerResp azmk.OwnerDataResponse
			if err := json.Unmarshal([]byte(cachedResp), &ownerResp); err == nil && ownerResp.Data != nil {
				// Cache hit — servisi çağırma, birbaşa cached data istifadə et
				ownerData := ownerResp.Data
				blacklisted := ownerData.CustomerCheck.BlacklistStatus
				s.logCutoff(ctx, appID, "AZMK_BLACKLIST", "Qara siyahı yoxlaması (cached)", "AZMK_GET_OWNER_DATA", true, !blacklisted,
					fmt.Sprintf("blacklistStatus = %v (cached)", blacklisted), "blacklistStatus = false", "")
				if blacklisted {
					setRejection("AZMK_BLACKLIST")
				}
				if !shouldReturn() {
					hasActive := ownerData.CustomerCheck.HasActiveCredit
					s.logCutoff(ctx, appID, "ACTIVE_CREDIT", "Aktiv kredit yoxlaması (cached)", "AZMK_GET_OWNER_DATA", true, !hasActive,
						fmt.Sprintf("hasActiveCredit = %v (cached)", hasActive), "hasActiveCredit = false", "")
					if hasActive {
						setRejection("ACTIVE_CREDIT")
					}
				}
				goto afterOwnerData // cache hit — servis call-ı keç
			}
			// parse xətası → servisi çağır
		}

		slog.Info("early cutoff: calling AZMK getOwnerData", "application_id", appID, "customer_pin", customerPIN)
		ownerData, err := s.customerDataProvider.GetOwnerData(ctx, customerPIN, serial)
		if err != nil {
			slog.Warn("early cutoff: AZMK getOwnerData failed — fail-soft (skip)", "error", err)
			s.logCutoff(ctx, appID, "AZMK_BLACKLIST", "Qara siyahı yoxlaması", "AZMK_GET_OWNER_DATA", false, true, "service error", "blacklistStatus = false", err.Error())
			s.logCutoff(ctx, appID, "ACTIVE_CREDIT", "Aktiv kredit yoxlaması", "AZMK_GET_OWNER_DATA", false, true, "service error", "hasActiveCredit = false", err.Error())
			hasServiceError = true // PR #198
		} else if ownerData != nil {
			// Kesim #5: Qara siyahı
			blacklisted := ownerData.CustomerCheck.BlacklistStatus
			s.logCutoff(ctx, appID, "AZMK_BLACKLIST", "Qara siyahı yoxlaması", "AZMK_GET_OWNER_DATA", true, !blacklisted,
				fmt.Sprintf("blacklistStatus = %v", blacklisted), "blacklistStatus = false", "")
			if blacklisted {
				setRejection("AZMK_BLACKLIST")
			}
			if !shouldReturn() {
				// Kesim #6: Aktiv kredit
				hasActive := ownerData.CustomerCheck.HasActiveCredit
				s.logCutoff(ctx, appID, "ACTIVE_CREDIT", "Aktiv kredit yoxlaması", "AZMK_GET_OWNER_DATA", true, !hasActive,
					fmt.Sprintf("hasActiveCredit = %v", hasActive), "hasActiveCredit = false", "")
				if hasActive {
					setRejection("ACTIVE_CREDIT")
				}
			}
		}
	}
afterOwnerData: // PR #205: cache hit halında bura jump edilir
	if shouldReturn() {
		return firstRejection, nil
	}

	// 2. AKB skoru və stop-faktor (AZMK getMkrScore)
	if s.customerDataProvider != nil {
		slog.Info("early cutoff: calling AZMK getMkrScore", "application_id", appID, "customer_pin", customerPIN)
		// PR #380: 3 günlük cache — HIT olsa fiziki çağırış edilmir
		var mkrScore *azmk.MkrScore
		if cached, ok := s.GetCachedServiceResponse(ctx, &appID, "AZMK_GET_MKR_SCORE", customerPIN, serial); ok {
			mkrScore = mkrScoreFromCache(cached)
		}
		var err error
		if mkrScore == nil {
			mkrScore, err = s.customerDataProvider.GetMkrScore(ctx, customerPIN, serial)
		}
		if err != nil {
			slog.Warn("early cutoff: AZMK getMkrScore failed — fail-soft (skip)", "error", err)
			s.logCutoff(ctx, appID, "AKB_SCORE_LOW", "Skor balı yoxlaması", "AZMK_GET_MKR_SCORE", false, true, "service error", "point >= 200", err.Error())
			s.logCutoff(ctx, appID, "AKB_STOP_FACTOR", "Stop-faktor yoxlaması", "AZMK_GET_MKR_SCORE", false, true, "service error", "response ∉ {AB,NI,NU,TY}", err.Error())
			hasServiceError = true // PR #198
		} else if mkrScore != nil {
			point := mkrScore.Score.Point
			resp := strings.ToUpper(mkrScore.Score.Response)

			// PR #434: Stop-faktor (Kesim #4) ƏVVƏL yoxlanılır. AZMK stop-faktorlu
			// müştəriyə Point=1 (placeholder) göndərir — köhnə sıralamada AKB_SCORE_LOW
			// (2 gün) AKB_STOP_FACTOR (30 gün) əvəzinə işə düşürdü. Konvensiya:
			// resolveAkbScoreAndStopFactors (credit_engine.go) Point=1-i stop-faktor
			// kimi qəbul edir.
			stopFactor := resp == "AB" || resp == "NI" || resp == "NU" || resp == "TY"
			s.logCutoff(ctx, appID, "AKB_STOP_FACTOR", "AKB stop faktoruna düşən müştərilərə imtina", "AZMK_GET_MKR_SCORE", true, !stopFactor,
				fmt.Sprintf("response = %s", resp), "response ∉ {AB,NI,NU,TY}", fmt.Sprintf("point = %d", point))
			if stopFactor {
				setRejection(fmt.Sprintf("AKB_STOP_FACTOR:%s", resp))
			}

			if !shouldReturn() {
				// Kesim #1: Skor < 200
				scorePassed := !(point > 0 && point < 200)
				s.logCutoff(ctx, appID, "AKB_SCORE_LOW", "Skor balı 200-dən aşağı olduqda imtina", "AZMK_GET_MKR_SCORE", true, scorePassed,
					fmt.Sprintf("point = %d", point), "point >= 200", fmt.Sprintf("response = %s", resp))
				if !scorePassed {
					setRejection("AKB_SCORE_LOW")
				}
			}
			// PR #228: AZMK AKB score-u app.AkbScore-ə saxla və DB-yə yaz
			// ki customer-confirm-də GetOffer düzgün credit level hesablasın.
			// PR #434: Point=1 stop-faktor placeholder-idir — real skor deyil;
			// yazsaq DB cache (GetRecentAkbScore, dbScore > 0 → "stop-faktor
			// yoxdur") sonrakı aşkarlanmanı gizlədərdi.
			if point > 1 {
				app.AkbScore = point
				if err := s.repo.UpdateAkbScore(ctx, appID, point); err != nil {
					slog.Warn("failed to save AKB score to DB", "error", err)
				}
				slog.Info("AKB score saved from AZMK", "application_id", appID, "akb_score", point)
			}
		}
	} else {
		akbScore, stopFactorCode, hasStopFactor := s.creditEngine.resolveAkbScoreAndStopFactors(ctx, customerPIN, 0)
		s.logCutoff(ctx, appID, "AKB_SCORE_LOW", "Skor balı 200-dən aşağı olduqda imtina", "LW_GET_AKB_SCORE", true, !(akbScore > 0 && akbScore < 200),
			fmt.Sprintf("score = %d", akbScore), "score >= 200", "")
		if akbScore > 0 && akbScore < 200 {
			setRejection("AKB_SCORE_LOW")
		}
		if !shouldReturn() && hasStopFactor {
			s.logCutoff(ctx, appID, "AKB_STOP_FACTOR", "Stop-faktor", "LW_GET_AKB_SCORE", true, false,
				fmt.Sprintf("code = %s", stopFactorCode), "no stop factor", "")
			setRejection(fmt.Sprintf("AKB_STOP_FACTOR:%s", stopFactorCode))
		}
	}
	if shouldReturn() {
		return firstRejection, nil
	}

	// 3. Yaş yoxlaması (AZMK GetPersonalInfo)
	age := 0
	if s.customerDataProvider != nil {
		slog.Info("early cutoff: calling AZMK GetPersonalInfo (age check)", "application_id", appID, "customer_pin", customerPIN)
		// PR #381: 3 günlük cache (migration 053, cache_days=3) — HIT olsa fiziki
		// çağırış edilmir. Cached body tam cavabdır: yaş (BirthDate), fullName
		// və qeydiyyat ünvanı (PR #243/#245) eynilə cached cavabdan oxunur.
		var data *azmk.CustomerData
		if cached, ok := s.GetCachedServiceResponse(ctx, &appID, "AZMK_GET_PERSONAL_INFO", customerPIN, serial); ok {
			data = customerDataFromCache(cached)
		}
		if data == nil {
			data = s.fetchCustomerDataFromAzmk(ctx, customerPIN, serial)
		}
		var fullName string
		if data != nil {
			// PR #486 (Mərhələ B): AZMK cavabındakı DocumentSeriaNumber daxil edilən
			// seriya ilə müqayisə olunur — uyğunsuzluqda müraciət rədd edilir.
			// Bu, həm cache-dən, həm canlı cavabdan gələn dataya aiddir və AZMK
			// özü FIN-lə axtarıb seriyanı ignore etsə belə deşii bağlayır.
			if serial != "" && data.DocumentSeriaNumber != "" && !serialMatches(data.DocumentSeriaNumber, serial) {
				slog.Error("PR #486: serial mismatch — AZMK document seria does not match entered serial",
					"application_id", appID, "customer_pin", customerPIN,
					"azmk_seria", data.DocumentSeriaNumber, "entered_serial", serial)
				s.logCutoff(ctx, appID, "SERIAL_MISMATCH", "Sənəd seriyası FIN kodu ilə uyğun gəlmir", "AZMK_GET_PERSONAL_INFO", true, false,
					fmt.Sprintf("azmk = %s, entered = %s", data.DocumentSeriaNumber, serial), "seriya uyğun", "")
				setRejection("SERIAL_MISMATCH")
				if shouldReturn() {
					return firstRejection, nil
				}
			}
			age = data.Age()
			fullName = data.FullName()
			slog.Info("customer data resolved from AZMK",
				"customer_pin", customerPIN, "birth_date", data.BirthDate, "age", age, "name", fullName)
		}
		// PR #243: GetPersonalInfo cavabındakı adı saxla — customer-confirm
		// və video mərhələlərində eyni servisə ikinci sorğu göndərilməsin.
		if fullName != "" && app.CustomerFullName == "" {
			app.CustomerFullName = fullName
			if err := s.repo.UpdateCustomerFullName(ctx, appID, fullName); err != nil {
				slog.Warn("failed to save customer full name to DB", "error", err)
			} else {
				slog.Info("customer full name saved from AZMK", "application_id", appID)
			}
		}
		// PR #245: qeydiyyat ünvanını saxla — dashboard-da göstərilir (read-only).
		// Yalnız boş olanda yazılır (AZMK mənbəyi hər dəfə yenilənmir).
		if data != nil && data.RegistrationAddress != "" && app.RegistrationAddress == "" {
			app.RegistrationAddress = data.RegistrationAddress
			if err := s.repo.UpdateRegistrationAddress(ctx, appID, data.RegistrationAddress); err != nil {
				slog.Warn("failed to save registration address to DB", "error", err)
			} else {
				slog.Info("registration address saved from AZMK", "application_id", appID)
			}
		}
	} else {
		age = s.creditEngine.resolveCustomerAge(ctx, customerPIN, serial)
	}
	agePassed := age <= 69
	s.logCutoff(ctx, appID, "AGE_OVER_69", "Yaşı 69+ olduqda imtina", "AZMK_GET_PERSONAL_INFO", true, agePassed,
		fmt.Sprintf("age = %d", age), "age <= 69", "")
	if !agePassed {
		setRejection("AGE_OVER_69")
	}
	if shouldReturn() {
		return firstRejection, nil
	}

	// 4. Kredit tarixçəsi kesim nöqtələri (AZMK inquireByIdCard)
	if s.customerDataProvider != nil {
		slog.Info("early cutoff: calling AZMK inquireByIdCard", "application_id", appID, "customer_pin", customerPIN)
		// PR #380: 3 günlük cache — HIT olsa fiziki çağırış edilmir
		var creditHistory *azmk.CreditHistory
		if cached, ok := s.GetCachedServiceResponse(ctx, &appID, "AZMK_INQUIRE_BY_ID_CARD", customerPIN, serial); ok {
			creditHistory = creditHistoryFromCache(cached)
		}
		var err error
		if creditHistory == nil {
			creditHistory, err = s.customerDataProvider.InquireByIdCard(ctx, customerPIN, serial)
		}
		if err != nil {
			slog.Warn("early cutoff: AZMK inquireByIdCard failed — fail-soft (skip)", "error", err)
			s.logCutoff(ctx, appID, "DELAY_RATIO_HIGH", "Gecikmə əmsalı yoxlaması", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "ratio <= 6", err.Error())
			s.logCutoff(ctx, appID, "ACTIVE_DELAY_HIGH", "Aktiv cari gecikmə yoxlaması", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "delay <= 5", err.Error())
			s.logCutoff(ctx, appID, "DELAY_3M", "Son 3 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "< 20", err.Error())
			s.logCutoff(ctx, appID, "DELAY_6M", "Son 6 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "< 30", err.Error())
			s.logCutoff(ctx, appID, "DELAY_12M", "Son 12 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "< 45", err.Error())
			s.logCutoff(ctx, appID, "DELAY_18M", "Son 18 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "< 60", err.Error())
			s.logCutoff(ctx, appID, "MONTHLY_PAYMENTS_HIGH", "Aktiv aylıq ödəniş yoxlaması", "AZMK_INQUIRE_BY_ID_CARD", false, true, "service error", "<= 2000", err.Error())
			hasServiceError = true // PR #198
		} else if creditHistory != nil {
			// Kesim #2: Gecikmə əmsalı > 6
			ratio := creditHistory.MaxDelayRatio()
			ratioPassed := ratio <= 6
			s.logCutoff(ctx, appID, "DELAY_RATIO_HIGH", "Gecikmə günləri üzrə əmsal 6-dan yüksək olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, ratioPassed,
				fmt.Sprintf("maxRatio = %.2f", ratio), "ratio <= 6", "", creditHistory.MaxDelayRatioDetail())
			if !ratioPassed {
				setRejection("DELAY_RATIO_HIGH")
			}

			if !shouldReturn() {
				// Kesim #7: Aktiv cari gecikmə > 5
				curDelay := creditHistory.MaxCurrentDelay()
				curDelayPassed := curDelay <= 5
				s.logCutoff(ctx, appID, "ACTIVE_DELAY_HIGH", "Aktiv kreditlərində cari gün gecikməsi 5-dən artıq olanlara imtina", "AZMK_INQUIRE_BY_ID_CARD", true, curDelayPassed,
					fmt.Sprintf("maxCurrentDelay = %d", curDelay), "delay <= 5", "", creditHistory.MaxCurrentDelayDetail())
				if !curDelayPassed {
					setRejection("ACTIVE_DELAY_HIGH")
				}
			}
			if shouldReturn() {
				// Skip remaining delay checks — but still log them as not-checked
				s.logCutoff(ctx, appID, "DELAY_3M", "Son 3 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "skipped (earlier cutoff failed)", "< 20", "")
				s.logCutoff(ctx, appID, "DELAY_6M", "Son 6 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "skipped (earlier cutoff failed)", "< 30", "")
				s.logCutoff(ctx, appID, "DELAY_12M", "Son 12 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "skipped (earlier cutoff failed)", "< 45", "")
				s.logCutoff(ctx, appID, "DELAY_18M", "Son 18 ay max gecikmə", "AZMK_INQUIRE_BY_ID_CARD", false, true, "skipped (earlier cutoff failed)", "< 60", "")
				s.logCutoff(ctx, appID, "MONTHLY_PAYMENTS_HIGH", "Aktiv aylıq ödəniş yoxlaması", "AZMK_INQUIRE_BY_ID_CARD", false, true, "skipped (earlier cutoff failed)", "<= 2000", "")
			} else {
				// Kesim #8: Son 3 ay max gecikmə ≥ 20
				d3 := creditHistory.MaxDelay3M()
				d3Passed := d3 < 20
				s.logCutoff(ctx, appID, "DELAY_3M", "Son 3 ayda maksimal gecikmə 20+ olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, d3Passed,
					fmt.Sprintf("maxDelay3M = %d", d3), "< 20", "")
				if !d3Passed {
					setRejection("DELAY_3M")
				}

				if !shouldReturn() {
					// Kesim #9: Son 6 ay max gecikmə ≥ 30
					d6 := creditHistory.MaxDelay6M()
					d6Passed := d6 < 30
					s.logCutoff(ctx, appID, "DELAY_6M", "Son 6 ayda maksimal gecikmə 30+ olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, d6Passed,
						fmt.Sprintf("maxDelay6M = %d", d6), "< 30", "")
					if !d6Passed {
						setRejection("DELAY_6M")
					}
				}
				if !shouldReturn() {
					// Kesim #10: Son 12 ay max gecikmə ≥ 45
					d12 := creditHistory.MaxDelay12M()
					d12Passed := d12 < 45
					s.logCutoff(ctx, appID, "DELAY_12M", "Son 12 ayda maksimal gecikmə 45+ olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, d12Passed,
						fmt.Sprintf("maxDelay12M = %d", d12), "< 45", "")
					if !d12Passed {
						setRejection("DELAY_12M")
					}
				}
				if !shouldReturn() {
					// Kesim #11: Son 18 ay max gecikmə ≥ 60
					d18 := creditHistory.MaxDelay18M()
					d18Passed := d18 < 60
					s.logCutoff(ctx, appID, "DELAY_18M", "Son 18 ayda maksimal gecikmə 60+ olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, d18Passed,
						fmt.Sprintf("maxDelay18M = %d", d18), "< 60", "")
					if !d18Passed {
						setRejection("DELAY_18M")
					}
				}
				if !shouldReturn() {
					// Kesim #12: Aktiv aylıq ödəniş > 2000
					monthlyPay := creditHistory.TotalActiveMonthlyPayments()
					monthlyPassed := monthlyPay <= 2000
					s.logCutoff(ctx, appID, "MONTHLY_PAYMENTS_HIGH", "Aktiv aylıq ödənişlərin cəmi 2000 AZN-dən artıq olduqda imtina", "AZMK_INQUIRE_BY_ID_CARD", true, monthlyPassed,
						fmt.Sprintf("totalMonthly = %.2f", monthlyPay), "<= 2000", "", creditHistory.TotalActiveMonthlyPaymentsDetail())
					if !monthlyPassed {
						setRejection("MONTHLY_PAYMENTS_HIGH")
					}
				}
			}
		}
	} else {
		// Backward compatible: LW provider
		var analytics loanAnalytics
		s.creditEngine.resolveAkbHistory(ctx, customerPIN, serial, &analytics)
		if analytics.akbHistoryAvailable {
			s.logCutoff(ctx, appID, "DELAY_RATIO_HIGH", "Gecikmə əmsalı", "LW_GET_AKB_HISTORY", true, analytics.delayRatio <= 6,
				fmt.Sprintf("ratio = %.2f", analytics.delayRatio), "<= 6", "")
			if analytics.delayRatio > 6 {
				setRejection("DELAY_RATIO_HIGH")
			}
			if !shouldReturn() && analytics.activeMaxDelayDays > 5 {
				s.logCutoff(ctx, appID, "ACTIVE_DELAY_HIGH", "Aktiv cari gecikmə", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("delay = %d", analytics.activeMaxDelayDays), "<= 5", "")
				setRejection("ACTIVE_DELAY_HIGH")
			}
			if !shouldReturn() && analytics.maxDelayLast3Months >= 20 {
				s.logCutoff(ctx, appID, "DELAY_3M", "Son 3 ay", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("max = %d", analytics.maxDelayLast3Months), "< 20", "")
				setRejection("DELAY_3M")
			}
			if !shouldReturn() && analytics.maxDelayLast6Months >= 30 {
				s.logCutoff(ctx, appID, "DELAY_6M", "Son 6 ay", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("max = %d", analytics.maxDelayLast6Months), "< 30", "")
				setRejection("DELAY_6M")
			}
			if !shouldReturn() && analytics.maxDelayLast12Months >= 45 {
				s.logCutoff(ctx, appID, "DELAY_12M", "Son 12 ay", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("max = %d", analytics.maxDelayLast12Months), "< 45", "")
				setRejection("DELAY_12M")
			}
			if !shouldReturn() && analytics.maxDelayLast18Months >= 60 {
				s.logCutoff(ctx, appID, "DELAY_18M", "Son 18 ay", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("max = %d", analytics.maxDelayLast18Months), "< 60", "")
				setRejection("DELAY_18M")
			}
			if !shouldReturn() && analytics.totalMonthlyPayments > 2000 {
				s.logCutoff(ctx, appID, "MONTHLY_PAYMENTS_HIGH", "Aylıq ödəniş", "LW_GET_AKB_HISTORY", true, false,
					fmt.Sprintf("total = %.2f", analytics.totalMonthlyPayments), "<= 2000", "")
				setRejection("MONTHLY_PAYMENTS_HIGH")
			}
		}
	}

	if firstRejection != "" {
		slog.Info("early cutoff: rejection", "application_id", appID, "reason", firstRejection)
		return firstRejection, nil
	}

	// PR #198/#207: əgər hər hansı xarici servis xətası olubsa, ALL_CHECKS_PASSED true olmamalıdır.
	// PR #207: service error olanda müraciəti reject et — kredit təklifi verilməsin.
	if hasServiceError {
		slog.Warn("early cutoff: service errors occurred — rejecting application (checks not fully performed)",
			"application_id", appID, "customer_pin", customerPIN, "age", age)
		s.logCutoff(ctx, appID, "ALL_CHECKS_PASSED", "Bütün kesim nöqtələri keçdi (xəta var)", "", false, false,
			"service errors occurred", "all checks must pass", "Bəzi xarici servis xətaları baş verdi — check-lər tam yoxlanılmadı")
		return "SERVICE_ERROR", nil
	}

	slog.Info("early cutoff: all checks passed", "application_id", appID, "customer_pin", customerPIN, "age", age)
	s.logCutoff(ctx, appID, "ALL_CHECKS_PASSED", "Bütün kesim nöqtələri keçdi", "", true, true, "", "", "")
	return "", nil
}

// logCutoff writes a cutoff check result to the database.
// PR #168: plan/fakt nəticələri hər müraciət üçün.
// PR #174: calculationDetails — variadic, kompleks hesablamaların detalları.
// PR #175: calculationDetails artıq JSON strukturundadır (DelayRatioDetailJSON,
//
//	CurrentDelayDetailJSON, MonthlyPaymentsDetailJSON — pkg/azmk/customer_data.go).
func (s *ApplicationService) logCutoff(ctx context.Context, appID int, code, name, service string, checked, passed bool, actualValue, threshold, details string, calculationDetails ...string) {
	if s.cutoffRepo == nil {
		return
	}
	cd := ""
	if len(calculationDetails) > 0 {
		cd = calculationDetails[0]
	}
	cr := &model.CutoffResult{
		ApplicationID:      appID,
		CutoffCode:         code,
		CutoffName:         name,
		ServiceName:        service,
		Checked:            checked,
		Passed:             passed,
		ActualValue:        actualValue,
		Threshold:          threshold,
		Details:            details,
		CalculationDetails: cd,
	}
	if err := s.cutoffRepo.Insert(ctx, cr); err != nil {
		slog.Warn("failed to log cutoff result", "error", err, "cutoff_code", code)
	}
}

// CompleteApplicationRequest is the body for PUT /api/applications/{id}/complete.
// The expert fills in these fields after the customer verifies their phone.
type CompleteApplicationRequest struct {
	CustomerFullName string  `json:"customer_full_name"`
	Amount           float64 `json:"amount"`
	TermMonths       int     `json:"term_months"`
	LoanPurpose      string  `json:"loan_purpose"`
	AkbScore         int     `json:"akb_score"`
	CardNumber       string  `json:"card_number"`
	Contact1Phone    string  `json:"contact1_phone"`
	Contact2Phone    string  `json:"contact2_phone"`
	Contact3Phone    string  `json:"contact3_phone"`
	Contact1Relation string  `json:"contact1_relation"` // PR #85: Ata, Ana, Qardaş, etc.
	Contact2Relation string  `json:"contact2_relation"`
	Contact3Relation string  `json:"contact3_relation"`
	ActualAddress    string  `json:"actual_address"`
}

// CompleteApplication fills in the remaining fields and triggers the credit engine.
// Called by the expert after the customer has verified their phone.
//
// PR #58: validation relaxed. When the customer has already gone through the
// customer-confirm flow (POST /api/applications/{id}/customer-confirm), fields
// like customer_full_name, amount, term_months, card_number, actual_address,
// and akb_score are already populated. The expert's job is then to add the
// 3 contact phones (collected during the verification call) and trigger the
// engine.
//
// Validation rules (PR #58):
//   - contact1_phone is REQUIRED (expert must collect at least 1 contact)
//   - contact2_phone, contact3_phone are OPTIONAL
//   - If customer_full_name is empty in the DB AND empty in the request → error
//   - If amount is 0 in the DB AND 0 in the request → error
//   - If term_months is 0 in the DB AND 0 in the request → error
//   - If card_number is empty in the DB AND empty in the request → error
//
// In short: fields already filled by customer-confirm are NOT re-required.
// The expert can override them by providing non-zero values in the request.
func (s *ApplicationService) CompleteApplication(ctx context.Context, appID int, req *CompleteApplicationRequest) (*model.LoanApplication, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}
	if req.Contact1Phone == "" {
		return nil, fmt.Errorf("contact1_phone is required (expert must collect at least 1 contact)")
	}

	// 1. Fetch application
	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}
	if app.Status != model.StatusPendingExpert {
		return nil, fmt.Errorf("application is not in pending_expert status (current: %s)", app.Status)
	}

	// 2. Merge request fields into the existing application.
	// For each field: if the request provides a non-empty value, use it;
	// otherwise keep the existing DB value (which may have been set by
	// customer-confirm). After the merge, validate that all required fields
	// are populated.
	if req.CustomerFullName != "" {
		app.CustomerFullName = req.CustomerFullName
	}
	if req.Amount > 0 {
		app.Amount = req.Amount
	}
	if req.TermMonths > 0 {
		app.TermMonths = req.TermMonths
	}
	if req.CardNumber != "" {
		app.CardNumber = req.CardNumber
	}
	if req.ActualAddress != "" {
		app.ActualAddress = req.ActualAddress
	}
	if req.AkbScore > 0 {
		app.AkbScore = req.AkbScore
	}
	app.LoanPurpose = req.LoanPurpose
	app.Contact1Phone = req.Contact1Phone
	app.Contact2Phone = req.Contact2Phone
	app.Contact3Phone = req.Contact3Phone
	// PR #85: merge contact relations
	if req.Contact1Relation != "" {
		app.Contact1Relation = req.Contact1Relation
	}
	if req.Contact2Relation != "" {
		app.Contact2Relation = req.Contact2Relation
	}
	if req.Contact3Relation != "" {
		app.Contact3Relation = req.Contact3Relation
	}
	app.Status = model.StatusPending // will transition to "checking" by the engine

	// 3. Validate that all required fields are now populated (either from
	// customer-confirm or from the expert's request).
	if app.CustomerFullName == "" {
		return nil, fmt.Errorf("customer_full_name is required (not set by customer-confirm and not provided in request)")
	}
	if app.Amount <= 0 {
		return nil, fmt.Errorf("amount must be greater than zero (not set by customer-confirm and not provided in request)")
	}
	if app.TermMonths <= 0 {
		return nil, fmt.Errorf("term_months must be greater than zero (not set by customer-confirm and not provided in request)")
	}
	if len(app.CardNumber) != 16 {
		return nil, fmt.Errorf("card_number must be exactly 16 digits (current: %d)", len(app.CardNumber))
	}

	// 4. Save to DB
	if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
		return nil, fmt.Errorf("failed to update application: %w", err)
	}

	// 5. Trigger credit engine async
	s.triggerAsyncProcessing(app)

	slog.Info("application completed, credit engine triggered",
		"application_id", appID,
		"customer_pin", app.CustomerPIN,
		"amount", app.Amount,
		"term_months", app.TermMonths,
		"contact1_phone", app.Contact1Phone)

	// Return the updated app (status is now pending, engine will transition to checking)
	return s.repo.GetApplicationByID(ctx, appID)
}

// Ensure otp import is used
var _ = otp.Provider(nil)
var _ = time.Second
