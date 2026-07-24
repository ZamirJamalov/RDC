package repository

import (
        "context"
        "database/sql"
        "fmt"
)

// LevelRange describes a single rate configuration row for a credit level.
type LevelRange struct {
        MinAmount          float64
        MaxAmount          float64
        TermMonths         int
        Rate               float64 // commission rate
        Phase              int
        AnnualInterestRate float64 // PR #78: real annual interest rate (55/52/48/45)
}

// GetCreditLevelRate looks up the applicable interest rate for a given credit level,
// amount, term, and unlock phase. unlock_phase uses <= so that phase 2 customers can
// also access phase 1 ranges.
func (r *ApplicationRepo) GetCreditLevelRate(ctx context.Context, level string, amount float64, termMonths int, unlockPhase int) (float64, error) {
        var rate float64
        err := r.db.QueryRowContext(ctx, `
                SELECT rate FROM credit_levels
                WHERE level_name = ? AND min_amount <= ? AND max_amount >= ? AND term_months = ? AND unlock_phase <= ? AND is_active = 1`,
                level, amount, amount, termMonths, unlockPhase).Scan(&rate)
        if err != nil {
                if err == sql.ErrNoRows {
                        return 0, fmt.Errorf("no rate found for level=%s amount=%.2f term=%d months (unlock_phase=%d)",
                                level, amount, termMonths, unlockPhase)
                }
                return 0, fmt.Errorf("failed to query credit level rate: %w", err)
        }
        return rate, nil
}

// CountApprovedAtLevel counts how many loan applications a customer has had approved
// at a specific credit level. Used to determine unlock_phase:
//   - 0 approved = phase 1 (first loan at this level, limited ranges)
//   - 1+ approved = phase 2 (full ranges unlocked)
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

// GetLevelRanges returns all active rate configurations for a given credit level and
// unlock phase. Used for building descriptive error messages when a requested
// amount/term has no matching rate.
func (r *ApplicationRepo) GetLevelRanges(ctx context.Context, level string, unlockPhase int) ([]LevelRange, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT min_amount, max_amount, term_months, rate, unlock_phase, annual_interest_rate
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
                if err := rows.Scan(&lr.MinAmount, &lr.MaxAmount, &lr.TermMonths, &lr.Rate, &lr.Phase, &lr.AnnualInterestRate); err != nil {
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
// Called whenever an application is approved (auto or manual), so future
// applications can compute the unlock_phase correctly.
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
