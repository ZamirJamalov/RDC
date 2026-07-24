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
        if req.Status == model.StatusRejected {
                rejectionReason = "Manually rejected"
        } else if req.Status == model.StatusApproved {
                // Calculate total amount for manual approval (Principal + Interest)
                totalAmount = calculateTotalAmount(app.Amount, app.ApprovedRate) // ApprovedRate is commission
        }

        err = s.repo.UpdateApplicationDecision(ctx, id,
                req.Status, creditLevel, rejectionReason, app.Amount, app.ApprovedRate, totalAmount)
        if err != nil {
                return nil, fmt.Errorf("failed to update status: %w", err)
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
        }

        // Return the updated application
        return s.repo.GetApplicationByID(ctx, id)
}
