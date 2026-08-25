package service

import (
        "context"
        "fmt"
        "log/slog"
        "strings"

        "rdc-source/internal/model"
        "rdc-source/pkg/azmk"
)

// UpdateStatusRequest is the request body for manually updating an application's status (mock/testing endpoint).
type UpdateStatusRequest struct {
        Status          string `json:"status"`           // "approved" or "rejected"
        CreditLevel     string `json:"credit_level"`     // required when status is "approved" (e.g. "new", "trusted", "valuable", "elite")
        RejectionReason string `json:"rejection_reason"` // PR #258/#287: MANUAL_* cutoff code (məs: MANUAL_VIDEO_MISMATCH)
}

// UpdateStatus manually sets an application's status.
// This is the manual approval/rejection endpoint used by operators.
//
// Rules:
//   - Only StatusApproved and StatusRejected are accepted.
//   - The application must be in StatusPendingApproval (set by the credit engine
//     for New/Trusted/Valuable levels after all checks pass).
//   - When status is StatusApproved, credit_level is required — it is stored on
//     the application so that CountApprovedAtLevel can find it for unlock_phase
//     calculation.
//   - When status is StatusRejected, credit_level is optional.
//
// PR #95: On approval, if the application has a discount_code, the discount is
// applied to the commission (total_amount is recalculated). The discount code
// is marked as 'used' atomically. A new discount code is generated for the
// approved customer and an SMS is sent with the new code.
func (s *ApplicationService) UpdateStatus(ctx context.Context, id int, req *UpdateStatusRequest) (*model.LoanApplication, error) {
        if id <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }

        // Validate status
        if req.Status != model.StatusApproved && req.Status != model.StatusRejected {
                return nil, fmt.Errorf("status must be '%s' or '%s', got '%s'",
                        model.StatusApproved, model.StatusRejected, req.Status)
        }

        // Fetch existing application to verify it exists
        app, err := s.repo.GetApplicationByID(ctx, id)
        if err != nil {
                return nil, err
        }

        // Validate that the application is awaiting expert review.
        // PR #226: pending_expert (PR #221 customer-confirm flow) və pending_approval
        // (legacy engine flow) — hər ikisində ekspert approve/reject edə bilər.
        if app.Status != model.StatusPendingApproval && app.Status != model.StatusPendingExpert {
                return nil, fmt.Errorf("application status is '%s', expected '%s' or '%s' — only applications awaiting expert review can be updated",
                        app.Status, model.StatusPendingExpert, model.StatusPendingApproval)
        }

        // Validate credit_level is provided for approvals
        if req.Status == model.StatusApproved && req.CreditLevel == "" {
                return nil, fmt.Errorf("credit_level is required when status is '%s'", model.StatusApproved)
        }

        // Validate credit_level value if provided
        if req.CreditLevel != "" && !model.IsValidCreditLevel(req.CreditLevel) {
                return nil, fmt.Errorf("credit_level must be one of new/trusted/valuable/elite, got '%s'", req.CreditLevel)
        }

        // Update via UpdateApplicationDecision so credit_level is stored
        creditLevel := req.CreditLevel
        if creditLevel == "" {
                creditLevel = app.CreditLevel // keep existing if not provided for rejection
        }

        var rejectionReason string
        var totalAmount float64
        var discountAmount float64

        if req.Status == model.StatusRejected {
                // PR #258: MANUAL_* cutoff code qəbul et (frontend-dən gəlir)
                // Əgər reason verilməyibsə, default "Manually rejected" istifadə et.
                rejectionReason = req.RejectionReason
                if rejectionReason == "" {
                        rejectionReason = "Manually rejected"
                }
        } else if req.Status == model.StatusApproved {
                // PR #95: if a discount code is present, validate it (race-condition
                // protection: between customer-confirm and approval, another customer
                // may have redeemed the same code), then compute the discount and
                // apply it to total_amount.
                if app.DiscountCode != "" && s.discountSvc != nil {
                        discountAmount, err = s.validateAndComputeDiscount(ctx, app)
                        if err != nil {
                                return nil, err
                        }
                }

                // Calculate total amount for manual approval (Principal + Interest)
                if discountAmount > 0 {
                        totalAmount = calculateTotalAmountWithDiscount(app.Amount, app.ApprovedRate, discountAmount)
                } else {
                        totalAmount = calculateTotalAmount(app.Amount, app.ApprovedRate) // ApprovedRate is commission
                }
        }

        err = s.repo.UpdateApplicationDecision(ctx, id,
                req.Status, creditLevel, rejectionReason, app.Amount, app.ApprovedRate, totalAmount)
        if err != nil {
                return nil, fmt.Errorf("failed to update status: %w", err)
        }

        // PR #258: manual reject-i cutoff_results cədvəlinə yaz.
        // Ekspert MANUAL_* səbəbi ilə imtina edəndə bu qeyd kəsilsin ki,
        // plan/fakt hesabatlarında görünsün və validity_days bloku işləsin.
        if req.Status == model.StatusRejected {
                s.logManualRejection(ctx, id, rejectionReason)
        }

        // PR #95: persist discount_amount on the application (if applicable).
        // This is a separate UPDATE so we don't break the existing
        // UpdateApplicationDecision signature (which other callers depend on).
        if req.Status == model.StatusApproved && discountAmount > 0 {
                if err := s.repo.UpdateApplicationDiscount(ctx, id, app.DiscountCode, &discountAmount); err != nil {
                        slog.Warn("failed to persist discount_amount (non-fatal)",
                                "application_id", id,
                                "error", err)
                }
        }

        // Save credit level history for manual approvals (same as auto-approve for Elite)
        if req.Status == model.StatusApproved {
                if histErr := s.repo.SaveCreditLevelHistory(ctx, app.CustomerPIN, creditLevel, id); histErr != nil {
                        slog.Warn("failed to save credit level history",
                                "application_id", id,
                                "customer_pin", app.CustomerPIN,
                                "credit_level", creditLevel,
                                "error", histErr)
                }

                // PR #95: mark the discount code as 'used' (best-effort — log on failure).
                // This is NOT inside the decision transaction because the decision is
                // already committed. If this fails, the code is still 'active' and the
                // next customer could potentially redeem it — but the discount was
                // already applied to this application, so the financial impact is
                // limited to a possible double-redemption (extremely rare).
                if app.DiscountCode != "" && s.discountSvc != nil {
                        s.markDiscountCodeUsed(ctx, app.DiscountCode, id)
                }

                // PR #95: generate a new discount code for the approved customer and
                // send an SMS with the code. The customer can share this code with
                // others to give them a commission discount on their next loan.
                s.generateAndSendDiscountSMS(ctx, id, app.CustomerPIN, app.CustomerPhone)

                // PR #119: AZMK Application create + Sign check + Disburse
                // Ekspert approve etdikdə AZMK-da kredit müraciəti yaradılır,
                // imzalanma yoxlanılır və pul köçürülür.
                if s.azmkProvider != nil && app.PartnerID != "" {
                        if err := s.azmkCreateSignDisburse(ctx, app, totalAmount); err != nil {
                                slog.Error("PR #283: AZMK approve flow failed — rolling back approval",
                                        "application_id", id,
                                        "error", err)
                                rejectReason := fmt.Sprintf("AZMK approve flow failed: %v", err)
                                if err := s.repo.UpdateApplicationDecision(ctx, id,
                                        model.StatusRejected, creditLevel, rejectReason, app.Amount, app.ApprovedRate, totalAmount); err != nil {
                                        slog.Error("PR #283: failed to rollback approval after AZMK failure",
                                                "application_id", id,
                                                "error", err)
                                }
                                return nil, fmt.Errorf("AZMK approve flow uğursuz: %w", err)
                        }
                }
        }

        // Return the updated application
        return s.repo.GetApplicationByID(ctx, id)
}

