package sima

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// MockProvider implements the SIMA Provider interface by returning canned
// responses. Used in dev/test environments where the real SIMA service is
// not available.
//
// InitKyc returns a fake session ID and URL. GetResult always returns
// "success" — tests that need a different status can set the Status field.
type MockProvider struct {
	// Status controls what GetResult returns. Default: "success".
	Status string
}

// NewMockProvider creates a new MockProvider with default status "success".
func NewMockProvider() *MockProvider {
	return &MockProvider{Status: "success"}
}

// InitKyc returns a mock session with a fake URL.
func (p *MockProvider) InitKyc(_ context.Context, appID int, fin string) (*InitResponse, error) {
	sessionID := fmt.Sprintf("SIMA-MOCK-%d-%d", appID, time.Now().Unix())
	slog.Info("mock SIMA KYC initiated",
		"application_id", appID,
		"fin", fin,
		"session_id", sessionID)
	return &InitResponse{
		SessionID: sessionID,
		URL:       fmt.Sprintf("https://mock-sima.example.com/verify/%s", sessionID),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}, nil
}

// GetResult returns a mock result with the configured status.
func (p *MockProvider) GetResult(_ context.Context, sessionID string) (*ResultResponse, error) {
	status := p.Status
	if status == "" {
		status = "success"
	}
	return &ResultResponse{
		SessionID:   sessionID,
		Status:      status,
		Detail:      fmt.Sprintf("mock SIMA result: %s", status),
		CompletedAt: time.Now(),
	}, nil
}

// Name returns "mock".
func (p *MockProvider) Name() string { return "mock" }
