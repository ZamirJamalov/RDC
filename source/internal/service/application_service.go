package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/azmk"
	"rdc-source/pkg/otp"
	"rdc-source/pkg/videorecord"
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
	// PR #349: azmkDisbursementFee silindi — disbursementFee artıq env-dən deyil,
	// credit_levels.commission-dan göndərilir (app.ApprovedRate / 100, AZMK kəsr formatında).

	// PR #152: AZMK CustomerDataService (yaş yoxlaması üçün)
	customerDataProvider azmk.CustomerDataProvider // set via SetCustomerDataProvider (nil = skip age check)

	// PR #168: Cutoff results repo (plan/fakt nəticələri)
	cutoffRepo *repository.CutoffResultRepo

	// PR #170: KYC verify toggle — false=KYC verify skip olunur
	kycVerifyEnabled bool

	// PR #171: Cutoff stop-on-first-fail — false=bütün kesimlər həmişə yoxlanılır
	cutoffStopOnFirstFail bool
	// PR #278: Cutoff checks enabled — false=cutoff-lar TAMAMƏN skip olunur
	cutoffChecksEnabled bool

	// PR #188: Video record service (Kvadrat Lab)
	videoRecordProvider    videorecord.Provider
	videoRecordRepo        *repository.VideoRecordRepo
	videoRecordEnabled     bool
	videoRecordWebhookURL  string
	videoRecordRedirectURL string

	// PR #205: Service cache (service_audit_logs üzərindən)
	serviceCacheRepo *repository.ServiceCacheRepo

	// PR #284: Referal SMS endirim faizi (X% parametri)
	referralDiscountPercent int
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
//
// When nil (e.g. in tests that don't care about discount), the approval
// flow proceeds without discount logic.
func (s *ApplicationService) SetDiscountService(svc *DiscountCodeService) {
	s.discountSvc = svc
}

// SetAzmkProvider injects the AZMK Online Lending provider after construction
// (PR #117). When set, VerifyInitApplication will:
//  1. Create AZMK KYC session → get kyc_id
//  2. Verify KYC (VERIFIED status)
//  3. Register Partner → get partner_id
//  4. Save kyc_id + partner_id to the application
//
// When nil (e.g. in tests), KYC/Partner steps are skipped.
//
// PR #349: disbursementFee parametri silindi — dəyər artıq env-dən deyil,
// hər müraciətin öz credit_levels.commission-dan (app.ApprovedRate/100) gəlir.
func (s *ApplicationService) SetAzmkProvider(provider azmk.Provider, branchCode, cardExpiring, productID string) {
	s.azmkProvider = provider
	s.azmkBranch = branchCode
	s.azmkCardExpiring = cardExpiring
	s.azmkProductID = productID
}

// SetCustomerDataProvider injects the AZMK CustomerDataService provider (PR #152).
// When set, runEarlyCutoffChecks will fetch customer data from AZMK and check age.
// When nil, age check uses the existing LW provider (backward compatible).
func (s *ApplicationService) SetCustomerDataProvider(provider azmk.CustomerDataProvider) {
	s.customerDataProvider = provider
}

// SetCutoffRepo injects the cutoff results repo (PR #168).
func (s *ApplicationService) SetCutoffRepo(repo *repository.CutoffResultRepo) {
	s.cutoffRepo = repo
}

// SetKycVerifyEnabled enables/disables KYC verify (PR #170).
// false=KYC verify skip olunur, cutoff-lar birbaşa yoxlanılır.
func (s *ApplicationService) SetKycVerifyEnabled(enabled bool) {
	s.kycVerifyEnabled = enabled
}

// IsKycVerifyEnabled returns whether KYC verify is enabled (PR #206).
// Frontend bu dəyərə əsasən KYC loading ekranını göstər/gizlət.
func (s *ApplicationService) IsKycVerifyEnabled() bool {
	return s.kycVerifyEnabled
}

// SetReferralDiscountPercent sets the referral SMS discount percent (PR #284).
// Disburse success SMS-indəki "X%" parametri (məs. 5 → "5% endirim").
func (s *ApplicationService) SetReferralDiscountPercent(percent int) {
	s.referralDiscountPercent = percent
}

