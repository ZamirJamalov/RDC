package service

import (
        "context"
        "fmt"
        "log/slog"

        "rdc-source/internal/model"
        "rdc-source/pkg/azmk"
        "rdc-source/pkg/otp"
)

// ApplicationService handles loan application business logic.
type ApplicationService struct {
        repo         ApplicationStore
        creditEngine *CreditEngine
        customerRepo CustomerStore
        otpService   *OTPService
        discountSvc  *DiscountCodeService // PR #95: set via SetDiscountService after construction
        smsProvider  otp.Provider         // PR #95: for approval SMS (may be nil if otpService is nil)

        // PR #117: AZMK Online Lending Service
        azmkProvider     azmk.Provider // set via SetAzmkProvider (nil = skip KYC/Partner)
        azmkBranch       string        // AZMK_BRANCH_CODE (məs. "HO")
        azmkCardExpiring string        // AZMK_CARD_EXPIRING (məs. "2030-01-01")
        azmkProductID    string        // AZMK_PRODUCT_ID (məs. "L07", config-dən dəyişilə bilər)
        azmkDisbursementFee float64    // AZMK_DISBURSEMENT_FEE (həmişə 0)
}

// NewApplicationService creates a new ApplicationService.
// The repo parameter accepts any ApplicationStore implementation (e.g.
// *repository.ApplicationRepo in production, or a mock in tests).
// The customerRepo is used to find or create a customer record before
// the application is created — customer info is stored in a single
// profile, not duplicated per application.
func NewApplicationService(repo ApplicationStore, engine *CreditEngine, customerRepo CustomerStore, otpService *OTPService) *ApplicationService {
        svc := &ApplicationService{
                repo:         repo,
                creditEngine: engine,
                customerRepo: customerRepo,
                otpService:   otpService,
        }
        if otpService != nil {
                svc.smsProvider = otpService.provider
        }
        return svc
}

// SetDiscountService injects the discount code service after construction
// (PR #95). Needed because DiscountCodeService is created after
// ApplicationService in main.go. When set, the approval flow will:
//   - validate the customer-entered discount code (if any)
//   - apply the discount to the commission
//   - mark the code as used
//   - generate a new code for the approved customer
//   - send an SMS with the new code
// When nil (e.g. in tests that don't care about discount), the approval
// flow proceeds without discount logic.
func (s *ApplicationService) SetDiscountService(svc *DiscountCodeService) {
        s.discountSvc = svc
}

// SetAzmkProvider injects the AZMK Online Lending provider after construction
// (PR #117). When set, VerifyInitApplication will:
//   1. Create AZMK KYC session → get kyc_id
//   2. Verify KYC (VERIFIED status)
//   3. Register Partner → get partner_id
//   4. Save kyc_id + partner_id to the application
// When nil (e.g. in tests), KYC/Partner steps are skipped.
func (s *ApplicationService) SetAzmkProvider(provider azmk.Provider, branchCode, cardExpiring, productID string, disbursementFee float64) {
        s.azmkProvider = provider
        s.azmkBranch = branchCode
        s.azmkCardExpiring = cardExpiring
        s.azmkProductID = productID
        s.azmkDisbursementFee = disbursementFee
}

// UpdateContactsRequest is the body for PUT /api/applications/{id}/contacts.
// PR #124: ekspert kontakt nömrələrini və yoxlanma statusunu saxlayır.
// Bu endpoint pending_approval statusunda da işləyir (CompleteApplication-a alternativ).
type UpdateContactsRequest struct {
        Contact1Phone    string `json:"contact1_phone,omitempty"`
        Contact2Phone    string `json:"contact2_phone,omitempty"`
        Contact3Phone    string `json:"contact3_phone,omitempty"`
        Contact1Relation string `json:"contact1_relation,omitempty"`
        Contact2Relation string `json:"contact2_relation,omitempty"`
        Contact3Relation string `json:"contact3_relation,omitempty"`
        // PR #128: kontakt şəxslərinin ad soyadı
        Contact1Name string `json:"contact1_name,omitempty"`
        Contact2Name string `json:"contact2_name,omitempty"`
        Contact3Name string `json:"contact3_name,omitempty"`
        // Verification: nil=yoxlanılmayıb, true=təsdiq, false=imtina
        Contact1Verified *bool `json:"contact1_verified,omitempty"`
        Contact2Verified *bool `json:"contact2_verified,omitempty"`
        Contact3Verified *bool `json:"contact3_verified,omitempty"`
}

// UpdateContacts saves contact phone numbers, relations, and verification status.
// PR #124: works in any status — unlike CompleteApplication which only works in pending_expert.
func (s *ApplicationService) UpdateContacts(ctx context.Context, appID int, req *UpdateContactsRequest) (*model.LoanApplication, error) {
        if appID <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }

        app, err := s.repo.GetApplicationByID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }

        // Update fields from request
        app.Contact1Phone = req.Contact1Phone
        app.Contact2Phone = req.Contact2Phone
        app.Contact3Phone = req.Contact3Phone
        app.Contact1Relation = req.Contact1Relation
        app.Contact2Relation = req.Contact2Relation
        app.Contact3Relation = req.Contact3Relation
        app.Contact1Name = req.Contact1Name
        app.Contact2Name = req.Contact2Name
        app.Contact3Name = req.Contact3Name
        app.Contact1Verified = req.Contact1Verified
        app.Contact2Verified = req.Contact2Verified
        app.Contact3Verified = req.Contact3Verified

        if err := s.repo.UpdateContacts(ctx, appID, app); err != nil {
                return nil, fmt.Errorf("failed to save contacts: %w", err)
        }

        slog.Info("contacts updated",
                "application_id", appID,
                "contact1_verified", req.Contact1Verified,
                "contact2_verified", req.Contact2Verified,
                "contact3_verified", req.Contact3Verified)

        return s.repo.GetApplicationByID(ctx, appID)
}

// UpdateTimer saves the elapsed timer seconds for an application.
// PR #134: ekspert müraciəti açandan təsdiq/imtinaya qədər vaxt saxlanır.
func (s *ApplicationService) UpdateTimer(ctx context.Context, appID int, seconds int) error {
        if appID <= 0 {
                return fmt.Errorf("invalid application id")
        }
        return s.repo.UpdateTimer(ctx, appID, seconds)
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
