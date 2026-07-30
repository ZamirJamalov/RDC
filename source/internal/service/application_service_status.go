package service

import (
        "context"
        "fmt"
        "log/slog"

        "rdc-source/internal/model"
)

// UpdateStatusRequest is the request body for manually updating an application's status (mock/testing endpoint).
type UpdateStatusRequest struct {
        Status      string `json:"status"`       // "approved" or "rejected"
        CreditLevel string `json:"credit_level"` // required when status is "approved" (e.g. "new", "trusted", "valuable", "elite")
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

        // Validate that the application is in pending_approval status
        if app.Status != model.StatusPendingApproval {
                return nil, fmt.Errorf("application status is '%s', expected '%s' — only applications awaiting manual review can be updated",
                        app.Status, model.StatusPendingApproval)
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
                rejectionReason = "Manually rejected"
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
        }

        // Return the updated application
        return s.repo.GetApplicationByID(ctx, id)
}

// validateAndComputeDiscount re-validates the discount code on the application
// (race-condition protection) and computes the discount amount to apply.
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

        // Compute the commission amount (without principal) and apply discount.
        commissionAmount := calculateCommissionAmount(app.Amount, app.ApprovedRate)
        discount := s.discountSvc.CalculateDiscount(dc, commissionAmount)

        slog.Info("approval: discount applied",
                "application_id", app.ID,
                "discount_code", app.DiscountCode,
                "discount_type", dc.DiscountType,
                "discount_value", dc.DiscountValue,
                "commission_amount", commissionAmount,
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

        // Send SMS to the customer with their new code.
        if customerPhone == "" {
                slog.Warn("approval: customer_phone empty — cannot send discount code SMS",
                        "application_id", appID,
                        "customer_pin", customerPIN,
                        "discount_code", newCode.Code)
                return
        }

        if s.smsProvider == nil {
                slog.Warn("approval: smsProvider nil — cannot send discount code SMS",
                        "application_id", appID,
                        "discount_code", newCode.Code)
                return
        }

        smsMsg := fmt.Sprintf("Hormetli musteri, sizin kreditiniz tesdiq edildi! Endirim kodunuz: %s. Bu kodu novbeti kredit goturenle paylasin — komisiya endirimi yararlan.", newCode.Code)
        if err := s.smsProvider.Send(ctx, customerPhone, smsMsg); err != nil {
                slog.Error("approval: failed to send discount code SMS (non-fatal)",
                        "application_id", appID,
                        "customer_pin", customerPIN,
                        "phone", customerPhone,
                        "discount_code", newCode.Code,
                        "error", err)
                return
        }

        slog.Info("approval: discount code SMS sent",
                "application_id", appID,
                "customer_pin", customerPIN,
                "phone", customerPhone,
                "discount_code", newCode.Code)
}
