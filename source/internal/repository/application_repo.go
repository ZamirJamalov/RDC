package repository

import (
        "context"
        "database/sql"
        "fmt"
        "strings"
        "time"

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
                        (customer_pin, customer_serial, customer_full_name, amount, term_months, loan_purpose, status, akb_score,
                         contact1_phone, contact2_phone, contact3_phone, actual_address, card_number, customer_phone)
                OUTPUT INSERTED.id
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
                app.CustomerPIN,
                app.CustomerSerial,
                app.CustomerFullName,
                app.Amount,
                app.TermMonths,
                app.LoanPurpose,
                app.Status,
                app.AkbScore,
                app.Contact1Phone,
                app.Contact2Phone,
                app.Contact3Phone,
                app.ActualAddress,
                app.CardNumber,
                app.CustomerPhone,
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
        var rejectionReason, creditLevel sql.NullString
        var approvedAmount, approvedRate, totalAmount sql.NullFloat64
        var akbScore sql.NullInt64
        var officialIncome sql.NullFloat64
        var contact1, contact2, contact3, contact1Rel, contact2Rel, contact3Rel, address, customerPhone, customerSerial sql.NullString
        var customerConfirmedAt sql.NullString

        err := r.db.QueryRowContext(ctx, `
                SELECT id, customer_pin, customer_full_name, amount, term_months, loan_purpose,
                       status, credit_level, approved_amount, approved_rate, total_amount,
                       rejection_reason_id, rejection_reason, akb_score,
                       official_income, contact1_phone, contact2_phone, contact3_phone, actual_address,
                       contact1_relation, contact2_relation, contact3_relation,
                       card_number, customer_phone, customer_serial,
                       customer_confirmed_at, card_ownership_confirmed,
                       created_at, updated_at
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
                &totalAmount,
                &rejectionReasonID,
                &rejectionReason,
                &akbScore,
                &officialIncome,
                &contact1,
                &contact2,
                &contact3,
                &address,
                &contact1Rel,
                &contact2Rel,
                &contact3Rel,
                &app.CardNumber,
                &customerPhone,
                &customerSerial,
                &customerConfirmedAt,
                &app.CardOwnershipConfirmed,
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
        app.TotalAmount = totalAmount.Float64
        app.RejectionReason = rejectionReason.String
        app.OfficialIncome = officialIncome.Float64
        app.Contact1Phone = contact1.String
        app.Contact2Phone = contact2.String
        app.Contact3Phone = contact3.String
        app.Contact1Relation = contact1Rel.String
        app.Contact2Relation = contact2Rel.String
        app.Contact3Relation = contact3Rel.String
        app.ActualAddress = address.String
        app.CustomerPhone = customerPhone.String
        app.CustomerSerial = customerSerial.String
        app.CustomerConfirmedAt = customerConfirmedAt.String
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
        status, creditLevel, rejectionReason string, approvedAmount, approvedRate, totalAmount float64) error {

        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET status = ?,
                    credit_level = ?,
                    approved_amount = ?,
                    approved_rate = ?,
                    total_amount = ?,
                    rejection_reason = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                status, creditLevel, approvedAmount, approvedRate, totalAmount, rejectionReason, id)
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

// HasPendingApplication checks if a customer has an active (non-final) application.
// Returns the existing app's ID and status, or 0 and "" if none.
// PR #89: also checks if the last rejected application's cutoff period has expired.
func (r *ApplicationRepo) HasPendingApplication(ctx context.Context, customerPIN string) (int, string, error) {
        var appID int
        var status string
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, status FROM loan_applications
                WHERE customer_pin = ? AND status IN ('pending', 'checking', 'pending_approval', 'pending_customer', 'pending_expert')
                ORDER BY id DESC`, customerPIN).Scan(&appID, &status)
        if err != nil {
                if err == sql.ErrNoRows {
                        // No active application — check if last rejection is still within validity period
                        return r.checkLastRejectionCutoff(ctx, customerPIN)
                }
                return 0, "", fmt.Errorf("failed to check pending applications: %w", err)
        }
        return appID, status, nil
}

// checkLastRejectionCutoff checks if the customer's most recent rejected
// application is still within the validity period of its cutoff rule.
// Returns (appID, "rejected", nil) if still blocked, or (0, "", nil) if
// the customer can re-apply.
func (r *ApplicationRepo) checkLastRejectionCutoff(ctx context.Context, customerPIN string) (int, string, error) {
        var appID int
        var rejectionReason string
        var updatedAt time.Time

        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, rejection_reason, updated_at
                FROM loan_applications
                WHERE customer_pin = ? AND status = 'rejected' AND rejection_reason IS NOT NULL AND rejection_reason != ''
                ORDER BY id DESC`, customerPIN).Scan(&appID, &rejectionReason, &updatedAt)
        if err != nil {
                if err == sql.ErrNoRows {
                        return 0, "", nil // No previous rejection — can apply
                }
                return 0, "", fmt.Errorf("failed to check last rejection: %w", err)
        }

        // Extract rule_code from rejection_reason (may have suffix like "AKB_STOP_FACTOR:AB")
        ruleCode := rejectionReason
        if idx := strings.Index(rejectionReason, ":"); idx > 0 {
                ruleCode = rejectionReason[:idx]
        }

        // Look up validity_days from business_cutoffs
        var validityDays int
        err = r.db.QueryRowContext(ctx, `
                SELECT validity_days FROM business_cutoffs
                WHERE rule_code = ? AND is_active = 1`, ruleCode).Scan(&validityDays)
        if err != nil {
                if err == sql.ErrNoRows {
                        // Rule not found in cutoffs table — allow re-apply (fail-soft)
                        return 0, "", nil
                }
                return 0, "", fmt.Errorf("failed to check cutoff validity: %w", err)
        }

        // validity_days = 0 means permanent rejection
        if validityDays == 0 {
                return appID, "rejected", nil
        }

        // Check if the validity period has expired
        daysSinceRejection := int(time.Since(updatedAt).Hours() / 24)
        if daysSinceRejection < validityDays {
                return appID, "rejected", nil // Still within validity period — blocked
        }

        // Validity period expired — customer can re-apply
        return 0, "", nil
}
