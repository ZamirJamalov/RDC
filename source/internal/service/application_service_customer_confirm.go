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
//   - customer_full_name  ← early cutoff mərhələsində saxlanmış AZMK GetPersonalInfo
//     cavabı (PR #243); boşdursa fallback olaraq burada çağırılır
//     (fail-soft, PR #227)
//   - akb_score           ← early cutoff mərhələsində PR #228 ilə saxlanmış
//     AZMK getMkrScore cavabı (PR #243); 0-dırsa fallback
//     olaraq burada çağırılır (fail-soft, PR #227)
//   - term_months         ← GetOffer ranges, matched to the selected amount
//   - contact1/2/3_phone  ← expert fills these later via CompleteApplication
type CustomerConfirmRequest struct {
	Amount                 float64 `json:"amount"`
	CardNumber             string  `json:"card_number"`
	ActualAddress          string  `json:"actual_address"`
	CardOwnershipConfirmed bool    `json:"card_ownership_confirmed"`
	// PR #149: CustomerPhone for IDOR check — must match the application's customer_phone.
	CustomerPhone string `json:"customer_phone"`
	// PR #95: optional discount/referral code entered by the customer.
	DiscountCode string `json:"discount_code,omitempty"`
	// PR #230: istifadəçi tərəfindən seçilən müddət (əgər bir neçə term varsa)
	TermMonths int `json:"term_months,omitempty"`
	// PR #313: müştərinin AZMK-da əvvəldən qeydiyyatda olan kartı seçilibsə
	// onun AZMK card ID-si. Dolu olanda card_number ignore edilir (yeni kart
	// daxil edilmir) və RegisterCard çağırılmır — kartın card_id-si birbaşa
	// yazılır və create/disburse-da göndərilir (PR #355).
	SelectedCardID string `json:"selected_card_id,omitempty"`
}

