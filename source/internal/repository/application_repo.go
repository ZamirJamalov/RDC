package repository

import (
        "context"
        "database/sql"
        "fmt"
        "strings"
        "time"

        "github.com/google/uuid"
        "github.com/microsoft/go-mssqldb"

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

// CreateApplication inserts a new loan application and sets the ID and PublicID on the struct.
// PR #191: public_id UUID DB tərəfindən generate olunur (DEFAULT NEWID()).
// PR #194: mssql.UniqueIdentifier kimi scan edir, sonra uuid.UUID string-ə çevirir.
func (r *ApplicationRepo) CreateApplication(ctx context.Context, app *model.LoanApplication) error {
        var rawPublicID mssql.UniqueIdentifier
        err := r.db.QueryRowContext(ctx, `
                INSERT INTO loan_applications
                        (customer_pin, customer_serial, customer_full_name, amount, term_months, loan_purpose, status, akb_score,
                         contact1_phone, contact2_phone, contact3_phone, actual_address, card_number, customer_phone)
                OUTPUT INSERTED.id, INSERTED.public_id
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
        ).Scan(&app.ID, &rawPublicID)
        if err != nil {
                return fmt.Errorf("failed to insert application: %w", err)
        }

        // Convert mssql.UniqueIdentifier → uuid.UUID → string
        app.PublicID = uuid.UUID(rawPublicID).String()

        return nil
}

// GetApplicationByID fetches a loan application by its primary key.
func (r *ApplicationRepo) GetApplicationByID(ctx context.Context, id int) (*model.LoanApplication, error) {
        var app model.LoanApplication
        var rawPublicID mssql.UniqueIdentifier // PR #194: UUID byte-ları
        var rejectionReasonID sql.NullInt64
        var rejectionReason, creditLevel sql.NullString
        var approvedAmount, approvedRate, totalAmount sql.NullFloat64
        var akbScore sql.NullInt64
        var officialIncome sql.NullFloat64
        var contact1, contact2, contact3, contact1Rel, contact2Rel, contact3Rel, contact1Name, contact2Name, contact3Name, address, customerPhone, customerSerial sql.NullString
        var customerConfirmedAt sql.NullString
        // PR #94: discount fields
        var discountCode sql.NullString
        var discountAmount sql.NullFloat64
        // PR #116: AZMK Online Lending fields
        var kycID, partnerID, cardID, lwApplicationID sql.NullString
        // PR #124: kontakt yoxlanma statusu
        var contact1Verified, contact2Verified, contact3Verified sql.NullBool
        // PR #142: processed_by fields
        var processedByUserID sql.NullInt64
        var processedByUsername sql.NullString
        // PR #148: audit fields
        var contactsUpdatedByUserID, timerUpdatedByUserID, mygovCheckedByUserID sql.NullInt64
        var contactsUpdatedByUsername, timerUpdatedByUsername, mygovCheckedByUsername sql.NullString
        var contactsUpdatedAt, mygovCheckedAt sql.NullString

        err := r.db.QueryRowContext(ctx, `
                SELECT id, public_id, customer_pin, customer_full_name, amount, term_months, loan_purpose,
                       status, credit_level, approved_amount, approved_rate, total_amount,
                       rejection_reason_id, rejection_reason, akb_score,
                       official_income, contact1_phone, contact2_phone, contact3_phone, actual_address,
                       contact1_relation, contact2_relation, contact3_relation,
                       contact1_name, contact2_name, contact3_name,
                       contact1_verified, contact2_verified, contact3_verified,
                       card_number, customer_phone, customer_serial,
                       customer_confirmed_at, card_ownership_confirmed,
                       discount_code, discount_amount,
                       kyc_id, partner_id, card_id, lw_application_id,
                       timer_seconds,
                       processed_by_user_id, processed_by_username,
                       contacts_updated_by_user_id, contacts_updated_by_username, contacts_updated_at,
                       timer_updated_by_user_id, timer_updated_by_username,
                       mygov_checked_by_user_id, mygov_checked_by_username, mygov_checked_at,
                       created_at, updated_at
                FROM loan_applications WHERE id = ?`, id).Scan(
                &app.ID,
                &rawPublicID,
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
                &contact1Name,
                &contact2Name,
                &contact3Name,
                &contact1Verified,
                &contact2Verified,
                &contact3Verified,
                &app.CardNumber,
                &customerPhone,
                &customerSerial,
                &customerConfirmedAt,
                &app.CardOwnershipConfirmed,
                &discountCode,
                &discountAmount,
                &kycID,
                &partnerID,
                &cardID,
                &lwApplicationID,
                &app.TimerSeconds,
                &processedByUserID,
                &processedByUsername,
                &contactsUpdatedByUserID,
                &contactsUpdatedByUsername,
                &contactsUpdatedAt,
                &timerUpdatedByUserID,
                &timerUpdatedByUsername,
                &mygovCheckedByUserID,
                &mygovCheckedByUsername,
                &mygovCheckedAt,
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
        // PR #128: kontakt adları
        app.Contact1Name = contact1Name.String
        app.Contact2Name = contact2Name.String
        app.Contact3Name = contact3Name.String
        // PR #124: kontakt yoxlanma statusu
        if contact1Verified.Valid { v := contact1Verified.Bool; app.Contact1Verified = &v }
        if contact2Verified.Valid { v := contact2Verified.Bool; app.Contact2Verified = &v }
        if contact3Verified.Valid { v := contact3Verified.Bool; app.Contact3Verified = &v }
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
        // PR #94: discount fields
        app.DiscountCode = discountCode.String
        if discountAmount.Valid {
                da := discountAmount.Float64
                app.DiscountAmount = &da
        }
        // PR #116: AZMK Online Lending fields
        app.KycID = kycID.String
        app.PartnerID = partnerID.String
        app.CardID = cardID.String
        app.LwApplicationID = lwApplicationID.String
        // PR #142: processed_by fields
        if processedByUserID.Valid {
            uid := int(processedByUserID.Int64)
            app.ProcessedByUserID = &uid
        }
        app.ProcessedByUsername = processedByUsername.String
        // PR #148: audit fields
        if contactsUpdatedByUserID.Valid {
            uid := int(contactsUpdatedByUserID.Int64)
            app.ContactsUpdatedByUserID = &uid
        }
        app.ContactsUpdatedByUsername = contactsUpdatedByUsername.String
        app.ContactsUpdatedAt = contactsUpdatedAt.String
        if timerUpdatedByUserID.Valid {
            uid := int(timerUpdatedByUserID.Int64)
            app.TimerUpdatedByUserID = &uid
        }
        app.TimerUpdatedByUsername = timerUpdatedByUsername.String
        if mygovCheckedByUserID.Valid {
            uid := int(mygovCheckedByUserID.Int64)
            app.MyGovCheckedByUserID = &uid
        }
        app.MyGovCheckedByUsername = mygovCheckedByUsername.String
        app.MyGovCheckedAt = mygovCheckedAt.String

        // PR #194: mssql.UniqueIdentifier → uuid.UUID → string
        app.PublicID = uuid.UUID(rawPublicID).String()

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

// UpdateProcessedBy sets the processed_by_user_id and processed_by_username fields.
// PR #142: called when an expert approves or rejects an application.
func (r *ApplicationRepo) UpdateProcessedBy(ctx context.Context, id int, userID int, username string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET processed_by_user_id = ?,
                    processed_by_username = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                userID, username, id)
        if err != nil {
                return fmt.Errorf("failed to update processed_by: %w", err)
        }
        return nil
}

// UpdateContactsAudit sets the contacts_updated_by fields.
// PR #148: called when an expert updates contact numbers.
func (r *ApplicationRepo) UpdateContactsAudit(ctx context.Context, id int, userID int, username string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET contacts_updated_by_user_id = ?,
                    contacts_updated_by_username = ?,
                    contacts_updated_at = GETDATE(),
                    updated_at = GETDATE()
                WHERE id = ?`,
                userID, username, id)
        if err != nil {
                return fmt.Errorf("failed to update contacts audit: %w", err)
        }
        return nil
}

// UpdateTimerAudit sets the timer_updated_by fields.
// PR #148: called when an expert saves the timer.
func (r *ApplicationRepo) UpdateTimerAudit(ctx context.Context, id int, userID int, username string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET timer_updated_by_user_id = ?,
                    timer_updated_by_username = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                userID, username, id)
        if err != nil {
                return fmt.Errorf("failed to update timer audit: %w", err)
        }
        return nil
}

