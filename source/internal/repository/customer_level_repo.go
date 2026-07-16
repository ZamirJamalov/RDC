package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// GetCustomerCurrentLevel returns the customer's current credit level based on
// their most recent LW-confirmed approved application.
//
// "LW-confirmed" means the application has a lw_loan_events record with
// event_status='transfer_completed'. If RDC approved but LW hasn't confirmed,
// the application is not counted — the customer stays at their previous level.
//
// Returns empty string if no LW-confirmed approved application exists.
func (r *ApplicationRepo) GetCustomerCurrentLevel(ctx context.Context, customerPIN string) (string, error) {
	// Get last approved application
	var appID int
	var level sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT TOP 1 id, credit_level
		FROM loan_applications
		WHERE customer_pin = ? AND status = 'approved'
		ORDER BY updated_at DESC`, customerPIN).Scan(&appID, &level)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get customer current level: %w", err)
	}

	// Check if LW confirmed this application (transfer_completed event exists)
	var count int
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lw_loan_events
		WHERE application_id = ? AND event_status = 'transfer_completed'`, appID).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to check LW confirmation: %w", err)
	}

	// If LW hasn't confirmed → treat as no approved application
	if count == 0 {
		return "", nil
	}

	return level.String, nil
}
