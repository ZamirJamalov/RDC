package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// OTPCode represents a stored OTP code record.
type OTPCode struct {
	ID           int
	Phone        string
	CodeHash     string
	Status       string // active, verified, expired, consumed
	Attempts     int
	MaxAttempts  int
	ExpiresAt    time.Time
	ConsumedAt   sql.NullTime
	CreatedAt    time.Time
}

// OTPRepo handles database operations for OTP codes (T-3.6).
type OTPRepo struct {
	db *sql.DB
}

// NewOTPRepo creates a new OTPRepo with the given database connection.
func NewOTPRepo(db *sql.DB) *OTPRepo {
	return &OTPRepo{db: db}
}

// Create inserts a new OTP code record. The codeHash should be a SHA-256 hash
// of the 6-digit code — never store the plaintext code in the database.
func (r *OTPRepo) Create(ctx context.Context, phone, codeHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO otp_codes (phone, code_hash, status, expires_at)
		VALUES (?, ?, 'active', ?)`,
		phone, codeHash, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert OTP code: %w", err)
	}
	return nil
}

// GetActiveByPhone retrieves the most recent active OTP code for a phone number.
// Returns sql.ErrNoRows if no active code exists.
func (r *OTPRepo) GetActiveByPhone(ctx context.Context, phone string) (*OTPCode, error) {
	var code OTPCode
	err := r.db.QueryRowContext(ctx, `
		SELECT TOP 1 id, phone, code_hash, status, attempts, max_attempts,
		       expires_at, consumed_at, created_at
		FROM otp_codes
		WHERE phone = ? AND status = 'active'
		ORDER BY created_at DESC`,
		phone).Scan(
		&code.ID,
		&code.Phone,
		&code.CodeHash,
		&code.Status,
		&code.Attempts,
		&code.MaxAttempts,
		&code.ExpiresAt,
		&code.ConsumedAt,
		&code.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// IncrementAttempts increments the failed verification attempt counter for the
// given OTP code ID. If attempts reach max_attempts, the code is marked as
// 'expired' (locked).
func (r *OTPRepo) IncrementAttempts(ctx context.Context, id int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE otp_codes
		SET attempts = attempts + 1,
		    status = CASE WHEN attempts + 1 >= max_attempts THEN 'expired' ELSE status END
		WHERE id = ?`,
		id)
	if err != nil {
		return fmt.Errorf("failed to increment OTP attempts: %w", err)
	}
	return nil
}

// MarkVerified marks the OTP code as verified (consumed). Sets status to
// 'verified' and consumed_at to the current time.
func (r *OTPRepo) MarkVerified(ctx context.Context, id int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE otp_codes
		SET status = 'verified',
		    consumed_at = GETDATE()
		WHERE id = ?`,
		id)
	if err != nil {
		return fmt.Errorf("failed to mark OTP as verified: %w", err)
	}
	return nil
}

// CountRecentCodes counts how many OTP codes were created for the given phone
// number within the last `window` seconds. Used for rate limiting (max 1 SMS
// per minute per phone).
func (r *OTPRepo) CountRecentCodes(ctx context.Context, phone string, window time.Duration) (int, error) {
	var count int
	since := time.Now().Add(-window)
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_codes
		WHERE phone = ? AND created_at >= ?`,
		phone, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count recent OTP codes: %w", err)
	}
	return count, nil
}

// ExpireOldCodes marks all active OTP codes that have passed their expires_at
// as 'expired'. Should be called periodically (e.g. on each send/verify) to
// keep the table clean.
func (r *OTPRepo) ExpireOldCodes(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE otp_codes
		SET status = 'expired'
		WHERE status = 'active' AND expires_at < GETDATE()`)
	if err != nil {
		return 0, fmt.Errorf("failed to expire old OTP codes: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}
