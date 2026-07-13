package service

import (
        "context"
        "encoding/json"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
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
}

// NewMyGovService creates a new MyGovService.
func NewMyGovService(provider mygov.Provider, repo *repository.MyGovRepo, appRepo *repository.ApplicationRepo, smsProvider otp.Provider, clientID, redirectURI, webURL string) *MyGovService {
        return &MyGovService{
                provider:    provider,
                repo:        repo,
                appRepo:     appRepo,
                smsProvider: smsProvider,
                clientID:    clientID,
                redirectURI: redirectURI,
                webURL:      webURL,
        }
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

        // 4. Build web URL (sent via SMS — clickable on all phones)
        webURL := mygov.BuildWebURL(s.webURL, s.clientID, nonce, state, s.redirectURI)

        // 5. Set expiry (5 minutes per MyGov spec)
        expiresAt := time.Now().Add(5 * time.Minute)

        // 6. Store in DB (deeplink for reference)
        if err := s.repo.CreateWithDeeplink(ctx, appID, customerPIN, nonce, state, deeplink, expiresAt); err != nil {
                return nil, fmt.Errorf("failed to store MyGov permission: %w", err)
        }

        // 7. Send SMS with web URL (NOT deeplink — mygov:// can't be clicked in SMS)
        mygovMessage := fmt.Sprintf("Icaze tesdiqlemek ucun linki acin: %s", webURL)
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
