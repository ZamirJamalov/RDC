package repository

import (
	"context"
	"database/sql"
	"fmt"
	"rdc-source/internal/model"
)

// ApplicationRepo handles database operations for loan applications.
type ApplicationRepo struct {
	db *sql.DB
}

// NewApplicationRepo creates a new ApplicationRepo with the given database connection.
func NewApplicationRepo(db *sql.DB) *ApplicationRepo {
	return &ApplicationRepo{db: db}
}

// CreateApplication inserts a new loan application and sets the ID on the struct.
func (r *ApplicationRepo) CreateApplication(ctx context.Context, app *model.LoanApplication) error {
	err := r.db.QueryRowContext(ctx, `
                INSERT INTO loan_applications
                        (customer_pin, customer_full_name, amount, term_months, loan_purpose, status, akb_score)
                OUTPUT INSERTED.id
                VALUES (?, ?, ?, ?, ?, ?, ?)`,
		app.CustomerPIN,
		app.CustomerFullName,
		app.Amount,
		app.TermMonths,
		app.LoanPurpose,
		app.Status,
		app.AkbScore,
	).Scan(&app.ID)
	if err != nil {
		return fmt.Errorf("failed to insert application: %w", err)
	}

	return nil
}

// GetApplicationByID fetches a loan application by its primary key.
func (r *ApplicationRepo) GetApplicationByID(ctx context.Context, id int) (*model.LoanApplication, error) {
	var app model.LoanApplication
	var rejectionReasonID sql.NullInt64
	var rejectionReason sql.NullString
	var creditLevel sql.NullString
	var approvedAmount sql.NullFloat64
	var approvedRate sql.NullFloat64
	var akbScore sql.NullInt64

	err := r.db.QueryRowContext(ctx, `
                SELECT id, customer_pin, customer_full_name, amount, term_months, loan_purpose,
                       status, credit_level, approved_amount, approved_rate,
                       rejection_reason_id, rejection_reason, akb_score, created_at, updated_at
                FROM loan_applications WHERE id = ?`, id).Scan(
		&app.ID,
		&app.CustomerPIN,
		&app.CustomerFullName,
		&app.Amount,
		&app.TermMonths,
		&app.LoanPurpose,
		&app.Status,
		&creditLevel,
		&approvedAmount,
		&approvedRate,
		&rejectionReasonID,
		&rejectionReason,
		&akbScore,
		&app.CreatedAt,
		&app.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("application with id %d not found", id)
		}
		return nil, fmt.Errorf("failed to query application: %w", err)
	}

	app.CreditLevel = creditLevel.String
	app.ApprovedAmount = approvedAmount.Float64
	app.ApprovedRate = approvedRate.Float64
	app.RejectionReason = rejectionReason.String
	if akbScore.Valid {
		app.AkbScore = int(akbScore.Int64)
	}
	if rejectionReasonID.Valid {
		rid := int(rejectionReasonID.Int64)
		app.RejectionReasonID = &rid
	}

	return &app, nil
}

// UpdateApplicationStatus updates only the status field of an application.
func (r *ApplicationRepo) UpdateApplicationStatus(ctx context.Context, id int, status string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE loan_applications SET status = ?, updated_at = GETDATE() WHERE id = ?",
		status, id)
	if err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}
	return nil
}

// UpdateApplicationDecision updates the decision-related fields after credit engine processing.
func (r *ApplicationRepo) UpdateApplicationDecision(ctx context.Context, id int,
	status, creditLevel, rejectionReason string, approvedAmount, approvedRate float64) error {

	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET status = ?,
                    credit_level = ?,
                    approved_amount = ?,
                    approved_rate = ?,
                    rejection_reason = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
		status, creditLevel, approvedAmount, approvedRate, rejectionReason, id)
	if err != nil {
		return fmt.Errorf("failed to update application decision: %w", err)
	}
	return nil
}

// SaveCheckResult inserts a check result for an application.
func (r *ApplicationRepo) SaveCheckResult(ctx context.Context, appID int, check *model.ApplicationCheckResult) error {
	_, err := r.db.ExecContext(ctx, `
                INSERT INTO application_checks (application_id, check_type, status, detail, checked_at)
                VALUES (?, ?, ?, ?, ?)`,
		appID, check.CheckType, check.Status, check.Detail, check.CheckedAt)
	if err != nil {
		return fmt.Errorf("failed to save check result: %w", err)
	}
	return nil
}

