package repository

import (
	"context"
	"fmt"

	"rdc-source/internal/model"
)

// UpdateApplicationDetails fills in the remaining fields after the expert
// completes the application (customer name, amount, term, card, contacts, etc).
// This is used by the CompleteApplication flow.
func (r *ApplicationRepo) UpdateApplicationDetails(ctx context.Context, id int, app *model.LoanApplication) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET customer_full_name = ?,
		    amount = ?,
		    term_months = ?,
		    loan_purpose = ?,
		    akb_score = ?,
		    contact1_phone = ?,
		    contact2_phone = ?,
		    contact3_phone = ?,
		    actual_address = ?,
		    card_number = ?,
		    status = ?,
		    updated_at = GETDATE()
		WHERE id = ?`,
		app.CustomerFullName,
		app.Amount,
		app.TermMonths,
		app.LoanPurpose,
		app.AkbScore,
		app.Contact1Phone,
		app.Contact2Phone,
		app.Contact3Phone,
		app.ActualAddress,
		app.CardNumber,
		app.Status,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update application details: %w", err)
	}
	return nil
}
