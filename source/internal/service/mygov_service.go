package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/azmk"
	"rdc-source/pkg/mygov"
	"rdc-source/pkg/otp"
)

// MyGovService handles MyGov data access operations (T-4.10).
type MyGovService struct {
	provider    mygov.Provider
	repo        *repository.MyGovRepo
	appRepo     *repository.ApplicationRepo
	smsProvider otp.Provider
	clientID    string
	redirectURI string
	webURL      string
	// PR #239: AZMK CustomerDataProvider — GetEmployeeInfoByPin üçün
	customerDataProvider azmk.CustomerDataProvider
	// PR #242: cutoff nəticələri — EMPLOYMENT_TENURE, DISABILITY_GROUP1
	cutoffRepo *repository.CutoffResultRepo
	// PR #279: EMPLOYMENT_TENURE minimum staj (ay, default: 6)
	employmentTenureMinMonths int
	// PR #375: employment/pension verify cache — service_cache_config-dakı
	// cache_days ərzində service_audit_logs-dan son uğurlu response oxunur
	cacheLookup serviceCacheLookup
}

// serviceCacheLookup is the minimal cache interface MyGovService needs (PR #375).
// *repository.ServiceCacheRepo bu interface-i satisfies edir (PR #205 mexanizmi
// üzərindən — cache mənbəyi service_audit_logs cədvəlidir).
type serviceCacheLookup interface {
	GetCacheDays(ctx context.Context, serviceName string) (int, error)
	GetCachedResponse(ctx context.Context, serviceName, customerPIN string, cacheDays int) (string, bool, error)
	// PR #379: cache HIT audit cədvəlində görünür (method='CACHE' marker row)
	LogCacheHit(ctx context.Context, appID *int, serviceName, customerPIN, responseBody string) error
}

var _ serviceCacheLookup = (*repository.ServiceCacheRepo)(nil)

// NewMyGovService creates a new MyGovService.
func NewMyGovService(provider mygov.Provider, repo *repository.MyGovRepo, appRepo *repository.ApplicationRepo, smsProvider otp.Provider, clientID, redirectURI, webURL string) *MyGovService {
	return &MyGovService{
		provider:                  provider,
		repo:                      repo,
		appRepo:                   appRepo,
		smsProvider:               smsProvider,
		clientID:                  clientID,
		redirectURI:               redirectURI,
		webURL:                    webURL,
		employmentTenureMinMonths: 6, // PR #279: default
	}
}

// SetCustomerDataProvider injects the AZMK CustomerDataProvider (PR #239).
// GetEmployeeInfoByPin AZMK CustomerDataService-ə sorğu göndərir.
func (s *MyGovService) SetCustomerDataProvider(provider azmk.CustomerDataProvider) {
	s.customerDataProvider = provider
}

// SetCutoffRepo injects the cutoff results repo (PR #242).
// VerifyEmployment/VerifyPension nəticələri cutoff_results cədvəlinə yazılır.
func (s *MyGovService) SetCutoffRepo(repo *repository.CutoffResultRepo) {
	s.cutoffRepo = repo
}

// SetEmploymentTenureMinMonths sets the EMPLOYMENT_TENURE threshold (PR #279).
// Default: 6 (config EMPLOYMENT_TENURE_MIN_MONTHS).
func (s *MyGovService) SetEmploymentTenureMinMonths(months int) {
	if months > 0 {
		s.employmentTenureMinMonths = months
	}
}

// SetServiceCacheLookup injects the service cache (PR #375): employment/pension
// verify cache_days qədər (hal-hazırda 3 gün) cache ilə işləyir — dashboard-dakı
// "Yoxla" düymələri hər klikdə AZMK-nı fiziki çağırmır. nil = cache deaktiv.
func (s *MyGovService) SetServiceCacheLookup(l serviceCacheLookup) {
	s.cacheLookup = l
}

