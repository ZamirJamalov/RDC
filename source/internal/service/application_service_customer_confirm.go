package service

import (
        "context"
        "fmt"
        "log/slog"
        "strings"
        "time"

        "rdc-source/internal/model"
        "rdc-source/pkg/azmk"
)

// CustomerConfirmRequest is the body for POST /api/applications/{id}/customer-confirm (PR #58).
// This is the form the customer fills in on the public website after their OTP has been
// verified and they've seen their credit offer:
//
//   - select an amount from the offered range
//   - enter their 16-digit card number
//   - tick the "this card belongs to me" checkbox
//   - enter their actual residential address
//
// Backend then fills in the remaining fields from external services:
//
//   - customer_full_name  ← AZMK GetPersonalInfo (fail-soft, PR #227)
//   - akb_score           ← AZMK getMkrScore     (fail-soft, PR #227)
//   - term_months         ← GetOffer ranges, matched to the selected amount
//   - contact1/2/3_phone  ← expert fills these later via CompleteApplication
type CustomerConfirmRequest struct {
        Amount                  float64 `json:"amount"`
        CardNumber              string  `json:"card_number"`
        ActualAddress           string  `json:"actual_address"`
        CardOwnershipConfirmed  bool    `json:"card_ownership_confirmed"`
        // PR #149: CustomerPhone for IDOR check — must match the application's customer_phone.
        CustomerPhone           string  `json:"customer_phone"`
        // PR #95: optional discount/referral code entered by the customer.
        // Validated against the discount_codes table (must exist, belong to a
        // different customer, and be in 'active' status). If valid, the code is
        // stored on the application; the actual discount is applied at approval.
        DiscountCode            string  `json:"discount_code,omitempty"`
}