// validateAndComputeDiscount re-validates the discount code on the application
// (race-condition protection) and computes the discount amount to apply.
//
// PR #109: endirim artıq FAİZDƏN (interestAmount) çıxılır, komissiyadan yox.
// annual_interest_rate credit_levels cədvəlindən alınır (level + amount + term + phase əsasında).
//
// Returns the discount amount (>= 0). Returns an error if the code is no
// longer valid (e.g. already used by another customer between customer-confirm
// and approval).
func (s *ApplicationService) validateAndComputeDiscount(ctx context.Context, app *model.LoanApplication) (float64, error) {
        customer, err := s.customerRepo.GetByPIN(ctx, app.CustomerPIN)
        if err != nil {
                slog.Error("approval: failed to fetch customer for discount re-validation",
                        "application_id", app.ID,
                        "customer_pin", app.CustomerPIN,
                        "error", err)
                return 0, fmt.Errorf("texniki xəta — müştəri məlumatları əldə edilə bilmədi")
        }

        dc, err := s.discountSvc.ValidateForCustomer(ctx, app.DiscountCode, customer.ID)
        if err != nil {
                slog.Info("approval: discount code no longer valid (race condition or expired)",
                        "application_id", app.ID,
                        "discount_code", app.DiscountCode,
                        "error", err)
                // Clear the discount code on the application so the customer can see
                // in the dashboard that the code was not applied.
                _ = s.repo.UpdateApplicationDiscount(ctx, app.ID, "", nil)
                return 0, fmt.Errorf("endirim kodu artıq keçərli deyil: %w", err)
        }

        // PR #109: faiz məbləğini hesabla (komissiya yox).
        // 1. credit_levels cədvəlindən annual_interest_rate al
        // 2. interestAmount = principal × annualRate × (term / 12)
        // 3. discount = CalculateDiscount(dc, interestAmount)

        // unlock_phase təyin et (CountApprovedAtLevel ilə)
        approvedCount, err := s.repo.CountApprovedAtLevel(ctx, app.CustomerPIN, app.CreditLevel)
        if err != nil {
                slog.Warn("approval: failed to count approved at level — assuming phase 1",
                        "application_id", app.ID,
                        "error", err)
                approvedCount = 0
        }
        unlockPhase := 1
        if approvedCount > 0 {
                unlockPhase = 2
        }

        annualInterestRate, err := s.repo.GetCreditLevelInterestRate(ctx, app.CreditLevel, app.Amount, app.TermMonths, unlockPhase)
        if err != nil {
                slog.Warn("approval: failed to fetch annual_interest_rate — discount skipped",
                        "application_id", app.ID,
                        "credit_level", app.CreditLevel,
                        "error", err)
                // Faiz tapılmadısa discount tətbiq etmə — amma approval uğurlu sayılır
                return 0, nil
        }

        interestAmount := calculateInterestAmount(app.Amount, annualInterestRate, app.TermMonths)
        discount := s.discountSvc.CalculateDiscount(dc, interestAmount)

        slog.Info("approval: discount applied (from interest)",
                "application_id", app.ID,
                "discount_code", app.DiscountCode,
                "discount_type", dc.DiscountType,
                "discount_value", dc.DiscountValue,
                "credit_level", app.CreditLevel,
                "annual_interest_rate", annualInterestRate,
                "interest_amount", interestAmount,
                "discount_amount", discount)

        return discount, nil
}