// SetCutoffChecksEnabled controls whether cutoff checks run at all (PR #278).
// false = cutoff-lar TAMAMƏN skip olunur (heç bir kesim yoxlanılmır).
func (s *ApplicationService) SetCutoffChecksEnabled(enabled bool) {
	s.cutoffChecksEnabled = enabled
}

// SetCutoffStopOnFirstFail controls cutoff behavior (PR #171).
// true (default) = ilk kesim rədd edildikdə digərləri yoxlanılmır
// false = bütün kesimlər həmişə yoxlanılır
func (s *ApplicationService) SetCutoffStopOnFirstFail(stop bool) {
	s.cutoffStopOnFirstFail = stop
}

// SetVideoRecordProvider injects the video record provider (PR #188).
// nil = video record deaktiv.
func (s *ApplicationService) SetVideoRecordProvider(provider videorecord.Provider) {
	s.videoRecordProvider = provider
}

// SetVideoRecordRepo injects the video record repo (PR #188).
func (s *ApplicationService) SetVideoRecordRepo(repo *repository.VideoRecordRepo) {
	s.videoRecordRepo = repo
}

// SetVideoRecordEnabled enables/disables video record requirement (PR #188).
// false = video record tələb olunmur (default).
func (s *ApplicationService) SetVideoRecordEnabled(enabled bool, webhookURL, redirectURL string) {
	s.videoRecordEnabled = enabled
	s.videoRecordWebhookURL = webhookURL
	s.videoRecordRedirectURL = redirectURL
}

// IsVideoRecordEnabled returns whether video record is required (PR #188).
func (s *ApplicationService) IsVideoRecordEnabled() bool {
	return s.videoRecordEnabled && s.videoRecordProvider != nil && s.videoRecordRepo != nil
}

// SetServiceCacheRepo injects the service cache repo (PR #205).
// nil = cache deaktiv (hər zaman birbaşa servise muraciet).
func (s *ApplicationService) SetServiceCacheRepo(repo *repository.ServiceCacheRepo) {
	s.serviceCacheRepo = repo
}

// GetCachedServiceResponse checks if there's a cached response for the given service
// and customer within the cache window. PR #205.
// PR #379: appID parametri əlavə olundu — cache HIT olanda service_audit_logs-a
// method='CACHE' marker row yazılır (cədvəldə cache istifadəsi görünür).
// Returns (response_body, true) if cache hit, ("", false) if cache miss.
func (s *ApplicationService) GetCachedServiceResponse(ctx context.Context, appID int, serviceName, customerPIN string) (string, bool) {
	if s.serviceCacheRepo == nil {
		return "", false // cache deaktiv
	}
	cacheDays, err := s.serviceCacheRepo.GetCacheDays(ctx, serviceName)
	if err != nil {
		slog.Warn("service cache: failed to get cache_days", "service", serviceName, "error", err)
		return "", false
	}
	if cacheDays <= 0 {
		return "", false // cache_days=0 → birbaşa servisi çağır
	}
	responseBody, found, err := s.serviceCacheRepo.GetCachedResponse(ctx, serviceName, customerPIN, cacheDays)
	if err != nil {
		slog.Warn("service cache: failed to get cached response", "service", serviceName, "error", err)
		return "", false
	}
	if found {
		slog.Info("service cache: HIT", "service", serviceName, "customer_pin", customerPIN, "cache_days", cacheDays)
		// PR #379: audit cədvəlində cache istifadəsini görünür et
		if lerr := s.serviceCacheRepo.LogCacheHit(ctx, &appID, serviceName, customerPIN, responseBody); lerr != nil {
			slog.Warn("service cache: failed to log cache hit", "service", serviceName, "error", lerr)
		}
	}
	return responseBody, found
}

