package service

import (
	"context"
	"fmt"
	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// MockLmsService handles mock LMS operations.
type MockLmsService struct {
	repo *repository.MockLmsRepo
}

// NewMockLmsService creates a new MockLmsService.
func NewMockLmsService(repo *repository.MockLmsRepo) *MockLmsService {
	return &MockLmsService{repo: repo}
}

// Setup replaces all existing mock loans for a customer with the new set.
// An empty loans array is valid — it represents a new customer with no loan history.
func (s *MockLmsService) Setup(ctx context.Context, req *model.MockLmsSetupRequest) error {
	if req.CustomerPIN == "" {
		return fmt.Errorf("customer_pin is required")
	}
	return s.repo.SetupLoans(ctx, req)
}

// Query retrieves the aggregated loan data for a customer.
func (s *MockLmsService) Query(ctx context.Context, customerPIN string) (*model.MockLMSCustomerLoans, error) {
	if customerPIN == "" {
		return nil, fmt.Errorf("customer_pin query parameter is required")
	}

	loans, err := s.repo.GetCustomerLoans(ctx, customerPIN)
	if err != nil {
		return nil, err
	}

	result := &model.MockLMSCustomerLoans{
		CustomerPIN:      customerPIN,
		HasExistingLoans: len(loans) > 0,
		LoanCount:        len(loans),
		Loans:            loans,
	}

	return result, nil
}
