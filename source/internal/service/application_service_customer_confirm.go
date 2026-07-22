package service

import (
        "context"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
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
//   - customer_full_name  ← LW router GetPersonalInfo (fail-hard on error)
//   - akb_score           ← LW router GetAkbScore     (fail-hard on error)
//   - term_months         ← GetOffer ranges, matched to the selected amount
//   - contact1/2/3_phone  ← expert fills these later via CompleteApplication
type CustomerConfirmRequest struct {
        Amount                  float64 `json:"amount"`
        CardNumber              string  `json:"card_number"`
        ActualAddress           string  `json:"actual_address"`
        CardOwnershipConfirmed  bool    `json:"card_ownership_confirmed"`
}

// CustomerConfirmApplication finalizes the customer-side of the application flow.
//
// Pipeline:
//  1. Validate the request (amount > 0, 16-digit card, address non-empty, checkbox ticked)
//  2. Fetch the application — must be in pending_expert status
//  3. Fetch PersonalInfo from LW router → customer_full_name (FAIL-HARD on error)
//  4. Resolve AKB score from LW router (FAIL-HARD on error)
//  5. Get credit offer → find the range matching the customer's amount → term_months
//  6. Save amount, term_months, card, address, full_name, akb_score,
//     customer_confirmed_at = now(), card_ownership_confirmed = true
//  7. Application stays in pending_expert — the expert will later call
//     CompleteApplication to add the 3 contact phones and trigger the engine.
//
// Why fail-hard on LW errors (PR #58, business decision):
//   - PersonalInfo and AKB are required for the credit engine to function
//   - Allowing the customer to submit without them would mean the expert sees
//     an empty application and has no way to recover
//   - Customer gets a clear "technical error, please try again" message
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

        // 1. Fetch the application — must be pending_expert (customer verified OTP)
        app, err := s.repo.GetApplicationByID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }
        if app.Status != model.StatusPendingExpert {
                return nil, fmt.Errorf("application is not in pending_expert status (current: %s) — only OTP-verified applications can be confirmed", app.Status)
        }

        // 2. Fetch PersonalInfo from LW router — fail-hard on error (business decision PR #58)
        personalInfo, err := s.creditEngine.lwProvider.GetPersonalInfo(ctx, app.CustomerPIN, app.CustomerSerial)
        if err != nil {
                slog.Error("customer-confirm: GetPersonalInfo failed — rejecting customer submission",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN,
                        "error", err)
                return nil, fmt.Errorf("texniki xəta — şəxsi məlumatlar əldə edilə bilmədi, bir az sonra yenidən cəhd edin")
        }
        if personalInfo == nil || personalInfo.FullName == "" {
                slog.Error("customer-confirm: GetPersonalInfo returned empty full name",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN)
                return nil, fmt.Errorf("texniki xəta — şəxsi məlumatlar boş qayıtdı, bir az sonra yenidən cəhd edin")
        }
        app.CustomerFullName = personalInfo.FullName

        // 3. Resolve AKB score from LW router — fail-hard on error (business decision PR #58)
        resolvedAkb, _, hasStopFactor := s.creditEngine.resolveAkbScoreAndStopFactors(ctx, app.CustomerPIN, 0)
        if hasStopFactor {
                // AKB stop factor — reject the application immediately, do not let the
                // customer proceed. This is rule 4 from PR #51.
                slog.Info("customer-confirm: AKB stop factor present — rejecting customer submission",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN)
                app.Status = model.StatusRejected
                app.RejectionReason = "AKB stop factor present (rejected at customer-confirm stage)"
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
        if resolvedAkb == 0 {
                // AKB returned no usable data (Point == 0). Per business decision PR #58,
                // we fail-hard — the credit engine needs a real score.
                slog.Error("customer-confirm: AKB returned no usable score — rejecting customer submission",
                        "application_id", appID,
                        "customer_pin", app.CustomerPIN)
                return nil, fmt.Errorf("texniki xəta — AKB skoru əldə edilə bilmədi, bir az sonra yenidən cəhd edin")
        }
        app.AkbScore = resolvedAkb

        // 4. Get credit offer → find the range matching the customer's amount → term_months
        offer, err := s.GetOffer(ctx, app.CustomerPIN, resolvedAkb)
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
        // Status stays pending_expert — the expert will later call CompleteApplication
        // to add contact1/2/3_phone and trigger the credit engine.

        // 5. Save
        if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
                return nil, fmt.Errorf("failed to save customer confirmation: %w", err)
        }

        slog.Info("customer confirmed application",
                "application_id", appID,
                "customer_pin", app.CustomerPIN,
                "customer_full_name", app.CustomerFullName,
                "amount", app.Amount,
                "term_months", app.TermMonths,
                "akb_score", app.AkbScore,
                "credit_level", offer.CreditLevel)

        return s.repo.GetApplicationByID(ctx, appID)
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
