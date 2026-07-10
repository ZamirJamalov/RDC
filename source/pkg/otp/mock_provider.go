package otp

import (
	"context"
	"log/slog"
)

// MockProvider implements the OTP Provider interface by logging the code
// instead of sending an actual SMS. Used in dev/test environments where
// you don't want to send real SMS messages (which cost money and require
// a real phone number).
//
// The code is logged at INFO level so it appears in the server logs and
// can be copied from there for testing. Example log line:
//
//	INFO mock OTP code phone=+994501234567 code=123456
type MockProvider struct{}

// NewMockProvider creates a new MockProvider.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// Send logs the OTP code at INFO level. Always succeeds (never returns an
// error) — there's no real SMS gateway to fail.
func (p *MockProvider) Send(_ context.Context, phone, code string) error {
	slog.Info("mock OTP code",
		"phone", phone,
		"code", code,
		"provider", "mock")
	return nil
}

// Name returns "mock".
func (p *MockProvider) Name() string {
	return "mock"
}
