package repository

import (
        "context"
        "database/sql"
        "fmt"

        "github.com/google/uuid"
        "github.com/microsoft/go-mssqldb"

        "rdc-source/internal/model"
)

// ListByStatus retrieves all applications with the given status, ordered by
// oldest first (FIFO — experts should review the oldest applications first).
// Used by the expert queue endpoint to list pending_approval applications.
//
// PR #94: includes discount_code so the expert dashboard can show whether
// the customer entered a referral code (transparency for the decision).
func (r *ApplicationRepo) ListByStatus(ctx context.Context, status string) ([]model.LoanApplication, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT id, public_id, customer_pin, customer_full_name, amount, term_months,
                       loan_purpose, status, credit_level, approved_amount, approved_rate,
                       discount_code,
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
                var creditLevel, loanPurpose, discountCode sql.NullString
                var approvedAmount, approvedRate sql.NullFloat64
                if err := rows.Scan(
                        &app.ID, &app.PublicID, &app.CustomerPIN, &app.CustomerFullName, &app.Amount,
                        &app.TermMonths, &loanPurpose, &app.Status, &creditLevel,
                        &approvedAmount, &approvedRate, &discountCode,
                        &app.CreatedAt, &app.UpdatedAt,
                ); err != nil {
                        return nil, fmt.Errorf("failed to scan application: %w", err)
                }
                app.LoanPurpose = loanPurpose.String
                app.CreditLevel = creditLevel.String
                app.ApprovedAmount = approvedAmount.Float64
                app.ApprovedRate = approvedRate.Float64
                app.DiscountCode = discountCode.String
                apps = append(apps, app)
        }
        if err = rows.Err(); err != nil {
                return nil, fmt.Errorf("error iterating applications: %w", err)
        }
        return apps, nil
}

// GetApplicationByPublicID fetches a loan application by its UUID public_id.
// PR #191: xarici API və UI public_id UUID istifadə edir.
// PR #192: UUID string mssql.UniqueIdentifier-a çevrilir (string → UNIQUEIDENTIFIER conversion xətası fix).
func (r *ApplicationRepo) GetApplicationByPublicID(ctx context.Context, publicID string) (*model.LoanApplication, error) {
        // Validate and parse the UUID string
        parsed, err := uuid.Parse(publicID)
        if err != nil {
                return nil, fmt.Errorf("invalid public_id format (not a valid UUID): %w", err)
        }

        // Convert to mssql.UniqueIdentifier for proper SQL Server comparison
        var mssqlUUID mssql.UniqueIdentifier
        copy(mssqlUUID[:], parsed[:])

        // Reuse GetApplicationByID by first looking up the INT id
        var id int
        err = r.db.QueryRowContext(ctx, `SELECT id FROM loan_applications WHERE public_id = ?`, mssqlUUID).Scan(&id)
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, nil
                }
                return nil, fmt.Errorf("failed to lookup application by public_id: %w", err)
        }
        return r.GetApplicationByID(ctx, id)
}
