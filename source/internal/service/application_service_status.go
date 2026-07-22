package service

import (
        "context"
        "fmt"
        "log/slog"

        "rdc-source/internal/model"
)

// UpdateStatusRequest is the request body for manually updating an application's status.
type UpdateStatusRequest struct {
        Status      string `json:"status"`       // "approved", "rejected", or "cancelled"
        CreditLevel string `json:"credit_level"` // required when status is "approved" (e.g. "new", "trusted", "valuable", "elite")
        Reason      string `json:"reason,omitempty"` // optional reason for rejection/cancellation
}

// UpdateStatus manually sets an application's status.
// This is the manual endpoint used by operators (experts).
//
// Rules:
//   - Accepted statuses: approved, rejected, cancelled.
//   - For approved/rejected: the application must be in pending_approval.
//   - For cancelled: the application must be in pending_expert or pending_approval.
//   - When status is approved, credit_level is required — it is stored on the
//     application so that CountApprovedAtLevel can find it for unlock_phase
//     calculation.
//   - When status is rejected, credit_level is optional (kept from existing).
//   - When status is cancelled, credit_level is ignored (kept from existing).
func (s *ApplicationService) UpdateStatus(ctx context.Context, id int, req *UpdateStatusRequest) (*model.LoanApplication, error) {
        if id <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }

        // Validate status
        if req.Status != model.StatusApproved &&
                req.Status != model.StatusRejected &&
                req.Status != model.StatusCancelled {
                return nil, fmt.Errorf("status must be '%s', '%s', or '%s', got '%s'",
                        model.StatusApproved, model.StatusRejected, model.StatusCancelled, req.Status)
        }

        // Fetch existing application to verify it exists
        app, err := s.repo.GetApplicationByID(ctx, id)
        if err != nil {
                return nil, err
        }

        // Validate status transition rules:
        //   approved / rejected  ← only from pending_approval
        //   cancelled           ← from pending_expert or pending_approval
        if req.Status == model.StatusCancelled {
                if !model.IsCancellable(app.Status) {
                        return nil, fmt.Errorf("application cannot be cancelled from status '%s' — only '%s' or '%s' applications can be cancelled",
                                app.Status, model.StatusPendingExpert, model.StatusPendingApproval)
                }
        } else {
                // approved / rejected
                if app.Status != model.StatusPendingApproval {
                        return nil, fmt.Errorf("application status is '%s', expected '%s' — only applications awaiting manual review can be approved/rejected",
                                app.Status, model.StatusPendingApproval)
                }
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
                creditLevel = app.CreditLevel // keep existing if not provided for rejection/cancellation
        }

        var rejectionReason string
        var totalAmount float64
        switch req.Status {
        case model.StatusRejected:
                if req.Reason != "" {
                        rejectionReason = req.Reason
                } else {
                        rejectionReason = "Manually rejected"
                }
        case model.StatusCancelled:
                if req.Reason != "" {
                        rejectionReason = "Cancelled by operator: " + req.Reason
                } else {
                        rejectionReason = "Cancelled by operator"
                }
        case model.StatusApproved:
                // Calculate total amount for manual approval (Principal + Interest)
                totalAmount = calculateTotalAmount(app.Amount, app.ApprovedRate)
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
