package lms

import (
	"context"
	"fmt"
	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// Provider defines the interface for fetching customer loan data from an LMS.
type Provider interface {
	GetCustomerLoans(ctx context.Context, pin string) (*model.MockLMSCustomerLoans, error)
}

// MockLMSProvider fetches loan data from the mock_lms_loans table via the repository.
type MockLMSProvider struct {
	repo *repository.MockLmsRepo
}

// NewMockLMSProvider creates a new MockLMSProvider backed by the given repository.
func NewMockLMSProvider(repo *repository.MockLmsRepo) *MockLMSProvider {
	return &MockLMSProvider{repo: repo}
}

// GetCustomerLoans retrieves all loans for a customer and aggregates them into a response.
func (p *MockLMSProvider) GetCustomerLoans(ctx context.Context, pin string) (*model.MockLMSCustomerLoans, error) {
	loans, err := p.repo.GetCustomerLoans(ctx, pin)
	if err != nil {
		return nil, fmt.Errorf("lms provider: failed to get customer loans: %w", err)
	}

	result := &model.MockLMSCustomerLoans{
		CustomerPIN:      pin,
		HasExistingLoans: len(loans) > 0,
		LoanCount:        len(loans),
		Loans:            loans,
	}

	return result, nil
}