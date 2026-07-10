package service

import (
	"context"
	"fmt"
	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// ApplicationService handles loan application business logic.
type ApplicationService struct {
	repo         *repository.ApplicationRepo
	creditEngine *CreditEngine
}

// NewApplicationService creates a new ApplicationService.
func NewApplicationService(repo *repository.ApplicationRepo, engine *CreditEngine) *ApplicationService {
	return &ApplicationService{
		repo:         repo,
		creditEngine: engine,
	}
}

// CreateApplication creates a new loan application with "pending" status and triggers the credit engine.
func (s *ApplicationService) CreateApplication(ctx context.Context, req *model.CreateApplicationRequest) (*model.LoanApplication, error) {
	if req.CustomerPIN == "" {
		return nil, fmt.Errorf("customer_pin is required")
	}
	if req.CustomerFullName == "" {
		return nil, fmt.Errorf("customer_full_name is required")
	}
	if req.Amount <= 0 {
		return nil, fmt.Errorf("amount must be greater than zero")
	}
	if req.TermMonths <= 0 {
		return nil, fmt.Errorf("term_months must be greater than zero")
	}

	app := &model.LoanApplication{
		CustomerPIN:      req.CustomerPIN,
		CustomerFullName: req.CustomerFullName,
		Amount:           req.Amount,
		TermMonths:       req.TermMonths,
		LoanPurpose:      req.LoanPurpose,
		Status:           "pending",
		AkbScore:         req.AkbScore,
	}

	// Check for duplicate: customer must not have an existing non-final application
	existingID, existingStatus, err := s.repo.HasPendingApplication(ctx, req.CustomerPIN)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing applications: %w", err)
	}
	if existingID > 0 {
		return nil, fmt.Errorf("mustərinin artıq işlənməkdə olan bir müraciəti var (id: %d, status: %s). Əvvəlki müraciət bitdikdən sonra yenisinə icazə verilir", existingID, existingStatus)
	}

	// Pre-validate: check if amount+term is valid for this customer's level
	// This runs synchronously so the user gets an immediate error (400) instead of a delayed rejection
	if err := s.creditEngine.PreValidate(ctx, req.CustomerPIN, req.Amount, req.TermMonths, req.AkbScore); err != nil {
		return nil, err
	}

	err = s.repo.CreateApplication(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("failed to create application: %w", err)
	}

	// Trigger credit engine asynchronously so the API returns immediately
	go func() {
		bgCtx := context.Background()
		if procErr := s.creditEngine.ProcessApplication(bgCtx, app.ID); procErr != nil {
			// Log the error; in production this would go to a proper logger
			_ = procErr
		}
	}()

	return app, nil
}

// GetApplication retrieves a single loan application by ID.
func (s *ApplicationService) GetApplication(ctx context.Context, id int) (*model.LoanApplication, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}
	return s.repo.GetApplicationByID(ctx, id)
}

// GetStatus retrieves the full status response including checks and decision for an application.
func (s *ApplicationService) GetStatus(ctx context.Context, id int) (*model.ApplicationStatusResponse, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}

	app, err := s.repo.GetApplicationByID(ctx, id)
	if err != nil {
		return nil, err
	}

	checks, err := s.repo.GetCheckResults(ctx, id)
	if err != nil {
		return nil, err
	}

	resp := &model.ApplicationStatusResponse{
		ApplicationID: app.ID,
		Status:        app.Status,
		CreditLevel:   app.CreditLevel,
		Checks:        checks,
	}

	// Include decision if the application has been decided or is awaiting manual approval
	if app.Status == "approved" || app.Status == "rejected" || app.Status == "pending_approval" {
		decision := &model.DecisionResult{
			Decision:       app.Status,
			ApprovedAmount: app.ApprovedAmount,
			ApprovedRate:   app.ApprovedRate,
			DecidedAt:      app.UpdatedAt,
		}
		if app.Status == "rejected" {
			decision.RejectionReason = app.RejectionReason
		}
		resp.Decision = decision
	}

	return resp, nil
}

// GetChecks retrieves all check results for an application.
func (s *ApplicationService) GetChecks(ctx context.Context, id int) ([]model.ApplicationCheckResult, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}
	return s.repo.GetCheckResults(ctx, id)
}

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