// fetchCustomerDataFromAzmk fetches customer data from AZMK CustomerDataService
// (GetPersonalInfo). Returns nil on error (fail-soft — no rejection).
// PR #152: yaş yoxlaması üçün əsas mənbə.
// PR #243: fullName da istifadə olunur — early cutoff mərhələsində saxlanılır ki
// customer-confirm/video eyni servisə ikinci sorğu göndərməsin.
// PR #245: RegistrationAddress da qaytarılır — qeydiyyat ünvanı DB-yə yazılır.
func (s *ApplicationService) fetchCustomerDataFromAzmk(ctx context.Context, customerPIN, serial string) *azmk.CustomerData {
	data, err := s.customerDataProvider.GetPersonalInfo(ctx, customerPIN, serial)
	if err != nil {
		slog.Warn("failed to fetch customer data from AZMK — fail-soft",
			"customer_pin", customerPIN, "error", err)
		return nil
	}
	return data
}

// UpdateContactNotesRequest is the body for PUT /api/applications/{id}/contact-notes (PR #266).
// Ekspertin zəng zamanı qeydləri — frontend blur event çağırır.
type UpdateContactNotesRequest struct {
	Contact1CallNote string `json:"contact1_call_note,omitempty"`
	Contact2CallNote string `json:"contact2_call_note,omitempty"`
	Contact3CallNote string `json:"contact3_call_note,omitempty"`
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

// UpdateAddressRequest is the body for PUT /api/applications/{id}/address.
// PR #245: ekspert faktiki ünvanı redaktə edir.
type UpdateAddressRequest struct {
	ActualAddress string `json:"actual_address"`
}

// UpdateActualAddress saves the expert-edited actual (factual) address.
// PR #245: dashboard-da faktiki ünvan redaktə edilə bilər və DB-də saxlanılır.
// Qərar verildikdən sonra (approved/rejected) dəyişmək olmaz (PR #135 paralel).
func (s *ApplicationService) UpdateActualAddress(ctx context.Context, appID int, address string) (*model.LoanApplication, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("faktiki ünvan boş ola bilməz")
	}
	if len(address) > 500 {
		return nil, fmt.Errorf("ünvan 500 simvoldan artıq ola bilməz")
	}

	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}
	if model.IsFinal(app.Status) {
		return nil, fmt.Errorf("qərar verildikdən sonra ünvan dəyişdirilə bilməz")
	}

	if err := s.repo.UpdateActualAddress(ctx, appID, address); err != nil {
		return nil, fmt.Errorf("failed to save actual address: %w", err)
	}
	app.ActualAddress = address

	slog.Info("actual address updated by expert", "application_id", appID)
	return app, nil
}

// BackfillRegistrationAddress fetches the AZMK registration address for an
// application that was created before PR #245 (registration_address boşdur).
// PR #245: dashboard-da qeydiyyat ünvanı boş olanda frontend bu endpoint-i çağırır.
// Fail-soft: AZMK xətası və ya provider yoxdursa tətbiq dəyişməz qaytarılır.
// Bir dəfəyə (one-time) — saxlandıqdan sonra bir daha çağırılmır.
func (s *ApplicationService) BackfillRegistrationAddress(ctx context.Context, appID int) (*model.LoanApplication, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("invalid application id")
	}
	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}
	if app.RegistrationAddress != "" {
		return app, nil // artıq var — AZMK çağırmağa ehtiyac yoxdur
	}
	if s.customerDataProvider == nil {
		return app, nil
	}

	data := s.fetchCustomerDataFromAzmk(ctx, app.CustomerPIN, app.CustomerSerial)
	if data == nil {
		return app, nil // fail-soft
	}
	if data.RegistrationAddress != "" {
		app.RegistrationAddress = data.RegistrationAddress
		if err := s.repo.UpdateRegistrationAddress(ctx, appID, data.RegistrationAddress); err != nil {
			slog.Warn("failed to save registration address to DB", "application_id", appID, "error", err)
		} else {
			slog.Info("registration address backfilled from AZMK", "application_id", appID)
		}
	}
	// PR #243 paralel: ad da boşdursa saxla (əlavə sorğu yoxdur — data artıq gəlib)
	if name := data.FullName(); name != "" && app.CustomerFullName == "" {
		app.CustomerFullName = name
		if err := s.repo.UpdateCustomerFullName(ctx, appID, name); err != nil {
			slog.Warn("failed to save customer full name to DB", "application_id", appID, "error", err)
		}
	}
	return app, nil
}