// CustomerConfirmApplication finalizes the customer-side of the application flow.
//
// Pipeline:
//  1. Validate the request (amount > 0, 16-digit card, address non-empty, checkbox ticked)
//  2. Fetch the application — must be in pending_customer status
//  3. Fetch PersonalInfo from AZMK → customer_full_name (fail-soft, PR #227)
//  4. Resolve AKB score from AZMK getMkrScore (fail-soft, PR #227)
//  5. Get credit offer → find the range matching the customer's amount → term_months
//  6. Save amount, term_months, card, address, full_name, akb_score,
//     customer_confirmed_at = now(), card_ownership_confirmed = true
//  7. Application transitions to pending_expert — visible in the RDC dashboard;
//     the expert later approves/rejects it.
//
// PR #227: LW router deaktiv edildi — AZMK birincil mənbədir, fail-soft.
//   - PersonalInfo/AKB xətaları müşterini bloklamır (warn + davam edir)
//   - Əsas cutoff yoxlamaları OTP verify mərhələsində (early cutoffs) artıq edilib,
//     customer-confirm-də yalnız stop-faktor müdafiə yoxlaması qalır
func (s *ApplicationService) CustomerConfirmApplication(ctx context.Context, appID int, req *CustomerConfirmRequest) (*model.LoanApplication, error) {
        if appID <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }
        if req.Amount <= 0 {
                return nil, fmt.Errorf("amount must be greater than zero")
        }
        if len(req.CardNumber) != 16 {
                return nil, fmt.Errorf("card_number must be exactly 16 digits")
        }
        if req.ActualAddress == "" {
                return nil, fmt.Errorf("actual_address is required")
        }
        if !req.CardOwnershipConfirmed {
                return nil, fmt.Errorf("card ownership must be confirmed (tick the checkbox)")
        }

        // PR #188: Video record check — əgər aktivdirsə, video tamamlanmalıdır
        if s.IsVideoRecordEnabled() {
                recorded, err := s.IsVideoRecorded(ctx, appID)
                if err != nil {
                        slog.Warn("customer-confirm: video record check failed — fail-soft (allowing)", "error", err)
                } else if !recorded {
                        return nil, fmt.Errorf("video identifikasiya tələb olunur — zəhmət olmasa əvvəlcə video qeydiyyatını tamamlayın")
                }
        }

        // 1. Fetch the application — must be pending_customer (OTP verified, cutoff passed, but not yet confirmed)
        // PR #221: OTP verify artıq pending_customer saxlayır (pending_expert-ə keçmir).
        // Customer-confirm-də pending_expert-ə keçir (RDC dashboard-a göndərilir).
        app, err := s.repo.GetApplicationByID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }
        if app.Status != model.StatusPendingCustomer {
                return nil, fmt.Errorf("müraciət təsdiq oluna bilməz (cari status: %s) — yalnız OTP təsdiq olunmuş müraciətlər təsdiqlənə bilər", app.Status)
        }

        // 2. PR #227: Fetch PersonalInfo from AZMK CustomerDataService — fail-soft.
        // LW router yanaşması deaktiv edildi; AZMK birincil mənbədir.
        // Xəta olsa: warn + davam (ad boş qalır, müraciət expert-ə keçir).
        if s.customerDataProvider != nil {
                data, err := s.customerDataProvider.GetPersonalInfo(ctx, app.CustomerPIN, app.CustomerSerial)
                if err != nil {
                        slog.Warn("customer-confirm: AZMK GetPersonalInfo failed — fail-soft (name left empty)",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "error", err)
                } else if data != nil {
                        if fullName := data.FullName(); fullName != "" {
                                app.CustomerFullName = fullName
                        }
                }
        }

        // 3. PR #227: AKB skoru AZMK getMkrScore-dan — fail-soft.
        // Skor alınmasa akb=0 ilə davam edilir; GetOffer öz fallback-ini işlədir.
        // Stop-faktor yalnız məlumat gələndə yoxlanılır (müdafiə məqsədilə —
        // əsas yoxlama OTP verify mərhələsindəki early cutoff-lardadır).
        resolvedAkb := 0
        if s.customerDataProvider != nil {
                mkrScore, err := s.customerDataProvider.GetMkrScore(ctx, app.CustomerPIN, app.CustomerSerial)
                if err != nil {
                        slog.Warn("customer-confirm: AZMK getMkrScore failed — fail-soft (akb stays 0)",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "error", err)
                } else if mkrScore != nil {
                        resp := strings.ToUpper(mkrScore.Score.Response)
                        if resp == "AB" || resp == "NI" || resp == "NU" || resp == "TY" {
                                // AKB stop factor — reject the application immediately, do not let the
                                // customer proceed. This is rule 4 from PR #51.
                                slog.Info("customer-confirm: AKB stop factor present — rejecting customer submission",
                                        "application_id", appID,
                                        "customer_pin", app.CustomerPIN,
                                        "response", resp)
                                app.Status = model.StatusRejected
                                app.RejectionReason = fmt.Sprintf("AKB_STOP_FACTOR:%s", resp)
                                app.AkbScore = 0
                                app.Amount = req.Amount
                                app.CardNumber = req.CardNumber
                                app.ActualAddress = req.ActualAddress
                                app.CustomerConfirmedAt = time.Now().Format(time.RFC3339)
                                app.CardOwnershipConfirmed = true
                                if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
                                        return nil, fmt.Errorf("failed to save rejection: %w", err)
                                }
                                return app, nil
                        }
                        resolvedAkb = mkrScore.Score.Point
                }
        }
        if resolvedAkb > 0 {
                app.AkbScore = resolvedAkb
        }

        // 4. Get credit offer → find the range matching the customer's amount → term_months
        // (GetOffer internally resolves the score fail-soft: LW first, then this fallback.)
        offer, err := s.GetOffer(ctx, app.CustomerPIN, app.AkbScore)
        if err != nil {
                slog.Error("customer-confirm: GetOffer failed — rejecting customer submission",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "error", err)
                return nil, fmt.Errorf("texniki xəta — kredit təklifi əldə edilə bilmədi, bir az sonra yenidən cəhd edin")
        }

        matchedRange, err := findRangeForAmount(offer.Ranges, req.Amount)
        if err != nil {
                return nil, fmt.Errorf("seçdiyiniz məbləğ %.0f AZN sizin kredit səviyyəniz (%s) üçün keçərli deyil: %w",
                        req.Amount, offer.CreditLevel, err)
        }

        app.Amount = req.Amount
        app.TermMonths = matchedRange.TermMonths
        app.CardNumber = req.CardNumber
        app.ActualAddress = req.ActualAddress
        app.CustomerConfirmedAt = time.Now().Format(time.RFC3339)
        app.CardOwnershipConfirmed = true

        // PR #95: validate discount code if the customer entered one.
        // The code is NOT marked as 'used' here — that happens atomically at
        // approval. If the customer enters an invalid code, we reject the whole
        // request so the customer can correct it.
        //
        // PR #108: xəta mesajlarına 'Endirim kodu:' prefiks əlavə olunur ki,
        // istifadəçi xətanın endirim koduna aid olduğunu dərhal başa düşsün.
        if req.DiscountCode != "" && s.discountSvc != nil {
                customer, err := s.customerRepo.GetByPIN(ctx, app.CustomerPIN)
                if err != nil {
                        slog.Error("customer-confirm: failed to fetch customer for discount validation",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "error", err)
                        return nil, fmt.Errorf("Endirim kodu yoxlanıla bilmədi — texniki xəta, bir az sonra yenidən cəhd edin")
                }
                if _, err := s.discountSvc.ValidateForCustomer(ctx, req.DiscountCode, customer.ID); err != nil {
                        slog.Info("customer-confirm: discount code rejected",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "discount_code", req.DiscountCode,
                                "error", err)
                        // PR #108: prefiks əlavə et ki, istifadəçi xətanın endirim koduna
                        // aid olduğunu dərhal başa düşsün (ValidateForCustomer artıq
                        // Azərbaycan dilində aydın mesajlar qaytarır: "yanlış endirim kodu",
                        // "öz endirim kodunuzdan...", "artıq istifadə olunub" və s.)
                        return nil, fmt.Errorf("Endirim kodu: %w", err)
                }
                app.DiscountCode = req.DiscountCode
                slog.Info("customer-confirm: discount code accepted",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "discount_code", app.DiscountCode)
        }

        // PR #225: total_amount və approved_rate hesabla (credit engine olmadan)
        // commission = matchedRange.Commission (məs: 14)
        // total_amount = principal + commission_amount = calculateTotalAmount(amount, commission)
        // approved_rate = commission rate
        commissionRate := matchedRange.Commission
        app.ApprovedRate = commissionRate
        app.TotalAmount = calculateTotalAmount(app.Amount, commissionRate)
        app.CreditLevel = offer.CreditLevel
        slog.Info("customer-confirm: calculated total_amount",
                "application_id", appID,
                "amount", app.Amount,
                "commission_rate", commissionRate,
                "total_amount", app.TotalAmount,
                "credit_level", app.CreditLevel)

        // PR #221: transition to 'pending_expert' — RDC dashboard-a göndərilir.
        // Əvvəl (Variant B): pending → credit engine işləyir → pending_approval/rejected.
        // İndi (PR #221): pending_expert — expert dashboard-da görünür, expert təsdiq/redd edir.
        // Credit engine artıq OTP verify mərhələsində cutoff-ları işlədib, nəticələri saxlayıb.
        // Expert role: MyGov verify, kontaktlar, timer, approve/reject.
        app.Status = model.StatusPendingExpert

        // PR #118: AZMK Card registration
        // Müştəri kart nömrəsini daxil edib təsdiq edəndən sonra, kartı AZMK-ya qeyd edirik.
        // Bu, sonradan disburse zamanı istifadə olunacaq.
        if s.azmkProvider != nil && app.PartnerID != "" {
                cardReq := &azmk.CardRequest{
                        CardData: azmk.CardData{
                                PartnerID: app.PartnerID,
                                Code:      req.CardNumber,
                                Expiring:  s.azmkCardExpiring,
                        },
                }
                cardID, err := s.azmkProvider.RegisterCard(ctx, cardReq)
                if err != nil {
                        slog.Error("AZMK Card registration failed",
                                "application_id", appID,
                                "customer_pin", app.CustomerPIN,
                                "partner_id", app.PartnerID,
                                "error", err)
                        return nil, fmt.Errorf("Kart qeydiyyatı uğursuz: %w", err)
                }
                app.CardID = cardID
                slog.Info("AZMK Card registered",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "partner_id", app.PartnerID,
                        "card_id", cardID)
        }

        // 5. Save
        if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
                return nil, fmt.Errorf("failed to save customer confirmation: %w", err)
        }

        slog.Info("customer confirmed application, triggering credit engine",
                "application_id", appID,
                "customer_pin", app.CustomerPIN,
                "customer_full_name", app.CustomerFullName,
                "amount", app.Amount,
                "term_months", app.TermMonths,
                "akb_score", app.AkbScore,
                "credit_level", offer.CreditLevel)

        // PR #221: credit engine artıq OTP verify mərhələsində cutoff-ları işlədib.
        // Customer-confirm-də credit engine çağırmırıq — müraciət pending_expert-ə keçir.
        // Expert dashboard-da təsdiq/redd edəcək (approve/reject).
        slog.Info("customer-confirm: application saved, transitioning to pending_expert (RDC dashboard)",
                "application_id", appID,
                "customer_pin", app.CustomerPIN,
                "amount", app.Amount,
                "term_months", app.TermMonths,
                "credit_level", offer.CreditLevel)

        // Save the final state
        if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
                return nil, fmt.Errorf("failed to save customer confirmation: %w", err)
        }

        finalApp, err := s.repo.GetApplicationByID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to fetch application after confirm: %w", err)
        }

        // PR #221: Variant B downgrade silindi — credit engine customer-confirm-də çağrılmır.
        // Müraciət pending_expert statusunda RDC dashboard-a göndərilir.
        // Expert approve/reject edəcək.

        return finalApp, nil
}

// findRangeForAmount returns the first OfferRange whose [min_amount, max_amount]
// interval contains the given amount. Returns an error if no range matches —
// this happens when the customer picks an amount outside their credit level's
// allowed interval (shouldn't occur with a well-behaved UI slider, but we
// defend against it anyway).
func findRangeForAmount(ranges []OfferRange, amount float64) (OfferRange, error) {
        for _, r := range ranges {
                if amount >= r.MinAmount && amount <= r.MaxAmount {
                        return r, nil
                }
        }
        return OfferRange{}, fmt.Errorf("no offer range covers amount %.0f (checked %d ranges)", amount, len(ranges))
}
