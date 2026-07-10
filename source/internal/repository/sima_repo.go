package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SimaRepo handles database operations for SIMA KYC sessions (T-4.4).
type SimaRepo struct {
	db *sql.DB
}

// NewSimaRepo creates a new SimaRepo.
func NewSimaRepo(db *sql.DB) *SimaRepo {
	return &SimaRepo{db: db}
}

// Create inserts a new SIMA session record.
func (r *SimaRepo) Create(ctx context.Context, appID int, sessionID, fin, url string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sima_sessions (application_id, session_id, fin, status, url, expires_at)
		VALUES (?, ?, ?, 'pending', ?, ?)`,
		appID, sessionID, fin, url, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert SIMA session: %w", err)
	}
	return nil
}

// GetBySessionID retrieves a SIMA session by its session ID.
func (r *SimaRepo) GetBySessionID(ctx context.Context, sessionID string) (*SimaSession, error) {
	var s SimaSession
	var completedAt sql.NullTime
	var detail sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, application_id, session_id, fin, status, detail, url,
		       started_at, completed_at, expires_at, created_at
		FROM sima_sessions WHERE session_id = ?`,
		sessionID).Scan(
		&s.ID, &s.ApplicationID, &s.SessionID, &s.Fin, &s.Status,
		&detail, &s.URL, &s.StartedAt, &completedAt, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	s.Detail = detail.String
	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	return &s, nil
}

// GetByApplicationID retrieves the most recent SIMA session for an application.
func (r *SimaRepo) GetByApplicationID(ctx context.Context, appID int) (*SimaSession, error) {
	var s SimaSession
	var completedAt sql.NullTime
	var detail sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT TOP 1 id, application_id, session_id, fin, status, detail, url,
		       started_at, completed_at, expires_at, created_at
		FROM sima_sessions WHERE application_id = ?
		ORDER BY created_at DESC`,
		appID).Scan(
		&s.ID, &s.ApplicationID, &s.SessionID, &s.Fin, &s.Status,
		&detail, &s.URL, &s.StartedAt, &completedAt, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	s.Detail = detail.String
	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	return &s, nil
}

// UpdateResult updates the status and detail of a SIMA session (called when
// the async callback arrives or polling completes).
func (r *SimaRepo) UpdateResult(ctx context.Context, sessionID, status, detail string) error {
	completedAt := "NULL"
	if status == "success" || status == "failed" || status == "expired" {
		completedAt = "GETDATE()"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE sima_sessions
		SET status = ?, detail = ?, completed_at = `+completedAt+`
		WHERE session_id = ?`,
		status, detail, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update SIMA session: %w", err)
	}
	return nil
}

// SimaSession represents a SIMA KYC session record.
type SimaSession struct {
	ID            int
	ApplicationID int
	SessionID     string
	Fin           string
	Status        string
	Detail        string
	URL           string
	StartedAt     time.Time
	CompletedAt   *time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
}