// GetCheckResults retrieves all check results for an application ordered by ID.
func (r *ApplicationRepo) GetCheckResults(ctx context.Context, appID int) ([]model.ApplicationCheckResult, error) {
	rows, err := r.db.QueryContext(ctx, `
                SELECT check_type, status, detail, checked_at
                FROM application_checks
                WHERE application_id = ?
                ORDER BY id`, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to query check results: %w", err)
	}
	defer rows.Close()

	var results []model.ApplicationCheckResult
	for rows.Next() {
		var cr model.ApplicationCheckResult
		if err := rows.Scan(&cr.CheckType, &cr.Status, &cr.Detail, &cr.CheckedAt); err != nil {
			return nil, fmt.Errorf("failed to scan check result: %w", err)
		}
		results = append(results, cr)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating check results: %w", err)
	}

	return results, nil
}

// GetCreditLevelRate looks up the applicable interest rate for a given credit level, amount, term, and unlock phase.
// unlock_phase uses <= so that phase 2 customers can also access phase 1 ranges.
func (r *ApplicationRepo) GetCreditLevelRate(ctx context.Context, level string, amount float64, termMonths int, unlockPhase int) (float64, error) {
	var rate float64
	err := r.db.QueryRowContext(ctx, `
                SELECT rate FROM credit_levels
                WHERE level_name = ? AND min_amount <= ? AND max_amount >= ? AND term_months = ? AND unlock_phase <= ? AND is_active = 1`,
		level, amount, amount, termMonths, unlockPhase).Scan(&rate)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no rate found for level=%s amount=%.2f term=%d months (unlock_phase=%d)", level, amount, termMonths, unlockPhase)
		}
		return 0, fmt.Errorf("failed to query credit level rate: %w", err)
	}
	return rate, nil
}

// CountApprovedAtLevel counts how many loan applications a customer has had approved at a specific credit level.
// This is used to determine the unlock_phase: 0 = phase 1 (first loan), 1+ = phase 2 (ranges unlocked).
func (r *ApplicationRepo) CountApprovedAtLevel(ctx context.Context, customerPIN string, level string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
                SELECT COUNT(*) FROM loan_applications
                WHERE customer_pin = ? AND credit_level = ? AND status = 'approved'`,
		customerPIN, level).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count approved applications at level: %w", err)
	}
	return count, nil
}

// LevelRange describes a single rate configuration row for a credit level.
type LevelRange struct {
	MinAmount  float64
	MaxAmount  float64
	TermMonths int
	Rate       float64
	Phase      int
}

// GetLevelRanges returns all active rate configurations for a given credit level and unlock phase.
// Used for building descriptive error messages when a requested amount/term has no matching rate.
func (r *ApplicationRepo) GetLevelRanges(ctx context.Context, level string, unlockPhase int) ([]LevelRange, error) {
	rows, err := r.db.QueryContext(ctx, `
                SELECT min_amount, max_amount, term_months, rate, unlock_phase
                FROM credit_levels
                WHERE level_name = ? AND unlock_phase <= ? AND is_active = 1
                ORDER BY unlock_phase, min_amount, term_months`,
		level, unlockPhase)
	if err != nil {
		return nil, fmt.Errorf("failed to query level ranges: %w", err)
	}
	defer rows.Close()

	var ranges []LevelRange
	for rows.Next() {
		var lr LevelRange
		if err := rows.Scan(&lr.MinAmount, &lr.MaxAmount, &lr.TermMonths, &lr.Rate, &lr.Phase); err != nil {
			return nil, fmt.Errorf("failed to scan level range: %w", err)
		}
		ranges = append(ranges, lr)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating level ranges: %w", err)
	}

	return ranges, nil
}

// SaveCreditLevelHistory records a credit level assignment for a customer.
func (r *ApplicationRepo) SaveCreditLevelHistory(ctx context.Context, customerPIN, toLevel string, appID int) error {
	_, err := r.db.ExecContext(ctx, `
                INSERT INTO credit_level_history (customer_pin, to_level, application_id)
                VALUES (?, ?, ?)`,
		customerPIN, toLevel, appID)
	if err != nil {
		return fmt.Errorf("failed to save credit level history: %w", err)
	}
	return nil
}

// HasPendingApplication checks if a customer already has an application that is not yet finalized.
// Non-final statuses are: pending, checking, pending_approval.
// Returns the existing application's ID and status if found, or 0 and empty string if not.
func (r *ApplicationRepo) HasPendingApplication(ctx context.Context, customerPIN string) (int, string, error) {
	var appID int
	var status string
	err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, status FROM loan_applications
                WHERE customer_pin = ? AND status IN ('pending', 'checking', 'pending_approval')
                ORDER BY id DESC`,
		customerPIN).Scan(&appID, &status)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, "", nil
		}
		return 0, "", fmt.Errorf("failed to check pending applications: %w", err)
	}
	return appID, status, nil
}
