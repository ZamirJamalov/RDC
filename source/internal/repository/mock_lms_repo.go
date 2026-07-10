package repository

import (
	"context"
	"database/sql"
	"fmt"
	"rdc-source/internal/model"
)

// MockLmsRepo handles database operations for mock LMS loan data.
type MockLmsRepo struct {
	db *sql.DB
}

// NewMockLmsRepo creates a new MockLmsRepo with the given database connection.
func NewMockLmsRepo(db *sql.DB) *MockLmsRepo {
	return &MockLmsRepo{db: db}
}

// SetupLoans replaces all existing loans for a customer with the new set.
func (r *MockLmsRepo) SetupLoans(ctx context.Context, req *model.MockLmsSetupRequest) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing loans for this customer
	_, err = tx.ExecContext(ctx, "DELETE FROM mock_lms_loans WHERE customer_pin = ?", req.CustomerPIN)
	if err != nil {
		return fmt.Errorf("failed to delete existing loans: %w", err)
	}

	// Insert new loans
	for _, loan := range req.Loans {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO mock_lms_loans
				(customer_pin, scenario_name, lms_loan_id, loan_type, amount, term_months,
				 start_date, end_date, status, remaining_amount, was_on_time, early_completion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.CustomerPIN,
			req.ScenarioName,
			loan.LmsLoanID,
			loan.LoanType,
			loan.Amount,
			loan.TermMonths,
			loan.StartDate,
			loan.EndDate,
			loan.Status,
			loan.RemainingAmount,
			loan.WasOnTime,
			loan.EarlyCompletion,
		)
		if err != nil {
			return fmt.Errorf("failed to insert loan %s: %w", loan.LmsLoanID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetCustomerLoans retrieves all mock LMS loans for a given customer PIN.
func (r *MockLmsRepo) GetCustomerLoans(ctx context.Context, customerPIN string) ([]model.MockLmsLoanRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, customer_pin, scenario_name, lms_loan_id, loan_type, amount, term_months,
		       start_date, end_date, status, remaining_amount, was_on_time, early_completion
		FROM mock_lms_loans
		WHERE customer_pin = ?`, customerPIN)
	if err != nil {
		return nil, fmt.Errorf("failed to query customer loans: %w", err)
	}
	defer rows.Close()

	var loans []model.MockLmsLoanRow
	for rows.Next() {
		var loan model.MockLmsLoanRow
		err := rows.Scan(
			&loan.ID,
			&loan.CustomerPIN,
			&loan.ScenarioName,
			&loan.LmsLoanID,
			&loan.LoanType,
			&loan.Amount,
			&loan.TermMonths,
			&loan.StartDate,
			&loan.EndDate,
			&loan.Status,
			&loan.RemainingAmount,
			&loan.WasOnTime,
			&loan.EarlyCompletion,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan loan row: %w", err)
		}
		loans = append(loans, loan)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating loan rows: %w", err)
	}

	return loans, nil
}