// markDiscountCodeUsed marks the discount code as 'used' by this application.
// Best-effort: failures are logged but do not fail the approval.
func (s *ApplicationService) markDiscountCodeUsed(ctx context.Context, code string, appID int) {
        dc, err := s.discountSvc.repo.GetByCode(ctx, code)
        if err != nil {
                slog.Warn("approval: failed to fetch discount code for mark-used",
                        "application_id", appID,
                        "discount_code", code,
                        "error", err)
                return
        }
        if err := s.discountSvc.repo.MarkUsed(ctx, dc.ID, appID); err != nil {
                slog.Warn("approval: failed to mark discount code as used (may cause double-redemption)",
                        "application_id", appID,
                        "discount_code", code,
                        "code_id", dc.ID,
                        "error", err)
                return
        }
        slog.Info("approval: discount code marked as used",
                "application_id", appID,
                "discount_code", code,
                "code_id", dc.ID)
}

// generateAndSendDiscountSMS generates a new discount code for the approved
// customer and sends an SMS with the code. Best-effort: failures are logged
// but do not fail the approval.
//
// The SMS informs the customer that their loan was approved and gives them a
// code they can share with others (who will get a commission discount on
// their next loan).
func (s *ApplicationService) generateAndSendDiscountSMS(ctx context.Context, appID int, customerPIN, customerPhone string) {
        if s.discountSvc == nil {
                return
        }

        // Look up the customer to get their ID (needed for code generation).
        customer, err := s.customerRepo.GetByPIN(ctx, customerPIN)
        if err != nil {
                slog.Error("approval: failed to fetch customer for discount code generation",
                        "application_id", appID,
                        "customer_pin", customerPIN,
                        "error", err)
                return
        }

        // Generate a new discount code owned by this customer.
        newCode, err := s.discountSvc.GenerateForApplication(ctx, appID, customer.ID)
        if err != nil {
                slog.Error("approval: failed to generate discount code (non-fatal)",
                        "application_id", appID,
                        "customer_pin", customerPIN,
                        "error", err)
                return
        }

        // PR #284: kod approve zamanı generasiya olunur, amma SMS burada göndərilmir —
        // referal SMS-i disburse success-də göndərilir (sendReferralSMSOnDisburse).
        slog.Info("approval: discount code generated (referral SMS will be sent on disburse success, PR #284)",
                "application_id", appID,
                "customer_pin", customerPIN,
                "discount_code", newCode.Code)
}