// SetProcessedBy records which dashboard user approved/rejected the application.
// PR #142: ekspert əməliyyatları istifadəçiyə bağlanır.
func (s *ApplicationService) SetProcessedBy(ctx context.Context, appID int, userID int, username string) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	return s.repo.UpdateProcessedBy(ctx, appID, userID, username)
}

// SetContactNotes saves expert call notes for each contact (PR #266).
// Frontend blur event çağırır — yalnız call notes save olunur (phone/relation yox).
func (s *ApplicationService) SetContactNotes(ctx context.Context, appID int, req *UpdateContactNotesRequest) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return fmt.Errorf("application not found: %w", err)
	}
	if model.IsFinal(app.Status) {
		return fmt.Errorf("qərar verildikdən sonra qeydlər dəyişdirilə bilməz")
	}
	app.Contact1CallNote = req.Contact1CallNote
	app.Contact2CallNote = req.Contact2CallNote
	app.Contact3CallNote = req.Contact3CallNote
	if err := s.repo.UpdateContactNotes(ctx, appID, app); err != nil {
		return fmt.Errorf("failed to save contact notes: %w", err)
	}
	slog.Info("contact call notes updated", "application_id", appID)
	return nil
}

// SetContactsAudit records which expert updated contact numbers.
// PR #148: audit fields.
func (s *ApplicationService) SetContactsAudit(ctx context.Context, appID int, userID int, username string) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	return s.repo.UpdateContactsAudit(ctx, appID, userID, username)
}

// SetTimerAudit records which expert saved the timer.
// PR #148: audit fields.
func (s *ApplicationService) SetTimerAudit(ctx context.Context, appID int, userID int, username string) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	return s.repo.UpdateTimerAudit(ctx, appID, userID, username)
}

// SetMyGovAudit records which expert performed MyGov verification.
// PR #148: audit fields.
func (s *ApplicationService) SetMyGovAudit(ctx context.Context, appID int, userID int, username string) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	return s.repo.UpdateMyGovAudit(ctx, appID, userID, username)
}

