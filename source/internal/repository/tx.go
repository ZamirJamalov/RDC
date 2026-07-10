package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/internal/model"
)

// TxRunner is a small interface that both *sql.DB and *sql.Tx satisfy.
// It allows the same repo methods to run against either a fresh connection
// or an in-progress transaction without code duplication.
type TxRunner interface {
        ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
        QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
        QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx wraps fn in a single database transaction. If fn returns nil, the
// transaction is committed; if fn returns an error, the transaction is rolled
// back. The tx is passed to fn as a TxRunner so the same repo methods that
// accept *sql.DB can also accept the tx.
//
// Usage from the service layer:
//
//      err := repo.WithTx(ctx, func(runner TxRunner) error {
//          // call tx-aware variants of repo methods here
//          return repo.UpdateApplicationStatusTx(ctx, runner, id, status)
//      })
//
// Note: the existing methods (CreateApplication, UpdateApplicationStatus, etc.)
// still use r.db directly — they remain non-transactional for backward
// compatibility. The *Tx variants accept a TxRunner and run within a tx.
// New code in the credit engine should prefer the *Tx variants when atomicity
// matters (e.g. the decision pipeline).
func (r *ApplicationRepo) WithTx(ctx context.Context, fn func(TxRunner) error) error {
        tx, err := r.db.BeginTx(ctx, nil)
        if err != nil {
                return fmt.Errorf("failed to begin transaction: %w", err)
        }

        // defer rollback — if commit succeeds, the rollback is a no-op.
        // This guards against early returns / panics inside fn.
        defer func() {
                _ = tx.Rollback()
        }()

        if err := fn(tx); err != nil {
                return err
        }

        if err := tx.Commit(); err != nil {
                return fmt.Errorf("failed to commit transaction: %w", err)
        }
        return nil
}

// --- Tx-aware variants of the mutation methods ---
//
// These accept a TxRunner so they can run inside a WithTx block. The read
// methods (GetApplicationByID, GetCheckResults, HasPendingApplication,
// GetCreditLevelRate, CountApprovedAtLevel, GetLevelRanges) also have tx
// variants for cases where the caller wants read-after-write consistency
// within the same transaction (e.g. update status, then re-read to verify).

// UpdateApplicationStatusTx is the tx-aware variant of UpdateApplicationStatus.
func (r *ApplicationRepo) UpdateApplicationStatusTx(ctx context.Context, runner TxRunner, id int, status string) error {
        _, err := runner.ExecContext(ctx,
                "UPDATE loan_applications SET status = ?, updated_at = GETDATE() WHERE id = ?",
                status, id)
        if err != nil {
                return fmt.Errorf("failed to update application status: %w", err)
        }
        return nil
}

// UpdateApplicationDecisionTx is the tx-aware variant of UpdateApplicationDecision.
func (r *ApplicationRepo) UpdateApplicationDecisionTx(ctx context.Context, runner TxRunner, id int,
        status, creditLevel, rejectionReason string, approvedAmount, approvedRate float64) error {
        _, err := runner.ExecContext(ctx, `
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

// SaveCheckResultTx is the tx-aware variant of SaveCheckResult.
func (r *ApplicationRepo) SaveCheckResultTx(ctx context.Context, runner TxRunner, appID int, check *model.ApplicationCheckResult) error {
        _, err := runner.ExecContext(ctx, `
                INSERT INTO application_checks (application_id, check_type, status, detail, checked_at)
                VALUES (?, ?, ?, ?, ?)`,
                appID, check.CheckType, check.Status, check.Detail, check.CheckedAt)
        if err != nil {
                return fmt.Errorf("failed to save check result: %w", err)
        }
        return nil
}

// SaveCreditLevelHistoryTx is the tx-aware variant of SaveCreditLevelHistory.
func (r *ApplicationRepo) SaveCreditLevelHistoryTx(ctx context.Context, runner TxRunner, customerPIN, toLevel string, appID int) error {
        _, err := runner.ExecContext(ctx, `
                INSERT INTO credit_level_history (customer_pin, to_level, application_id)
                VALUES (?, ?, ?)`,
                customerPIN, toLevel, appID)
        if err != nil {
                return fmt.Errorf("failed to save credit level history: %w", err)
        }
        return nil
}