// UpdateMyGovAudit sets the mygov_checked_by fields.
// PR #148: called when an expert performs a MyGov verification.
func (r *ApplicationRepo) UpdateMyGovAudit(ctx context.Context, id int, userID int, username string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET mygov_checked_by_user_id = ?,
                    mygov_checked_by_username = ?,
                    mygov_checked_at = GETDATE(),
                    updated_at = GETDATE()
                WHERE id = ?`,
                userID, username, id)
        if err != nil {
                return fmt.Errorf("failed to update mygov audit: %w", err)
        }
        return nil
}

// UpdateApplicationDiscount sets the discount_code and discount_amount fields
// on a loan application. PR #94: called by customer-confirm flow (discount_code)
// and by the approval flow (discount_amount after discount is computed).
//
// discountCode: the code string the customer entered (empty to clear).
// discountAmount: nil to clear, non-nil to set a value.
func (r *ApplicationRepo) UpdateApplicationDiscount(ctx context.Context, id int, discountCode string, discountAmount *float64) error {
        var amt interface{}
        if discountAmount != nil {
                amt = *discountAmount
        }
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET discount_code = ?,
                    discount_amount = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                discountCode, amt, id)
        if err != nil {
                return fmt.Errorf("failed to update application discount: %w", err)
        }
        return nil
}

// UpdateApplicationDiscountTx is the tx-aware variant of UpdateApplicationDiscount.
func (r *ApplicationRepo) UpdateApplicationDiscountTx(ctx context.Context, runner TxRunner, id int, discountCode string, discountAmount *float64) error {
        var amt interface{}
        if discountAmount != nil {
                amt = *discountAmount
        }
        _, err := runner.ExecContext(ctx, `
                UPDATE loan_applications
                SET discount_code = ?,
                    discount_amount = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                discountCode, amt, id)
        if err != nil {
                return fmt.Errorf("failed to update application discount: %w", err)
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
// PR #208: pending_customer və pending_expert artıq blocklanmır — yarımçıq müraciətlər
// üçün təkrar müraciət mümkündür. Yalnız pending, checking, pending_approval blocklanır
// (kredit engine işləyir və ya ekspert təsdiqindədir).
func (r *ApplicationRepo) HasPendingApplication(ctx context.Context, customerPIN string) (int, string, error) {
        var appID int
        var status string
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, status FROM loan_applications
                WHERE customer_pin = ? AND status IN ('pending', 'checking', 'pending_approval')
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

// UpdateContacts saves contact phone numbers, relations, and verification status.
// PR #124: works in any status (pending_expert, pending_approval, etc.) —
// unlike CompleteApplication which only works in pending_expert.
func (r *ApplicationRepo) UpdateContacts(ctx context.Context, id int, app *model.LoanApplication) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET contact1_phone = ?,
                    contact2_phone = ?,
                    contact3_phone = ?,
                    contact1_relation = ?,
                    contact2_relation = ?,
                    contact3_relation = ?,
                    contact1_name = ?,
                    contact2_name = ?,
                    contact3_name = ?,
                    contact1_verified = ?,
                    contact2_verified = ?,
                    contact3_verified = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                nullableString(app.Contact1Phone),
                nullableString(app.Contact2Phone),
                nullableString(app.Contact3Phone),
                nullableString(app.Contact1Relation),
                nullableString(app.Contact2Relation),
                nullableString(app.Contact3Relation),
                nullableString(app.Contact1Name),
                nullableString(app.Contact2Name),
                nullableString(app.Contact3Name),
                nullableBool(app.Contact1Verified),
                nullableBool(app.Contact2Verified),
                nullableBool(app.Contact3Verified),
                id,
        )
        if err != nil {
                return fmt.Errorf("failed to update contacts: %w", err)
        }
        return nil
}

// nullableBool returns nil when b is nil, otherwise returns the bool value.
func nullableBool(b *bool) interface{} {
        if b == nil {
                return nil
        }
        return *b
}

// UpdateTimer saves the elapsed timer seconds for an application.
// PR #134: ekspert müraciəti açandan təsdiq/imtinaya qədər vaxt saxlanır.
func (r *ApplicationRepo) UpdateTimer(ctx context.Context, id int, seconds int) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET timer_seconds = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
                seconds, id)
        if err != nil {
                return fmt.Errorf("failed to update timer: %w", err)
        }
        return nil
}
