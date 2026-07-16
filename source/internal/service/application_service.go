package service

import (
        "context"
        "fmt"
        "log/slog"

        "rdc-source/internal/model"
)

// ApplicationService handles loan application business logic.
type ApplicationService struct {
        repo         ApplicationStore
        creditEngine *CreditEngine
        customerRepo CustomerStore
	otpService   *OTPService
}

// NewApplicationService creates a new ApplicationService.
// The repo parameter accepts any ApplicationStore implementation (e.g.
// *repository.ApplicationRepo in production, or a mock in tests).
// The customerRepo is used to find or create a customer record before
// the application is created — customer info is stored in a single
// profile, not duplicated per application.
func NewApplicationService(repo ApplicationStore, engine *CreditEngine, customerRepo CustomerStore, otpService *OTPService) *ApplicationService {
        return &ApplicationService{
                repo:         repo,
                creditEngine: engine,
                customerRepo: customerRepo,
		otpService:   otpService,
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
        // Validate card number: must be exactly 16 digits
        if len(req.CardNumber) != 16 {
                return nil, fmt.Errorf("card_number must be exactly 16 digits")
        }
        for _, c := range req.CardNumber {
                if c < '0' || c > '9' {
                        return nil, fmt.Errorf("card_number must contain only digits")
                }
        }

        app := &model.LoanApplication{
                CustomerPIN:      req.CustomerPIN,
                CustomerFullName: req.CustomerFullName,
                Amount:           req.Amount,
                TermMonths:       req.TermMonths,
                LoanPurpose:      req.LoanPurpose,
                Status:           model.StatusPending,
                AkbScore:         req.AkbScore,
                CardNumber:       req.CardNumber,
                CustomerPhone:    req.CustomerPhone,
                Contact1Phone:    req.Contact1Phone,
                Contact2Phone:    req.Contact2Phone,
                Contact3Phone:    req.Contact3Phone,
                ActualAddress:    req.ActualAddress,
        }

        // Find or create the customer record (single profile per PIN).
        // Customer info is stored in the customers table, not duplicated
        // per application.
        customer := &model.Customer{
                CustomerPIN:   req.CustomerPIN,
                FullName:      req.CustomerFullName,
                ActualAddress: req.ActualAddress,
        }
        if err := s.customerRepo.GetOrCreate(ctx, customer); err != nil {
                return nil, fmt.Errorf("failed to find or create customer: %w", err)
        }
        slog.Info("customer ready",
                "customer_id", customer.ID,
                "customer_pin", customer.CustomerPIN)

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

        // Link the application to the customer record (best-effort —
        // if this fails, the application is still valid, just not linked).
        if err := s.customerRepo.LinkApplication(ctx, app.ID, customer.ID); err != nil {
                slog.Warn("failed to link application to customer",
                        "application_id", app.ID,
                        "customer_id", customer.ID,
                        "error", err)
        }

        // Trigger credit engine asynchronously with retry (T-1.2). The HTTP
        // response returns immediately; the pipeline runs in the background.
        // If all retries fail, the application is marked as rejected with a
        // descriptive reason (see retry.go::triggerAsyncProcessing).
        s.triggerAsyncProcessing(app)

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
        if model.IsFinal(app.Status) || app.Status == model.StatusPendingApproval {
                decision := &model.DecisionResult{
                        Decision:       app.Status,
                        ApprovedAmount: app.ApprovedAmount,
                        ApprovedRate:   app.ApprovedRate,
                        DecidedAt:      app.UpdatedAt,
                }
                if app.Status == model.StatusRejected {
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

// ListPendingApproval retrieves all applications in pending_approval status.
// Used by the expert queue endpoint to show operators which applications
// are waiting for manual review. Ordered by oldest first (FIFO).
func (s *ApplicationService) ListPendingApproval(ctx context.Context) ([]model.LoanApplication, error) {
        return s.repo.ListByStatus(ctx, model.StatusPendingApproval)
}
