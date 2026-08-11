package service

import (
        "context"
        "fmt"
        "log/slog"
        "strings"
        "time"

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
func (s *ApplicationService) InitApplication(ctx context.Context, req *InitApplicationRequest) (*model.LoanApplication, error) {
        if req.CustomerPIN == "" {
                return nil, fmt.Errorf("customer_pin is required")
        }
        if req.CustomerPhone == "" {
                return nil, fmt.Errorf("customer_phone is required")
        }

        // PR #70: Check for duplicate — customer must not have an existing non-final application.
        // This blocks new applications while a previous one is still being processed.
        existingID, existingStatus, err := s.repo.HasPendingApplication(ctx, req.CustomerPIN)
        if err != nil {
                return nil, fmt.Errorf("failed to check existing applications: %w", err)
        }
        if existingID > 0 {
                return nil, fmt.Errorf("Sizin artıq işlənməkdə olan müraciətiniz var (№%d, status: %s). Bu müraciət həll olunana qədər yeni müraciət edə bilməzsiniz", existingID, existingStatus)
        }

        app := &model.LoanApplication{
                CustomerPIN:    req.CustomerPIN,
                CustomerSerial: req.CustomerSerial,
                CustomerPhone:  req.CustomerPhone,
                Status:         model.StatusPendingCustomer,
        }

        if err := s.repo.CreateApplication(ctx, app); err != nil {
                return nil, fmt.Errorf("failed to create application: %w", err)
        }

        // Send OTP
        otpResp, err := s.otpService.SendOTP(ctx, req.CustomerPhone)
        if err != nil {
                return nil, fmt.Errorf("failed to send OTP: %w", err)
        }
        if !otpResp.Sent {
                return nil, fmt.Errorf("OTP could not be sent (rate limited). Retry after %d seconds", otpResp.RetryAfterS)
        }

        slog.Info("application initialized, OTP sent",
                "application_id", app.ID,
                "customer_pin", req.CustomerPIN,
                "phone", req.CustomerPhone)

        return app, nil
}

// VerifyInitApplicationRequest is the body for POST /api/applications/init/verify.
type VerifyInitApplicationRequest struct {
        ApplicationID int    `json:"application_id"`
        Phone         string `json:"phone"`
        OTPCode       string `json:"otp_code"`
}

// VerifyInitApplication verifies the OTP code and transitions the application
// from pending_customer to pending_expert.
//
// PR #117: AZMK KYC və Partner registration OTP-dən SONRA, cutoff-dan ƏVVƏL baş verir.
// PR #112: AUTO cutoff yoxlamaları KYC-dən sonra, kredit təklifindən ƏVVƏL baş verir.
//
// Flow:
//   1. OTP verify
//   2. AZMK KYC (create session → verify → get kyc_id)
//   3. AZMK Partner registration (get partner_id)
//   4. AUTO cutoff checks (AKB, blacklist, age, delay, active loan, etc.)
//   5. If all pass → pending_expert
func (s *ApplicationService) VerifyInitApplication(ctx context.Context, req *VerifyInitApplicationRequest) (*model.LoanApplication, error) {
        if req.ApplicationID <= 0 {
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
                return nil, fmt.Errorf("invalid OTP code, %d attempts remaining", verifyResp.Attempts)
        }

        // 2. Fetch application
        app, err := s.repo.GetApplicationByID(ctx, req.ApplicationID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }
        if app.Status != model.StatusPendingCustomer {
                return nil, fmt.Errorf("application is not in pending_customer status (current: %s)", app.Status)
        }

        // 3. PR #117: AZMK KYC + Partner registration
        // Müştəri kimliyini təsdiq etmədən cutoff yoxlamaq mənasızdır.
        if s.azmkProvider != nil {
                kycErr := s.runAzmkKycAndPartner(ctx, app)
                if kycErr != nil {
                        // KYC rədd olundu — müştəriyə xəbər ver
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
                        return app, nil
                }
        }

        // 4. PR #112: Early AUTO cutoff yoxlamaları
        // Müştəri kredit təklifi görməzdən əvvəl yoxlanılır.
        rejectionReason, err := s.runEarlyCutoffChecks(ctx, app)
        if err != nil {
                slog.Error("early cutoff checks failed — proceeding to pending_expert (fail-soft)",
                        "application_id", app.ID,
                        "customer_pin", app.CustomerPIN,
                        "error", err)
                // Fail-soft: cutoff xətası olanda müştərini bloklamırıq — normal flow davam edir
        } else if rejectionReason != "" {
                // Cutoff rədd etdi — statusu rejected et və səbəbi yaz
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
                return app, nil
        }

        // 5. Cutoff keçdi — transition to pending_expert
        app.Status = model.StatusPendingExpert
        if err := s.repo.UpdateApplicationStatus(ctx, app.ID, app.Status); err != nil {
                return nil, fmt.Errorf("failed to update status: %w", err)
        }

        slog.Info("application verified, KYC passed, early cutoff passed, waiting for expert",
                "application_id", app.ID,
                "customer_pin", app.CustomerPIN,
                "kyc_id", app.KycID,
                "partner_id", app.PartnerID)

        return app, nil
}

// runAzmkKycAndPartner performs AZMK KYC verification and Partner registration.
// PR #117: OTP-dən sonra, cutoff-dan əvvəl çağrılır.
//
// Steps:
//   1. Create KYC session (POST /kyc) → get kyc_id
//   2. Verify KYC (GET /kyc/{id}) → must be VERIFIED
//   3. Register Partner (POST /partner with kycId) → get partner_id
//   4. Save kyc_id + partner_id to the application
//
// Returns error if KYC fails or is not verified.
func (s *ApplicationService) runAzmkKycAndPartner(ctx context.Context, app *model.LoanApplication) error {
        // Build PartnerData from application info
        phone := app.CustomerPhone
        // AZMK expects phone without +994 prefix (məs. "513153393")
        phone = strings.TrimPrefix(phone, "+994")

        pd := azmk.PartnerData{
                AsanFinanceEmployeeInfo: false,
                AsanFinancePersonalInfo: false,
                FirstName:               "-", // müşteri info henüz yoxdur
                LastName:                "-",
                Mkr:                     false,
                Mobile:                  phone,
                Pin:                     app.CustomerPIN,
                BranchCode:              s.azmkBranch,
                Passport:                app.CustomerSerial,
                HomeAddress:             "-",
        }

        // 1. Create KYC session
        kycReq := &azmk.KYCRequest{PartnerData: pd}
        kycID, err := s.azmkProvider.KYC(ctx, kycReq)
        if err != nil {
                slog.Error("AZMK KYC creation failed",
                        "application_id", app.ID,
                        "customer_pin", app.CustomerPIN,
                        "error", err)
                return fmt.Errorf("KYC yaradıla bilmədi: %w", err)
        }
        slog.Info("AZMK KYC session created",
                "application_id", app.ID,
                "kyc_id", kycID)

        // 2. Verify KYC — PR #155: 3 dəqiqə polling (hər 3 san., maksimum 60 cəhd)
        // AZMK status: SENT → VERIFIED (və ya Invalidid)
        // SENT olanda polling edirik — müştəri verify edənə qədər gözləyirik.
        // 3 dəqiqə (180 san) / 3 san = 60 cəhd
        const maxKYCAttempts = 60
        const kycPollInterval = 3 * time.Second
        var verified bool
        for attempt := 1; attempt <= maxKYCAttempts; attempt++ {
                verified, err = s.azmkProvider.VerifyKYC(ctx, kycID)
                if err != nil {
                        slog.Error("AZMK KYC verify failed — invalid ID",
                                "application_id", app.ID,
                                "kyc_id", kycID,
                                "attempt", attempt,
                                "error", err)
                        return fmt.Errorf("KYC yoxlanıla bilmədi: %w", err)
                }
                if verified {
                        slog.Info("AZMK KYC verified",
                                "application_id", app.ID,
                                "kyc_id", kycID,
                                "attempt", attempt)
                        break
                }
                slog.Info("AZMK KYC not verified yet, polling...",
                        "application_id", app.ID,
                        "kyc_id", kycID,
                        "attempt", attempt,
                        "max_attempts", maxKYCAttempts)
                if attempt < maxKYCAttempts {
                        time.Sleep(kycPollInterval)
                }
        }
        if !verified {
                slog.Info("AZMK KYC not verified after 3 minutes",
                        "application_id", app.ID,
                        "kyc_id", kycID,
                        "attempts", maxKYCAttempts)
                return fmt.Errorf("KYC təsdiq olunmadı — 3 dəqiqə ərzində verify olunmadı")
        }

        // 3. Register Partner (with kycId)
        pd.KycID = kycID
        partnerReq := &azmk.PartnerRequest{PartnerData: pd}
        partnerID, err := s.azmkProvider.RegisterPartner(ctx, partnerReq)
        if err != nil {
                slog.Error("AZMK Partner registration failed",
                        "application_id", app.ID,
                        "kyc_id", kycID,
                        "error", err)
                return fmt.Errorf("Partner qeydiyyatı uğursuz: %w", err)
        }
        slog.Info("AZMK Partner registered",
                "application_id", app.ID,
                "kyc_id", kycID,
                "partner_id", partnerID)

        // 4. Save kyc_id + partner_id to application
        app.KycID = kycID
        app.PartnerID = partnerID
        if err := s.repo.UpdateApplicationDetails(ctx, app.ID, app); err != nil {
                slog.Error("AZMK: failed to save kyc_id/partner_id",
                        "application_id", app.ID,
                        "error", err)
                // Non-fatal: IDs are in memory, will be saved on next UpdateApplicationDetails call
        }

        return nil
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

        // 1. Qara siyahı və aktiv kredit yoxlaması
        // PR #159/#160: yalnız AZMK getOwnerData istifadə olunur.
        // LW GetAzmkBlacklist və LW CheckBlacklist artıq istifadə olunmur.
        if s.customerDataProvider != nil {
                ownerData, err := s.customerDataProvider.GetOwnerData(ctx, customerPIN, serial)
                if err != nil {
                        slog.Warn("early cutoff: AZMK getOwnerData failed — fail-soft (skip)", "error", err)
                } else if ownerData != nil {
                        if ownerData.CustomerCheck.BlacklistStatus {
                                slog.Info("early cutoff: AZMK blacklist triggered",
                                        "application_id", app.ID,
                                        "customer_pin", customerPIN)
                                return "AZMK_BLACKLIST", nil
                        }
                        if ownerData.CustomerCheck.HasActiveCredit {
                                slog.Info("early cutoff: AZMK active credit detected",
                                        "application_id", app.ID,
                                        "customer_pin", customerPIN)
                                return "ACTIVE_CREDIT", nil
                        }
                        slog.Info("AZMK getOwnerData — customer is clean",
                                "application_id", app.ID,
                                "customer_pin", customerPIN,
                                "is_existing_customer", ownerData.CustomerCheck.IsExistingCustomer,
                                "total_delay_days", ownerData.CustomerCheck.TotalDelayDaysCumulative)
                }
        } else {
                // Backward compatible: yalnız LW CheckBlacklist (GetAzmkBlacklist silindi)
                blacklisted, err := s.creditEngine.lwProvider.CheckBlacklist(ctx, customerPIN)
                if err != nil {
                        slog.Warn("early cutoff: LW blacklist check failed — fail-soft (skip)", "error", err)
                } else if blacklisted {
                        return "LW_BLACKLIST", nil
                }
        }

        // 2. AKB skoru və stop-faktor
        // PR #160: əgər AZMK CustomerDataProvider varsa, getMkrScore istifadə et.
        // Əks halda LW provider-dən (köhnə metod) istifadə et — backward compatible.
        if s.customerDataProvider != nil {
                mkrScore, err := s.customerDataProvider.GetMkrScore(ctx, customerPIN, serial)
                if err != nil {
                        slog.Warn("early cutoff: AZMK getMkrScore failed — fail-soft (skip)", "error", err)
                } else if mkrScore != nil {
                        // PR #160: point < 200 → AKB_SCORE_LOW
                        if mkrScore.Score.Point > 0 && mkrScore.Score.Point < 200 {
                                slog.Info("early cutoff: AKB score low",
                                        "application_id", app.ID,
                                        "customer_pin", customerPIN,
                                        "point", mkrScore.Score.Point)
                                return "AKB_SCORE_LOW", nil
                        }
                        // PR #160: response = "D" və ya "E" → stop-faktor
                        resp := strings.ToUpper(mkrScore.Score.Response)
                        if resp == "D" || resp == "E" {
                                slog.Info("early cutoff: AKB stop factor triggered",
                                        "application_id", app.ID,
                                        "customer_pin", customerPIN,
                                        "response", mkrScore.Score.Response,
                                        "point", mkrScore.Score.Point)
                                return fmt.Sprintf("AKB_STOP_FACTOR:%s", mkrScore.Score.Response), nil
                        }
                        slog.Info("AZMK getMkrScore — score passed",
                                "application_id", app.ID,
                                "customer_pin", customerPIN,
                                "point", mkrScore.Score.Point,
                                "response", mkrScore.Score.Response,
                                "pd_rate", mkrScore.Score.PdRate)
                }
        } else {
                // Backward compatible: LW provider
                akbScore, stopFactorCode, hasStopFactor := s.creditEngine.resolveAkbScoreAndStopFactors(ctx, customerPIN, 0)
                if hasStopFactor {
                        return fmt.Sprintf("AKB_STOP_FACTOR:%s", stopFactorCode), nil
                }
                if akbScore > 0 && akbScore < 200 {
                        return "AKB_SCORE_LOW", nil
                }
        }

        // 3. Yaş yoxlaması
        // PR #152: əgər AZMK CustomerDataProvider varsa, ondan istifadə et (daha dəqiq).
        // Əks halda LW provider-dən (köhnə metod) istifadə et — backward compatible.
        age := 0
        if s.customerDataProvider != nil {
                age = s.resolveCustomerAgeFromAzmk(ctx, customerPIN, serial)
        } else {
                age = s.creditEngine.resolveCustomerAge(ctx, customerPIN, serial)
        }
        if age > 69 {
                return "AGE_OVER_69", nil
        }

        // 4. AKB History yoxlamaları (gecikmə, aktiv kredit, aylıq ödəniş)
        // Bu yoxlamalar loanAnalytics strukturunu doldurur
        var analytics loanAnalytics
        s.creditEngine.resolveAkbHistory(ctx, customerPIN, serial, &analytics)

        // 4a. Gecikmə tarixçəsi (yalnız AKB history available olanda)
        if analytics.akbHistoryAvailable {
                if analytics.delayRatio > 6 {
                        return "DELAY_RATIO_HIGH", nil
                }
                if analytics.activeMaxDelayDays > 5 {
                        return "ACTIVE_DELAY_HIGH", nil
                }
                if analytics.maxDelayLast3Months >= 20 {
                        return "DELAY_3M", nil
                }
                if analytics.maxDelayLast6Months >= 30 {
                        return "DELAY_6M", nil
                }
                if analytics.maxDelayLast12Months >= 45 {
                        return "DELAY_12M", nil
                }
                if analytics.maxDelayLast18Months >= 60 {
                        return "DELAY_18M", nil
                }
                if analytics.totalMonthlyPayments > 2000 {
                        return "MONTHLY_PAYMENTS_HIGH", nil
                }
        }

        // 5. Aktiv kredit və gecikməli ödəniş (LW loans)
        customerLoans, loansErr := s.creditEngine.lwProvider.GetCustomerLoans(ctx, customerPIN)
        if loansErr != nil {
                slog.Warn("early cutoff: failed to fetch customer loans — fail-soft (skip)",
                        "customer_pin", customerPIN,
                        "error", loansErr)
        } else {
                loanAnalytics := computeAnalytics(customerLoans.Loans)
                if loanAnalytics.hasActive {
                        return "ACTIVE_LOAN", nil
                }
                if loanAnalytics.completedCount > 0 && !loanAnalytics.allOnTime {
                        return "LATE_PAYMENT", nil
                }
        }

        slog.Info("early cutoff: all checks passed",
                "application_id", app.ID,
                "customer_pin", customerPIN,
                "age", age,
                "akb_history_available", analytics.akbHistoryAvailable,
                "delay_ratio", analytics.delayRatio,
                "active_delay", analytics.activeMaxDelayDays,
                "max_3m", analytics.maxDelayLast3Months,
                "max_6m", analytics.maxDelayLast6Months,
                "max_12m", analytics.maxDelayLast12Months,
                "max_18m", analytics.maxDelayLast18Months,
                "monthly_payments", analytics.totalMonthlyPayments)

        return "", nil
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
        Contact1Relation string `json:"contact1_relation"` // PR #85: Ata, Ana, Qardaş, etc.
        Contact2Relation string `json:"contact2_relation"`
        Contact3Relation string `json:"contact3_relation"`
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
