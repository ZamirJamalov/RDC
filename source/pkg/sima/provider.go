package sima

import (
	"context"
	"time"
)

// Provider defines the interface for SIMA KYC (Know Your Customer) operations.
// SIMA is Azerbaijan's digital identity verification service — customers
// verify their identity via a mobile app (face recognition + ID document).
//
// Flow:
//  1. RDC calls InitKyc(applicationID) → SIMA returns a session URL
//  2. Customer opens the URL on their phone and completes verification
//  3. SIMA notifies LW (async callback), LW forwards to RDC's
//     POST /api/rdc/callback/sima-result endpoint
//  4. RDC's callback handler persists the result and updates the application
//
// The Provider is responsible for:
//   - Initiating the KYC session (InitKyc)
//   - Querying the result synchronously (GetResult) — used as a polling
//     fallback when the async callback is delayed or lost
//
// The Provider does NOT handle callbacks — that's the LWCallbackHandler's job.
type Provider interface {
	// InitKyc starts a new SIMA KYC session for the given application.
	// Returns the session ID and a URL the customer should open on their phone.
	InitKyc(ctx context.Context, appID int, fin string) (*InitResponse, error)

	// GetResult fetches the KYC result for a session. Used as a polling
	// fallback when the async callback doesn't arrive within a reasonable time.
	// Returns the current status ("pending", "success", "failed", "expired").
	GetResult(ctx context.Context, sessionID string) (*ResultResponse, error)

	// Name returns a human-readable identifier ("mock", "sima-http").
	Name() string
}

// InitResponse is returned by InitKyc.
type InitResponse struct {
	SessionID  string    `json:"session_id"`
	URL        string    `json:"url"`         // customer opens this on their phone
	ExpiresAt  time.Time `json:"expires_at"`  // session expires if not completed by this time
}

// ResultResponse is returned by GetResult.
type ResultResponse struct {
	SessionID   string    `json:"session_id"`
	Status      string    `json:"status"`     // pending, success, failed, expired
	Detail      string    `json:"detail,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}