// sendReferralSMSOnDisburse sends the referral SMS after a successful disburse (PR #284).
// Text: "Endirim kodunu dostunla paylaş,dostun X% endirimlə kredit əldə etsin,sən də
// növbəti kreditində X% endirim qazan! Kod: 123456(referal kodu)"
// X% — REFERRAL_DISCOUNT_PERCENT konfiqurasiyasından (referralDiscountPercent).
// Kod — approve zamanı generasiya olunmuş referal kodu (GetByApplicationID).
// Bütün xətalar non-fatal — disburse uğuru təsirlənmir.
func (s *ApplicationService) sendReferralSMSOnDisburse(ctx context.Context, app *model.LoanApplication) {
        if s.smsProvider == nil {
                slog.Warn("disburse: smsProvider nil — referral SMS göndərilə bilmədi",
                        "application_id", app.ID)
                return
        }
        if app.CustomerPhone == "" {
                slog.Warn("disburse: customer_phone empty — referral SMS göndərilə bilmədi",
                        "application_id", app.ID)
                return
        }
        if s.discountSvc == nil {
                slog.Warn("disburse: discountSvc nil — referral SMS göndərilə bilmədi",
                        "application_id", app.ID)
                return
        }

        // Approve zamanı generasiya olunmuş kodu tap
        code, err := s.discountSvc.repo.GetByApplicationID(ctx, app.ID)
        if err != nil {
                slog.Error("disburse: failed to fetch referral code for application (non-fatal)",
                        "application_id", app.ID,
                        "error", err)
                return
        }

        percent := s.referralDiscountPercent
        if percent <= 0 {
                percent = 5 // default
        }

        smsMsg := fmt.Sprintf("Endirim kodunu dostunla paylaş,dostun %d%% endirimlə kredit əldə etsin,sən də növbəti kreditində %d%% endirim qazan! Kod: %s(referal kodu)",
                percent, percent, code.Code)
        if err := s.smsProvider.Send(ctx, app.CustomerPhone, smsMsg); err != nil {
                slog.Error("disburse: failed to send referral SMS (non-fatal)",
                        "application_id", app.ID,
                        "phone", app.CustomerPhone,
                        "discount_code", code.Code,
                        "error", err)
                return
        }

        slog.Info("disburse: referral SMS sent",
                "application_id", app.ID,
                "phone", app.CustomerPhone,
                "discount_code", code.Code,
                "discount_percent", percent)
}

