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
)

// MyGovService handles MyGov data access operations (T-4.10).
type MyGovService struct {
	provider mygov.Provider
	repo     *repository.MyGovRepo
}

// NewMyGovService creates a new MyGovService.
func NewMyGovService(provider mygov.Provider, repo *repository.MyGovRepo) *MyGovService {
	return &MyGovService{provider: provider, repo: repo}
}

// GenerateLink creates a MyGov permission link for the customer.
// The customer opens this URL and grants RDC access to their official data.
func (s *MyGovService) GenerateLink(ctx context.Context, appID int, customerPIN string) (*model.MyGovPermissionResponse, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("application_id must be positive")
	}
	if customerPIN == "" {
		return nil, fmt.Errorf("customer_pin is required")
	}

	// Call MyGov provider
	link, err := s.provider.GeneratePermissionLink(ctx, customerPIN)
	if err != nil {
		return nil, fmt.Errorf("MyGov GeneratePermissionLink failed: %w", err)
	}

	// Store in DB
	if err := s.repo.Create(ctx, appID, customerPIN, link.Token, link.URL, link.ExpiresAt); err != nil {
		return nil, fmt.Errorf("failed to store MyGov permission: %w", err)
	}

	slog.Info("MyGov permission link created",
		"application_id", appID,
		"customer_pin", customerPIN,
		"provider", s.provider.Name())

	return &model.MyGovPermissionResponse{
		ApplicationID: appID,
		URL:           link.URL,
		ExpiresAt:     link.ExpiresAt.Format(time.RFC3339),
	}, nil
}

// FetchData retrieves the customer's authorized data from MyGov and stores it.
// Called after the customer grants permission (via callback or polling).
func (s *MyGovService) FetchData(ctx context.Context, appID int) error {
	// Get the permission record
	perm, err := s.repo.GetByApplicationID(ctx, appID)
	if err != nil {
		return fmt.Errorf("failed to get MyGov permission: %w", err)
	}

	if perm.PermissionToken == "" {
		return fmt.Errorf("no permission token for application %d", appID)
	}

	// Fetch data from MyGov
	data, err := s.provider.FetchAuthorizedData(ctx, perm.PermissionToken)
	if err != nil {
		return fmt.Errorf("MyGov FetchAuthorizedData failed: %w", err)
	}

	// Serialize and store
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

// GetIncome retrieves the official income for an application (from stored data).
// Returns 0 if no data has been fetched yet.
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
