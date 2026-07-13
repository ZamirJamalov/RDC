package otp

import "context"

// Provider defines the interface for sending SMS messages.
// Implementations:
//   - MockProvider (dev/test): logs the message instead of sending SMS
//   - HTTPProvider (production): calls a real SMS gateway
//   - DynamicSMSProvider: reads active provider config from DB
//
// The Send method accepts a fully-formatted message string — it does
// NOT add any prefixes or suffixes. The caller (OTPService, MyGovService)
// is responsible for constructing the message text.
type Provider interface {
        // Send delivers the given message to the given phone number via SMS.
        Send(ctx context.Context, phone, message string) error

        // Name returns a human-readable identifier for the provider ("mock",
        // "http", "dynamic").
        Name() string
}