// CustomerConfirmApplication finalizes the customer-side of the application flow.
//
// Pipeline:
//  1. Validate the request (amount > 0, 16-digit card, address non-empty, checkbox ticked)
//  2. Fetch the application — must be in pending_customer status
//  3. Fetch PersonalInfo from AZMK → customer_full_name (fail-soft, PR #227;
//     PR #243: early mərhələdə saxlanılıbsa çağırılmır)
//  4. Resolve AKB score from AZMK getMkrScore (fail-soft, PR #227;
//     PR #243: app.AkbScore > 0 olanda çağırılmır — PR #228 ilə saxlanılıb)
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
	// PR #313: köhnə kart seçilibsə card_number tələb olunmur (maskalı kod
	// AZMK-dan götürülür). Yeni kart daxil edilirsə 16 rəqəm tələb olunur.
	if req.SelectedCardID == "" && len(req.CardNumber) != 16 {
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

	// PR #313: köhnə kart seçilib — AZMK kart siyahısında bu tətbiqin
	// partner-i altında olduğunu təsdiqlə. Bu, başqa partnerin kart ID-si
	// ilə disburse-a yol vermir (fraud qorunması) və bitmiş kartı bloklayır.
	var selectedCard *azmk.CardInfo
	if req.SelectedCardID != "" {
		if s.azmkProvider == nil || app.PartnerID == "" {
			return nil, fmt.Errorf("seçilmiş kart təsdiqlənə bilmədi — zəhmət olmasa yeni kart daxil edin")
		}
		cards, err := s.azmkProvider.GetCards(ctx, app.PartnerID)
		if err != nil {
			slog.Error("PR #313: AZMK GetCards failed during confirm",
				"application_id", appID,
				"partner_id", app.PartnerID,
				"error", err)
			return nil, fmt.Errorf("seçilmiş kart yoxlanıla bilmədi — zəhmət olmasa yeni kart daxil edin")
		}
		selectedCard = findActiveCardByID(cards, req.SelectedCardID, time.Now())
		if selectedCard == nil {
			return nil, fmt.Errorf("seçilmiş kart tapılmadı və ya bitib — zəhmət olmasa siyahıdan yenidən seçin və ya yeni kart daxil edin")
		}
	}
	// Kartın göstərilən dəyəri: yeni kart üçün PAN, köhnə kart üçün AZMK
	// maskalı kodu (16 simvola sığması üçün defissizləşdirilir).
	cardDisplay := req.CardNumber
	if selectedCard != nil {
		cardDisplay = maskCardCode(selectedCard.Code)
	}

	// 2. PR #227: Fetch PersonalInfo from AZMK CustomerDataService — fail-soft.
	// LW router yanaşması deaktiv edildi; AZMK birincil mənbədir.
	// Xəta olsa: warn + davam (ad boş qalır, müraciət expert-ə keçir).
	// PR #243: ad artıq early cutoff mərhələsində alınaraq saxlanılıbsa (AZMK_GET_PERSONAL_INFO
	// müraciəti bir dəfə edilir) — yenidən çağırmağa ehtiyac yoxdur. Yalnız boşdursa çağır
	// (köhnə müraciətlər / early mərhələdə xəta olanlar üçün fallback).
	if s.customerDataProvider != nil {
		if app.CustomerFullName == "" {
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
		} else {
			slog.Info("customer-confirm: customer_full_name already saved — skipping AZMK GetPersonalInfo (PR #243)",
				"application_id", appID)
		}
	}

	// 3. PR #227: AKB skoru AZMK getMkrScore-dan — fail-soft.
	// Skor alınmasa akb=0 ilə davam edilir; GetOffer öz fallback-ini işlədir.
	// Stop-faktor yalnız məlumat gələndə yoxlanılır (müdafiə məqsədilə —
	// əsas yoxlama OTP verify mərhələsindəki early cutoff-lardadır).
	// PR #243: app.AkbScore > 0 demək early cutoff-larda PR #228 ilə saxlanılıb —
	// pending_customer statusu həm skor, həm stop-faktor yoxlamasının keçdiyini
	// zəmanət verir (kəsilsəydı müraciət rejected olardı). İkinci AZMK_GET_MKR_SCORE
	// sorğusuna ehtiyac yoxdur. AkbScore == 0 (point=0 və ya köhnə müraciət) olanda
	// müdafiə məqsədilə bir dəfə çağırılır.
	resolvedAkb := 0
	if s.customerDataProvider != nil && app.AkbScore == 0 {
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
				app.CardNumber = cardDisplay
				app.ActualAddress = req.ActualAddress
				app.CustomerConfirmedAt = time.Now().Format(time.RFC3339)
				app.CardOwnershipConfirmed = true
				if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
					return nil, fmt.Errorf("failed to save rejection: %w", err)
				}
				// PR #362: AKB stop factor reject — müştəriyə imtina SMS-i (non-fatal)
				s.sendRejectionSMS(ctx, app)
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

	matchedRange, err := findRangeForAmount(offer.Ranges, req.Amount, req.TermMonths)
	if err != nil {
		return nil, fmt.Errorf("seçdiyiniz məbləğ %.0f AZN sizin kredit səviyyəniz (%s) üçün keçərli deyil: %w",
			req.Amount, offer.CreditLevel, err)
	}

	app.Amount = req.Amount
	app.TermMonths = matchedRange.TermMonths
	app.CardNumber = cardDisplay
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

	// PR #118/#313/#355: AZMK Card registration
	// PR #355: köhnə kart seçilibsə RegisterCard ÇAĞIRILMIR — kart onsuzda
	// AZMK-da qeydiyyatdadır və onun card_id-si birbaşa istifadə olunur:
	// application/create (PR #353) və disburse cardId-ni app.CardID-dən göndərir.
	// (PR #347/#352-dəki maskalı-kodla yenidən qeydiyyat ləğv olunub — AZMK
	// maskalı kodu 400 "Invalid code" ilə rədd edir, çağırış mənasız idi.)
	// Yeni kart daxil edilibsə RegisterCard çağırılır; xəta baş verərsə proses
	// DAVAM ETMİR — confirm hard-fail olur (PR #352), müraciət pending_customer
	// qalır və RDC dashboard-a DÜŞMÜR.
	if selectedCard != nil {
		app.CardID = selectedCard.ID
		slog.Info("PR #355: saved card selected — RegisterCard skipped, card_id used directly",
			"application_id", appID,
			"customer_pin", app.CustomerPIN,
			"partner_id", app.PartnerID,
			"card_id", selectedCard.ID,
			"card_code", selectedCard.Code)
	} else if s.azmkProvider != nil && app.PartnerID != "" {
		cardReq := &azmk.CardRequest{
			CardData: azmk.CardData{
				PartnerID: app.PartnerID,
				Code:      req.CardNumber,
				Expiring:  s.azmkCardExpiring,
			},
		}
		cardID, err := s.azmkProvider.RegisterCard(ctx, cardReq)
		if err != nil {
			slog.Error("PR #352: AZMK Card registration failed — confirm bloklanır, müraciət dashboard-a getmir",
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
	slog.Info("PR #281: step 6 — customer confirmed, transitioning to pending_expert (RDC dashboard)",
		"step", "6.customer_confirm",
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
// PR #230: findRangeForAmount — əgər termMonths > 0-dırsa, amount + term-a uyğun range tap
func findRangeForAmount(ranges []OfferRange, amount float64, termMonths int) (OfferRange, error) {
	// Əvvəl amount + term-a uyğun range-i tap
	if termMonths > 0 {
		for _, r := range ranges {
			if amount >= r.MinAmount && amount <= r.MaxAmount && r.TermMonths == termMonths {
				return r, nil
			}
		}
	}
	// Yoxda, yalnız amount-a uyğun ilk range-i qaytar
	for _, r := range ranges {
		if amount >= r.MinAmount && amount <= r.MaxAmount {
			return r, nil
		}
	}
	return OfferRange{}, fmt.Errorf("no offer range covers amount %.0f term %d (checked %d ranges)", amount, termMonths, len(ranges))
}
