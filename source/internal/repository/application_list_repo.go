package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/internal/model"
)

// ListByStatus retrieves all applications with the given status, ordered by
// oldest first (FIFO — experts should review the oldest applications first).
// Used by the expert queue endpoint to list pending_approval applications.
func (r *ApplicationRepo) ListByStatus(ctx context.Context, status string) ([]model.LoanApplication, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT id, customer_pin, customer_full_name, amount, term_months,
                       loan_purpose, status, credit_level, approved_amount, approved_rate,
                       created_at, updated_at
                FROM loan_applications
                WHERE status = ?
                ORDER BY created_at ASC`, status)
        if err != nil {
                return nil, fmt.Errorf("failed to list applications by status: %w", err)
        }
        defer rows.Close()

        var apps []model.LoanApplication
        for rows.Next() {
                var app model.LoanApplication
                var creditLevel, loanPurpose sql.NullString
                var approvedAmount, approvedRate sql.NullFloat64
                if err := rows.Scan(
                        &app.ID, &app.CustomerPIN, &app.CustomerFullName, &app.Amount,
                        &app.TermMonths, &loanPurpose, &app.Status, &creditLevel,
                        &approvedAmount, &approvedRate, &app.CreatedAt, &app.UpdatedAt,
                ); err != nil {
                        return nil, fmt.Errorf("failed to scan application: %w", err)
                }
                app.LoanPurpose = loanPurpose.String
                app.CreditLevel = creditLevel.String
                app.ApprovedAmount = approvedAmount.Float64
                app.ApprovedRate = approvedRate.Float64
                apps = append(apps, app)
        }
        return apps, nil
}