// SetActualAddressAudit records which expert updated the actual address.
// PR #249: PR #245 UpdateAddress endpoint faktiki ünvanı redaktə edirdi,
// amma kim tərəfindən dəyişdirildiyi DB-də qeyd olunmurdu (yalnız slog.Info).
func (s *ApplicationService) SetActualAddressAudit(ctx context.Context, appID int, userID int, username string) error {
	if appID <= 0 {
		return fmt.Errorf("invalid application id")
	}
	return s.repo.UpdateActualAddressAudit(ctx, appID, userID, username)
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
	// PR #257: HasPendingApplication 4 değer qaytarır (daysRemaining əlavə olundu, PR #256).
	// Bu axın (CreateApplication) admin tərəfindən yaradılan müraciətlər üçündür —
	// blocked rejection signalı burada lazım deyil, amma imza dəyişdiyi üçün 4 dəyər qəbul edirik.
	existingID, existingStatus, _, err := s.repo.HasPendingApplication(ctx, req.CustomerPIN)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing applications: %w", err)
	}
	if existingID > 0 {
		return nil, fmt.Errorf("mustərinin artıq işlənməkdə olan bir müraciəti var (id: %d, status: %s). Əvvəlki müraciət bitdikdən sonra yenisinə icazə verilir", existingID, existingStatus)
	}

	// Pre-validate: check if amount+term is valid for this customer's level
	// This runs synchronously so the user gets an immediate error (400) instead of a delayed rejection
	// PR #379: serial da göndürilir — AZMK inquireByIdCard serialsız 400 qaytarır
	if err := s.creditEngine.PreValidate(ctx, req.CustomerPIN, req.CustomerSerial, req.Amount, req.TermMonths, req.AkbScore); err != nil {
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

// GetApplicationByPublicID retrieves a loan application by its UUID public_id.
// PR #191: xarici API və UI public_id UUID istifadə edir.
func (s *ApplicationService) GetApplicationByPublicID(ctx context.Context, publicID string) (*model.LoanApplication, error) {
	if publicID == "" {
		return nil, fmt.Errorf("invalid public id")
	}
	app, err := s.repo.GetApplicationByPublicID(ctx, publicID)
	if err != nil {
		return nil, err
	}
	// PR #348: dashboard-da faizi komissiyadan ayrı göstərmək üçün illik faiz
	// dərəcəsini də qaytarırıq (AZMK create ilə eyni helper, eyni dəyər).
	if app.CreditLevel != "" {
		app.AnnualInterestRate = s.annualInterestRateForApp(ctx, app)
	}
	return app, nil
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

// ListPendingApproval retrieves all applications in pending_expert, pending_approval və pending_signature (PR #312) statusunda olan müraciətləri qaytarır.
// PR #221/#223: customer-confirm artıq pending_expert-ə keçir (əvvəl pending_approval idi).
// Expert dashboard hər üçünü göstərir — pending_signature imza gözləyən müraciətlərdir.
// Ordered by oldest first (FIFO).
func (s *ApplicationService) ListPendingApproval(ctx context.Context) ([]model.LoanApplication, error) {
	expertApps, err := s.repo.ListByStatus(ctx, model.StatusPendingExpert)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending_expert: %w", err)
	}
	approvalApps, err := s.repo.ListByStatus(ctx, model.StatusPendingApproval)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending_approval: %w", err)
	}
	// PR #312: pending_signature — AZMK application yaradılıb, müştərinin
	// imzası gözlənir. Ekspert artıq action edə bilmir, amma admin
	// dashboard-da bu "gözləyən" müraciətləri görməlidir.
	signApps, err := s.repo.ListByStatus(ctx, model.StatusPendingSignature)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending_signature: %w", err)
	}
	// PR #312: disburse_failed — disburse xətası/nəticəsi naməlum, manual
	// yoxlama tələb olunur — admin bunu dashboard-da görməlidir.
	failedApps, err := s.repo.ListByStatus(ctx, model.StatusDisburseFailed)
	if err != nil {
		return nil, fmt.Errorf("failed to list disburse_failed: %w", err)
	}
	// Birləşdir və tarixə görə sırala (oldest first)
	all := append(expertApps, approvalApps...)
	all = append(all, signApps...)
	all = append(all, failedApps...)
	// Bubble sort by CreatedAt (kiçik list üçün kifayətdir)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].CreatedAt > all[j].CreatedAt {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	return all, nil
}

// ListRejected retrieves all rejected applications (PR #313) — RDC dashboard
// "Imtina olunmus" tabi ucun. Siralama: yenisi evvelde (newest first).
func (s *ApplicationService) ListRejected(ctx context.Context) ([]model.LoanApplication, error) {
	apps, err := s.repo.ListByStatus(ctx, model.StatusRejected)
	if err != nil {
		return nil, fmt.Errorf("failed to list rejected applications: %w", err)
	}
	// ISO string tarixler leksikografik siralanir
	for i := 0; i < len(apps); i++ {
		for j := i + 1; j < len(apps); j++ {
			if apps[i].CreatedAt < apps[j].CreatedAt {
				apps[i], apps[j] = apps[j], apps[i]
			}
		}
	}
	return apps, nil
}

// ListIssued retrieves successfully issued applications (PR #376) — RDC
// dashboard "Uğurla verilənlər" tabi üçün: approved (köhnə/auto-approve yolu)
// + disbursed (AZMK imza axını — pul kartına köçürülüb) statusları.
// Siralama: yenisi evvelde (ListRejected ilə eyni).
func (s *ApplicationService) ListIssued(ctx context.Context) ([]model.LoanApplication, error) {
	approvedApps, err := s.repo.ListByStatus(ctx, model.StatusApproved)
	if err != nil {
		return nil, fmt.Errorf("failed to list approved applications: %w", err)
	}
	disbursedApps, err := s.repo.ListByStatus(ctx, model.StatusDisbursed)
	if err != nil {
		return nil, fmt.Errorf("failed to list disbursed applications: %w", err)
	}
	all := append(approvedApps, disbursedApps...)
	// ISO string tarixler leksikografik siralanir (ListRejected ilə eyni)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].CreatedAt < all[j].CreatedAt {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	return all, nil
}
