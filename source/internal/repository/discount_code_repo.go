package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/internal/model"
)

// DiscountCodeRepo handles database operations for discount/referral codes.
//
// PR #94: foundation layer. The service-layer logic (generation, validation,
// discount application) will be added in PR #95.
//
// All methods accept a context.Context for cancellation/timeout propagation.
// The *Tx variants accept a TxRunner so they can run inside an approval
// transaction (atomicity: code generation + application update + mark-used
// must all succeed or all roll back).
type DiscountCodeRepo struct {
        db *sql.DB
}

// NewDiscountCodeRepo creates a new DiscountCodeRepo with the given database connection.
func NewDiscountCodeRepo(db *sql.DB) *DiscountCodeRepo {
        return &DiscountCodeRepo{db: db}
}

// Create inserts a new discount code record and sets the ID + CreatedAt on the struct.
func (r *DiscountCodeRepo) Create(ctx context.Context, c *model.DiscountCode) error {
        return r.CreateTx(ctx, r.db, c)
}

// CreateTx is the tx-aware variant of Create.
// PR #97: issued_from_application_id is now nullable — if IssuedFromApplicationID
// is nil (manually-created code), NULL is stored.
func (r *DiscountCodeRepo) CreateTx(ctx context.Context, runner TxRunner, c *model.DiscountCode) error {
        var issuedFromAppID interface{}
        if c.IssuedFromApplicationID != nil {
                issuedFromAppID = *c.IssuedFromApplicationID
        }
        err := runner.QueryRowContext(ctx, `
                INSERT INTO discount_codes
                        (code, issued_to_customer_id, issued_from_application_id,
                         discount_type, discount_value, status, valid_until)
                OUTPUT INSERTED.id, INSERTED.created_at
                VALUES (?, ?, ?, ?, ?, ?, ?)`,
                c.Code,
                c.IssuedToCustomerID,
                issuedFromAppID,
                c.DiscountType,
                c.DiscountValue,
                c.Status,
                c.ValidUntil,
        ).Scan(&c.ID, &c.CreatedAt)
        if err != nil {
                return fmt.Errorf("failed to insert discount code: %w", err)
        }
        return nil
}

// GetByCode retrieves a discount code by its code string (case-sensitive).
// Returns sql.ErrNoRows (wrapped) if not found.
func (r *DiscountCodeRepo) GetByCode(ctx context.Context, code string) (*model.DiscountCode, error) {
        return r.GetByCodeTx(ctx, r.db, code)
}

// GetByCodeTx is the tx-aware variant of GetByCode (for re-validation inside tx).
func (r *DiscountCodeRepo) GetByCodeTx(ctx context.Context, runner TxRunner, code string) (*model.DiscountCode, error) {
        var c model.DiscountCode
        var issuedFromAppID, usedByAppID sql.NullInt64
        var usedAt, validUntil sql.NullTime

        err := runner.QueryRowContext(ctx, `
                SELECT id, code, issued_to_customer_id, issued_from_application_id,
                       discount_type, discount_value, status,
                       used_by_application_id, used_at, valid_until, created_at
                FROM discount_codes
                WHERE code = ?`, code).Scan(
                &c.ID,
                &c.Code,
                &c.IssuedToCustomerID,
                &issuedFromAppID,
                &c.DiscountType,
                &c.DiscountValue,
                &c.Status,
                &usedByAppID,
                &usedAt,
                &validUntil,
                &c.CreatedAt,
        )
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, fmt.Errorf("discount code %q not found: %w", code, err)
                }
                return nil, fmt.Errorf("failed to query discount code: %w", err)
        }

        if issuedFromAppID.Valid {
                id := int(issuedFromAppID.Int64)
                c.IssuedFromApplicationID = &id
        }
        if usedByAppID.Valid {
                id := int(usedByAppID.Int64)
                c.UsedByApplicationID = &id
        }
        if usedAt.Valid {
                t := usedAt.Time
                c.UsedAt = &t
        }
        if validUntil.Valid {
                t := validUntil.Time
                c.ValidUntil = &t
        }

        return &c, nil
}

