package repository

import (
        "context"
        "database/sql"
        "fmt"
        "time"

        "rdc-source/internal/model"
)

// LWLoanEventRepo handles database operations for LW loan events.
type LWLoanEventRepo struct {
        db *sql.DB
}

// NewLWLoanEventRepo creates a new LWLoanEventRepo.
func NewLWLoanEventRepo(db *sql.DB) *LWLoanEventRepo {
        return &LWLoanEventRepo{db: db}
}

// Create inserts a new LW loan event record.
func (r *LWLoanEventRepo) Create(ctx context.Context, appID int, status, lmsLoanID, detail string, eventAt time.Time) error {
        _, err := r.db.ExecContext(ctx, `
                INSERT INTO lw_loan_events (application_id, event_status, lms_loan_id, detail, event_at)
                VALUES (?, ?, ?, ?, ?)`,
                appID, status, lmsLoanID, detail, eventAt)
        if err != nil {
                return fmt.Errorf("failed to insert LW loan event: %w", err)
        }
        return nil
}

// GetByApplicationID retrieves all LW loan events for an application,
// ordered by event_at ascending (chronological).
func (r *LWLoanEventRepo) GetByApplicationID(ctx context.Context, appID int) ([]model.LWLoanEvent, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT id, application_id, event_status, lms_loan_id, detail, event_at
                FROM lw_loan_events
                WHERE application_id = ?
                ORDER BY event_at ASC`, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to query LW loan events: %w", err)
        }
        defer rows.Close()

        var events []model.LWLoanEvent
        for rows.Next() {
                var e model.LWLoanEvent
                var lmsLoanID, detail sql.NullString
                if err := rows.Scan(&e.ID, &e.ApplicationID, &e.EventStatus, &lmsLoanID, &detail, &e.EventAt); err != nil {
                        return nil, fmt.Errorf("failed to scan LW loan event: %w", err)
                }
                e.LmsLoanID = lmsLoanID.String
                e.Detail = detail.String
                events = append(events, e)
        }
        if err = rows.Err(); err != nil {
                return nil, fmt.Errorf("error iterating LW loan events: %w", err)
        }
        return events, nil
}
