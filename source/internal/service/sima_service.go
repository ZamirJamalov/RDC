package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
	"rdc-source/pkg/sima"
)

// SimaService handles SIMA KYC operations (T-4.5).
//
// Flow:
//  1. InitKyc: call SIMA provider → store session in DB → return URL
//  2. HandleCallback: update session status from LW callback
//  3. (Optional) PollResult: fallback if callback is delayed
type SimaService struct {
	provider sima.Provider
	repo     *repository.SimaRepo
}

// NewSimaService creates a new SimaService.
func NewSimaService(provider sima.Provider, repo *repository.SimaRepo) *SimaService {
	return &SimaService{provider: provider, repo: repo}
}

// InitKyc starts a SIMA KYC session for the given application.
// Returns the session URL that the customer should open on their phone.
func (s *SimaService) InitKyc(ctx context.Context, appID int, fin string) (*model.SimaInitResponse, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("application_id must be positive")
	}
	if fin == "" {
		return nil, fmt.Errorf("fin is required")
	}

	// Call SIMA provider
	resp, err := s.provider.InitKyc(ctx, appID, fin)
	if err != nil {
		return nil, fmt.Errorf("SIMA InitKyc failed: %w", err)
	}

	// Store session in DB
	if err := s.repo.Create(ctx, appID, resp.SessionID, fin, resp.URL, resp.ExpiresAt); err != nil {
		return nil, fmt.Errorf("failed to store SIMA session: %w", err)
	}

	slog.Info("SIMA KYC session created",
		"application_id", appID,
		"session_id", resp.SessionID,
		"provider", s.provider.Name())

	return &model.SimaInitResponse{
		ApplicationID: appID,
		SessionID:     resp.SessionID,
		URL:           resp.URL,
		ExpiresAt:     resp.ExpiresAt.Format(time.RFC3339),
	}, nil
}

// HandleCallback processes the async SIMA result callback from LW.
// Updates the session status in the DB and logs the result.
func (s *SimaService) HandleCallback(ctx context.Context, appID int, sessionID, status, detail string) error {
	if err := s.repo.UpdateResult(ctx, sessionID, status, detail); err != nil {
		return fmt.Errorf("failed to update SIMA session: %w", err)
	}

	slog.Info("SIMA KYC callback processed",
		"application_id", appID,
		"session_id", sessionID,
		"status", status,
		"detail", detail)

	return nil
}

// GetStatus retrieves the current SIMA session status for an application.
// Used by the credit engine to check if KYC was completed.
func (s *SimaService) GetStatus(ctx context.Context, appID int) (string, error) {
	session, err := s.repo.GetByApplicationID(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("failed to get SIMA session: %w", err)
	}
	return session.Status, nil
}
