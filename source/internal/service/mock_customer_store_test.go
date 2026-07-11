package service

import (
	"context"

	"rdc-source/internal/model"
)

// mockCustomerStore is a test-only implementation of CustomerStore.
// It records calls so tests can assert behavior.
type mockCustomerStore struct {
	// Configurable return values
	getOrCreateErr error
	linkErr        error

	// Recording of calls
	createdCustomers []model.Customer
	linkedApps       []struct {
		AppID      int
		CustomerID int
	}
}

func (m *mockCustomerStore) GetOrCreate(_ context.Context, c *model.Customer) error {
	m.createdCustomers = append(m.createdCustomers, *c)
	if m.getOrCreateErr != nil {
		return m.getOrCreateErr
	}
	// Simulate auto-increment ID assignment
	c.ID = len(m.createdCustomers)
	return nil
}

func (m *mockCustomerStore) LinkApplication(_ context.Context, appID, customerID int) error {
	m.linkedApps = append(m.linkedApps, struct {
		AppID      int
		CustomerID int
	}{AppID: appID, CustomerID: customerID})
	return m.linkErr
}

func (m *mockCustomerStore) UpdatePhone(_ context.Context, _ int, _ string) error {
	return nil
}

// newMockCustomerStore returns a mockCustomerStore with safe defaults.
func newMockCustomerStore() *mockCustomerStore {
	return &mockCustomerStore{}
}