// MarkUsed marks a discount code as 'used' by a specific application.
// Sets status='used', used_by_application_id, used_at=GETDATE().
func (r *DiscountCodeRepo) MarkUsed(ctx context.Context, codeID, applicationID int) error {
        return r.MarkUsedTx(ctx, r.db, codeID, applicationID)
}

// MarkUsedTx is the tx-aware variant of MarkUsed.
func (r *DiscountCodeRepo) MarkUsedTx(ctx context.Context, runner TxRunner, codeID, applicationID int) error {
        res, err := runner.ExecContext(ctx, `
                UPDATE discount_codes
                SET status = ?,
                    used_by_application_id = ?,
                    used_at = GETDATE()
                WHERE id = ? AND status = ?`,
                model.DiscountStatusUsed,
                applicationID,
                codeID,
                model.DiscountStatusActive,
        )
        if err != nil {
                return fmt.Errorf("failed to mark discount code as used: %w", err)
        }
        rows, err := res.RowsAffected()
        if err != nil {
                return fmt.Errorf("failed to check rows affected: %w", err)
        }
        if rows == 0 {
                return fmt.Errorf("discount code %d could not be marked as used (already used or not found)", codeID)
        }
        return nil
}

// ExistsByCode returns true if a discount code with the given code string
// already exists. Used during code generation to avoid collisions.
func (r *DiscountCodeRepo) ExistsByCode(ctx context.Context, code string) (bool, error) {
        var exists int
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 1 FROM discount_codes WHERE code = ?`, code).Scan(&exists)
        if err != nil {
                if err == sql.ErrNoRows {
                        return false, nil
                }
                return false, fmt.Errorf("failed to check discount code existence: %w", err)
        }
        return true, nil
}

// GetByOwnerCustomerID returns all discount codes owned by a customer,
// ordered by most recent first. Future use: customer dashboard "my codes" view.
func (r *DiscountCodeRepo) GetByOwnerCustomerID(ctx context.Context, customerID int) ([]*model.DiscountCode, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT id, code, issued_to_customer_id, issued_from_application_id,
                       discount_type, discount_value, status,
                       used_by_application_id, used_at, valid_until, created_at
                FROM discount_codes
                WHERE issued_to_customer_id = ?
                ORDER BY created_at DESC`, customerID)
        if err != nil {
                return nil, fmt.Errorf("failed to query discount codes by owner: %w", err)
        }
        defer rows.Close()

        var codes []*model.DiscountCode
        for rows.Next() {
                var c model.DiscountCode
                var issuedFromAppID, usedByAppID sql.NullInt64
                var usedAt, validUntil sql.NullTime

                if err := rows.Scan(
                        &c.ID,
                        &c.Code,
                        &c.IssuedToCustomerID,
                        &issuedFromAppID,
                        &c.DiscountType,
                        &c.DiscountValue,
                        &c.Status,
                        &usedByAppID,
                        &usedAt,
                        &validUntil,
                        &c.CreatedAt,
                ); err != nil {
                        return nil, fmt.Errorf("failed to scan discount code: %w", err)
                }

                if issuedFromAppID.Valid {
                        id := int(issuedFromAppID.Int64)
                        c.IssuedFromApplicationID = &id
                }
                if usedByAppID.Valid {
                        id := int(usedByAppID.Int64)
                        c.UsedByApplicationID = &id
                }
                if usedAt.Valid {
                        t := usedAt.Time
                        c.UsedAt = &t
                }
                if validUntil.Valid {
                        t := validUntil.Time
                        c.ValidUntil = &t
                }

                codes = append(codes, &c)
        }

        if err = rows.Err(); err != nil {
                return nil, fmt.Errorf("error iterating discount codes: %w", err)
        }

        return codes, nil
}
