package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/internal/model"
)

// VideoRecordRepo handles database operations for video records.
// PR #188: video record service audit və status poll məlumatlarını saxlayır.
type VideoRecordRepo struct {
        db *sql.DB
}

// NewVideoRecordRepo creates a new VideoRecordRepo.
func NewVideoRecordRepo(db *sql.DB) *VideoRecordRepo {
        return &VideoRecordRepo{db: db}
}

// Insert creates a new video record row.
func (r *VideoRecordRepo) Insert(ctx context.Context, vr *model.VideoRecord) error {
        _, err := r.db.ExecContext(ctx, `
                INSERT INTO video_records
                        (application_id, app_id_external, order_redirect_url, phone, amount, customer_name,
                         request_body, response_body, recorded, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, GETDATE(), GETDATE())`,
                vr.ApplicationID, vr.AppIDExternal, vr.OrderRedirectURL, vr.Phone, vr.Amount, vr.CustomerName,
                vr.RequestBody, vr.ResponseBody,
        )
        if err != nil {
                return fmt.Errorf("failed to insert video record: %w", err)
        }
        return nil
}

// GetByApplication retrieves the latest video record for an application.
func (r *VideoRecordRepo) GetByApplication(ctx context.Context, appID int) (*model.VideoRecord, error) {
        row := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, application_id, app_id_external, order_redirect_url, phone, amount,
                       customer_name, request_body, response_body, status_request_body, status_response_body,
                       recorded, status_checked_at, created_at, updated_at
                FROM video_records
                WHERE application_id = ?
                ORDER BY id DESC`, appID)

        var vr model.VideoRecord
        var orderRedirectURL, phone, customerName, requestBody, responseBody, statusReqBody, statusRespBody sql.NullString
        var amount sql.NullFloat64
        var statusCheckedAt sql.NullTime

        if err := row.Scan(
                &vr.ID, &vr.ApplicationID, &vr.AppIDExternal, &orderRedirectURL, &phone, &amount,
                &customerName, &requestBody, &responseBody, &statusReqBody, &statusRespBody,
                &vr.Recorded, &statusCheckedAt, &vr.CreatedAt, &vr.UpdatedAt,
        ); err != nil {
                if err == sql.ErrNoRows {
                        return nil, nil
                }
                return nil, fmt.Errorf("failed to scan video record: %w", err)
        }

        vr.OrderRedirectURL = orderRedirectURL.String
        vr.Phone = phone.String
        if amount.Valid {
                vr.Amount = amount.Float64
        }
        vr.CustomerName = customerName.String
        vr.RequestBody = requestBody.String
        vr.ResponseBody = responseBody.String
        vr.StatusRequestBody = statusReqBody.String
        vr.StatusResponseBody = statusRespBody.String
        if statusCheckedAt.Valid {
                t := statusCheckedAt.Time
                vr.StatusCheckedAt = &t
        }
        return &vr, nil
}

// UpdateStatus updates the recorded flag, status response body, and status_checked_at.
// PR #188: status poll nəticəsini saxlayır.
func (r *VideoRecordRepo) UpdateStatus(ctx context.Context, appID int, recorded bool, statusReqBody, statusRespBody string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE video_records
                SET recorded = ?,
                    status_request_body = ?,
                    status_response_body = ?,
                    status_checked_at = GETDATE(),
                    updated_at = GETDATE()
                WHERE application_id = ?`,
                recorded, statusReqBody, statusRespBody, appID,
        )
        if err != nil {
                return fmt.Errorf("failed to update video record status: %w", err)
        }
        return nil
}

// IsRecorded checks whether a video has been recorded for an application.
// PR #188: confirm düyməsini aktivləşdirmək üçün yoxlanılır.
func (r *VideoRecordRepo) IsRecorded(ctx context.Context, appID int) (bool, error) {
        var recorded bool
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 recorded FROM video_records
                WHERE application_id = ?
                ORDER BY id DESC`, appID).Scan(&recorded)
        if err != nil {
                if err == sql.ErrNoRows {
                        return false, nil
                }
                return false, fmt.Errorf("failed to check video record status: %w", err)
        }
        return recorded, nil
}

// ListByApplication retrieves all video records for an application (audit trail).
func (r *VideoRecordRepo) ListByApplication(ctx context.Context, appID int) ([]model.VideoRecord, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT id, application_id, app_id_external, order_redirect_url, phone, amount,
                       customer_name, request_body, response_body, status_request_body, status_response_body,
                       recorded, status_checked_at, created_at, updated_at
                FROM video_records
                WHERE application_id = ?
                ORDER BY id ASC`, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to list video records: %w", err)
        }
        defer rows.Close()

        var results []model.VideoRecord
        for rows.Next() {
                var vr model.VideoRecord
                var orderRedirectURL, phone, customerName, requestBody, responseBody, statusReqBody, statusRespBody sql.NullString
                var amount sql.NullFloat64
                var statusCheckedAt sql.NullTime

                if err := rows.Scan(
                        &vr.ID, &vr.ApplicationID, &vr.AppIDExternal, &orderRedirectURL, &phone, &amount,
                        &customerName, &requestBody, &responseBody, &statusReqBody, &statusRespBody,
                        &vr.Recorded, &statusCheckedAt, &vr.CreatedAt, &vr.UpdatedAt,
                ); err != nil {
                        return nil, fmt.Errorf("failed to scan video record: %w", err)
                }

                vr.OrderRedirectURL = orderRedirectURL.String
                vr.Phone = phone.String
                if amount.Valid {
                        vr.Amount = amount.Float64
                }
                vr.CustomerName = customerName.String
                vr.RequestBody = requestBody.String
                vr.ResponseBody = responseBody.String
                vr.StatusRequestBody = statusReqBody.String
                vr.StatusResponseBody = statusRespBody.String
                if statusCheckedAt.Valid {
                        t := statusCheckedAt.Time
                        vr.StatusCheckedAt = &t
                }
                results = append(results, vr)
        }
        return results, rows.Err()
}