// cachedResponse returns the cached response body for a service+PIN if caching
// is enabled (service_cache_config.cache_days > 0) and a fresh successful entry
// exists in service_audit_logs. PR #375 — ApplicationService.GetCachedServiceResponse
// ilə eyni məntiq (PR #205). PR #379: HIT olanda service_audit_logs-a method='CACHE'
// marker row yazılır ki, cədvəldə cache-dən oxunduğu görünsün.
func (s *MyGovService) cachedResponse(ctx context.Context, appID int, serviceName, customerPIN string) (string, bool) {
	if s.cacheLookup == nil {
		return "", false // cache deaktiv
	}
	days, err := s.cacheLookup.GetCacheDays(ctx, serviceName)
	if err != nil {
		slog.Warn("mygov verify cache: failed to get cache_days", "service", serviceName, "error", err)
		return "", false
	}
	if days <= 0 {
		return "", false // cache_days=0 → birbaşa servisə müraciət
	}
	body, found, err := s.cacheLookup.GetCachedResponse(ctx, serviceName, customerPIN, days)
	if err != nil {
		slog.Warn("mygov verify cache: failed to get cached response", "service", serviceName, "error", err)
		return "", false
	}
	if found {
		slog.Info("mygov verify cache: HIT", "service", serviceName, "customer_pin", customerPIN, "cache_days", days)
		// PR #379: audit cədvəlində cache istifadəsini görünür et
		if lerr := s.cacheLookup.LogCacheHit(ctx, &appID, serviceName, customerPIN, body); lerr != nil {
			slog.Warn("mygov verify cache: failed to log cache hit", "service", serviceName, "error", lerr)
		}
	}
	return body, found
}

// GenerateLink creates a MyGov consent deeplink and sends it via SMS
// to the customer's OTP-verified phone number.
func (s *MyGovService) GenerateLink(ctx context.Context, appID int, customerPIN string) (*model.MyGovPermissionResponse, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("application_id must be positive")
	}
	if customerPIN == "" {
		return nil, fmt.Errorf("customer_pin is required")
	}

	// 1. Get application to retrieve customer_phone
	app, err := s.appRepo.GetApplicationByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app.CustomerPhone == "" {
		return nil, fmt.Errorf("customer_phone not found — application has no OTP-verified phone")
	}

	// 2. Generate nonce and state (secure random)
	nonce, err := mygov.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	state, err := mygov.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// 3. Build deeplink (stored in DB for reference)
	deeplink := mygov.BuildDeeplink(s.clientID, nonce, state, s.redirectURI)

	// 5. Set expiry (5 minutes per MyGov spec)
	expiresAt := time.Now().Add(5 * time.Minute)

	// 6. Store in DB (deeplink for reference)
	if err := s.repo.CreateWithDeeplink(ctx, appID, customerPIN, nonce, state, deeplink, expiresAt); err != nil {
		return nil, fmt.Errorf("failed to store MyGov permission: %w", err)
	}

	// 7. Send SMS with static web URL (no query params)
	mygovMessage := fmt.Sprintf("Icazeni tesdiqlemek ucun linki acin: %s", s.webURL)
	if err := s.smsProvider.Send(ctx, app.CustomerPhone, mygovMessage); err != nil {
		slog.Error("failed to send MyGov SMS",
			"application_id", appID,
			"phone", app.CustomerPhone,
			"error", err)
		return nil, fmt.Errorf("failed to send SMS: %w", err)
	}

	slog.Info("MyGov deeplink generated and SMS sent",
		"application_id", appID,
		"customer_pin", customerPIN,
		"phone", app.CustomerPhone,
		"expires_at", expiresAt.Format(time.RFC3339))

	return &model.MyGovPermissionResponse{
		ApplicationID: appID,
		URL:           deeplink,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
	}, nil
}

// FetchData retrieves the customer's authorized data from MyGov and stores it.
func (s *MyGovService) FetchData(ctx context.Context, appID int) error {
	perm, err := s.repo.GetByApplicationID(ctx, appID)
	if err != nil {
		return fmt.Errorf("failed to get MyGov permission: %w", err)
	}
	if perm.PermissionToken == "" {
		return fmt.Errorf("no permission token for application %d", appID)
	}
	data, err := s.provider.FetchAuthorizedData(ctx, perm.PermissionToken)
	if err != nil {
		return fmt.Errorf("MyGov FetchAuthorizedData failed: %w", err)
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal MyGov data: %w", err)
	}
	if err := s.repo.UpdateData(ctx, appID, string(dataJSON)); err != nil {
		return fmt.Errorf("failed to store MyGov data: %w", err)
	}
	slog.Info("MyGov data fetched and stored",
		"application_id", appID,
		"customer_pin", perm.CustomerPIN,
		"official_income", data.OfficialIncome)
	return nil
}

// GetIncome retrieves the official income for an application.
func (s *MyGovService) GetIncome(ctx context.Context, appID int) (float64, error) {
	perm, err := s.repo.GetByApplicationID(ctx, appID)
	if err != nil {
		return 0, fmt.Errorf("failed to get MyGov permission: %w", err)
	}
	if perm.DataJSON == "" {
		return 0, nil
	}
	var data mygov.AuthorizedData
	if err := json.Unmarshal([]byte(perm.DataJSON), &data); err != nil {
		return 0, fmt.Errorf("failed to parse MyGov data: %w", err)
	}
	return data.OfficialIncome, nil
}
