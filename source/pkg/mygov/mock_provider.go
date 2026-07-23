package mygov

import (
        "context"
        "fmt"
        "log/slog"
        "time"
)

// MockProvider implements the MyGov Provider interface by returning canned
// responses. Used in dev/test environments.
type MockProvider struct{}

// NewMockProvider creates a new MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// GeneratePermissionLink returns a mock permission URL.
func (p *MockProvider) GeneratePermissionLink(_ context.Context, fin string) (*PermissionLink, error) {
        token := fmt.Sprintf("MOCK-MYGOV-%s-%d", fin, time.Now().Unix())
        slog.Info("mock MyGov permission link generated", "fin", fin, "token", token)
        return &PermissionLink{
                Token:     token,
                URL:       fmt.Sprintf("https://mock-mygov.example.com/permit/%s", token),
                ExpiresAt: time.Now().Add(30 * time.Minute),
        }, nil
}

// FetchAuthorizedData returns mock official data.
// PR #64: includes sample WorkHistory and pension fields.
func (p *MockProvider) FetchAuthorizedData(_ context.Context, token string) (*AuthorizedData, error) {
        slog.Info("mock MyGov data fetched", "token", token)
        now := time.Now()
        // Simulate a customer who started their current job 8 months ago
        // (passes the 6-month tenure rule in PR #65).
        currentJobStart := now.AddDate(0, -8, 0)
        return &AuthorizedData{
                Fin:            "MOCK",
                FullName:       "Mock Customer",
                OfficialIncome: 1500.0,
                EmployerName:   "Mock Employer LLC",
                Address:        "Mock Address, Baku",
                FetchedAt:      now,
                WorkHistory: []WorkPlace{
                        {
                                EmployerName: "Mock Employer LLC",
                                StartDate:    currentJobStart,
                                EndDate:      nil, // currently employed
                                Position:     "Software Engineer",
                        },
                },
                DisabilityGroup: 0, // no disability
                IsPensioner:     false,
                PensionType:     "",
        }, nil
}

// Name returns "mock".
func (p *MockProvider) Name() string { return "mock" }