// azmkCreateSignDisburse performs the AZMK Online Lending approve flow:
//   1. Application create (POST /application/create) → lw_application_id
//   2. Sign check (GET /application/{id}/sign) — "already signed" yoxla
//   3. Disburse (POST /application/disburse) — pulu karta köçür
//
// PR #119: ekspert approve zamanı çağrılır. Bütün addımlar best-effort:
// xəta olanda approval uğurlu sayılır, AZMK xətası log lanır.
//
// LoanData sahələri:
//   - clientId: partner_id (KYC/Partner registration-dan)
//   - productId: config-dən (AZMK_PRODUCT_ID)
//   - amount: total_amount (principal + commission)
//   - term: app.TermMonths
//   - branchCode: config-dən (AZMK_BRANCH_CODE)
//   - interestRate: annual_interest_rate (credit_levels-dən) — AZMK KƏSR formatında göndərilir: 48 → 0.48, 30 → 0.30 (PR #311)
//   - disbursementFee: config-dən (AZMK_DISBURSEMENT_FEE, həmişə 0)
func (s *ApplicationService) azmkCreateSignDisburse(ctx context.Context, app *model.LoanApplication, totalAmount float64) error {
        // annual_interest_rate-i credit_levels-dən al
        approvedCount, _ := s.repo.CountApprovedAtLevel(ctx, app.CustomerPIN, app.CreditLevel)
        unlockPhase := 1
        if approvedCount > 0 {
                unlockPhase = 2
        }
        annualInterestRate, err := s.repo.GetCreditLevelInterestRate(ctx, app.CreditLevel, app.Amount, app.TermMonths, unlockPhase)
        if err != nil {
                slog.Warn("AZMK: failed to fetch annual_interest_rate — using 0",
                        "application_id", app.ID,
                        "error", err)
                annualInterestRate = 0
        }

        // 1. AZMK Application create
        appReq := &azmk.ApplicationCreateRequest{
                LoanData: azmk.LoanData{
                        ClientID:        app.PartnerID,
                        ProductID:       s.azmkProductID,
                        Amount:          totalAmount,
                        Term:            app.TermMonths,
                        BranchCode:      s.azmkBranch,
                        InterestRate:    annualInterestRate / 100.0, // PR #311: AZMK kəsr gözləyir (48 → 0.48)
                        DisbursementFee: s.azmkDisbursementFee,
                        LetterNumber:    "",
                },
        }

        lwAppID, err := s.azmkProvider.CreateApplication(ctx, appReq)
        if err != nil {
                return fmt.Errorf("AZMK application create failed: %w", err)
        }

        // lw_application_id saxla
        app.LwApplicationID = lwAppID
        if err := s.repo.UpdateApplicationDetails(ctx, app.ID, app); err != nil {
                slog.Warn("AZMK: failed to save lw_application_id",
                        "application_id", app.ID,
                        "lw_application_id", lwAppID,
                        "error", err)
        }

        // 2. Sign check — "already signed" yoxla
        // Bu addım asinxron ola bilər (müştəri SIMA ilə imzalayana qədər gözləmək)
        // Hazırda dərhal yoxlayırıq — əgər imzalanmayıbsa, disburse etmirik
        signed, err := s.azmkProvider.CheckSign(ctx, lwAppID)
        if err != nil {
                slog.Warn("AZMK: sign check failed — skipping disburse",
                        "application_id", app.ID,
                        "lw_application_id", lwAppID,
                        "error", err)
                return nil // sign check xətası — disburse etmə
        }
        if !signed {
                slog.Info("AZMK: contract not yet signed — disburse will be triggered later",
                        "application_id", app.ID,
                        "lw_application_id", lwAppID)
                return nil // hələ imzalanmayıb — disburse etmə
        }

        // 3. Disburse — pulu karta köçür
        disburseReq := &azmk.DisburseRequest{
                LoanData: azmk.LoanData{
                        ApplicationID: lwAppID,
                        CardID:        app.CardID,
                },
        }

        if err := s.azmkProvider.Disburse(ctx, disburseReq); err != nil {
                slog.Error("AZMK: disburse failed",
                        "application_id", app.ID,
                        "lw_application_id", lwAppID,
                        "card_id", app.CardID,
                        "error", err)
                return fmt.Errorf("AZMK disburse failed: %w", err)
        }

        slog.Info("PR #281: step 7-9 completed — AZMK full approve flow (create + sign + disburse)",
                "application_id", app.ID,
                "lw_application_id", lwAppID,
                "card_id", app.CardID,
                "amount", totalAmount,
                "term", app.TermMonths,
                "interest_rate", annualInterestRate,
                "interest_rate_sent", annualInterestRate/100.0)

        // PR #284: disburse success — müştəriyə referal kodu SMS-i göndər (non-fatal)
        s.sendReferralSMSOnDisburse(ctx, app)

        return nil
}


// logManualRejection writes a manual rejection to cutoff_results.
// PR #258: ekspert tərəfindən MANUAL_* səbəbi ilə imtina ediləndə
// cutoff_results cədvəlinə yazılır ki:
//   - plan/fakt hesabatlarında görünsün
//   - checkLastRejectionCutoff validity_days bloku işləsin (PR #256)
//
// rejectionReason format: "MANUAL_VIDEO_MISMATCH" və ya "MANUAL_VIDEO_MISMATCH:əlavə mətn"
// cutoff_code üçün prefix ayrılır, details üçün tam mətn saxlanılır.
func (s *ApplicationService) logManualRejection(ctx context.Context, appID int, rejectionReason string) {
	if s.cutoffRepo == nil {
		return
	}

	// cutoff_code = rejection_reason-ün ':'-dən əvvəlki hissəsi
	code := rejectionReason
	details := ""
	if idx := strings.Index(rejectionReason, ":"); idx > 0 {
		code = rejectionReason[:idx]
		details = rejectionReason[idx+1:]
	}

	// Yalnız MANUAL_* kodları üçün yaz (digər rejeksiyalar auto-cutoff tərəfindən
	// artıq yazılıb — məs: AKB_SCORE_LOW, EMPLOYMENT_TENURE və s.)
	if !strings.HasPrefix(code, "MANUAL_") {
		return
	}

	cr := &model.CutoffResult{
		ApplicationID: appID,
		CutoffCode:    code,
		CutoffName:   "Manual imtina",
		ServiceName:  "EXPERT_MANUAL",
		Checked:      true,
		Passed:       false,
		ActualValue:  rejectionReason,
		Threshold:    "manual reject",
		Details:      details,
	}
	if err := s.cutoffRepo.Insert(ctx, cr); err != nil {
		slog.Warn("failed to log manual rejection cutoff result",
			"application_id", appID,
			"rejection_reason", rejectionReason,
			"error", err)
	} else {
		slog.Info("manual rejection logged to cutoff_results",
			"application_id", appID,
			"cutoff_code", code)
	}
}
