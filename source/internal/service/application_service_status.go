package service

import (
	"context"
	"fmt"
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
//   - Only "approved" and "rejected" are accepted.
//   - The application must be in "pending_approval" status (set by the credit engine
//     for New/Trusted/Valuable levels after all checks pass).
//   - When status is "approved", credit_level is required — it is stored on the application
//     so that CountApprovedAtLevel can find it for unlock_phase calculation.
//   - When status is "rejected", credit_level is optional.
func (s *ApplicationService) UpdateStatus(ctx context.Context, id int, req *UpdateStatusRequest) (*model.LoanApplication, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}

	// Validate status
	if req.Status != "approved" && req.Status != "rejected" {
		return nil, fmt.Errorf("status must be 'approved' or 'rejected', got '%s'", req.Status)
	}

	// Fetch existing application to verify it exists
	app, err := s.repo.GetApplicationByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validate that the application is in pending_approval status
	if app.Status != "pending_approval" {
		return nil, fmt.Errorf("application status is '%s', expected 'pending_approval' — only applications awaiting manual review can be updated", app.Status)
	}

	// Validate credit_level is provided for approvals
	if req.Status == "approved" && req.CreditLevel == "" {
		return nil, fmt.Errorf("credit_level is required when status is 'approved'")
	}

	// Update via UpdateApplicationDecision so credit_level is stored
	creditLevel := req.CreditLevel
	if creditLevel == "" {
		creditLevel = app.CreditLevel // keep existing if not provided for rejection
	}

	var rejectionReason string
	if req.Status == "rejected" {
		rejectionReason = "Manually rejected"
	}

	err = s.repo.UpdateApplicationDecision(ctx, id,
		req.Status, creditLevel, rejectionReason, app.Amount, app.ApprovedRate)
	if err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	// Save credit level history for manual approvals (same as auto-approve for Elite)
	if req.Status == "approved" {
		if histErr := s.repo.SaveCreditLevelHistory(ctx, app.CustomerPIN, creditLevel, id); histErr != nil {
			_ = histErr
		}
	}

	// Return the updated application
	return s.repo.GetApplicationByID(ctx, id)
}
