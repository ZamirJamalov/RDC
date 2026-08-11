package repository

import (
	"context"
	"database/sql"
	"fmt"

	"rdc-source/internal/model"
)

// ServiceAuditLogRepo handles database operations for service audit logs.
type ServiceAuditLogRepo struct {
	db *sql.DB
}

// NewServiceAuditLogRepo creates a new ServiceAuditLogRepo.
func NewServiceAuditLogRepo(db *sql.DB) *ServiceAuditLogRepo {
	return &ServiceAuditLogRepo{db: db}
}

// Insert logs a service call to the database.
func (r *ServiceAuditLogRepo) Insert(ctx context.Context, log *model.ServiceAuditLog) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO service_audit_logs
			(application_id, service_name, method, url, request_body, response_body,
			 status_code, duration_ms, error, created_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ApplicationID,
		log.ServiceName,
		log.Method,
		log.URL,
		log.RequestBody,
		log.ResponseBody,
		log.StatusCode,
		log.DurationMs,
		log.Error,
		log.CreatedByUserID,
	)
	if err != nil {
		return fmt.Errorf("failed to insert service audit log: %w", err)
	}
	return nil
}

// ListByApplication retrieves all audit logs for a given application, ordered by time.
func (r *ServiceAuditLogRepo) ListByApplication(ctx context.Context, appID int) ([]model.ServiceAuditLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, application_id, service_name, method, url, request_body, response_body,
		       status_code, duration_ms, error, created_at, created_by_user_id
		FROM service_audit_logs
		WHERE application_id = ?
		ORDER BY created_at ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []model.ServiceAuditLog
	for rows.Next() {
		var log model.ServiceAuditLog
		var appID, statusCode, durationMs, createdByUserID sql.NullInt64
		var requestBody, responseBody, errMsg sql.NullString
		if err := rows.Scan(
			&log.ID, &appID, &log.ServiceName, &log.Method, &log.URL,
			&requestBody, &responseBody, &statusCode, &durationMs, &errMsg,
			&log.CreatedAt, &createdByUserID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		if appID.Valid {
			v := int(appID.Int64)
			log.ApplicationID = &v
		}
		log.RequestBody = requestBody.String
		log.ResponseBody = responseBody.String
		if statusCode.Valid {
			v := int(statusCode.Int64)
			log.StatusCode = &v
		}
		if durationMs.Valid {
			v := int(durationMs.Int64)
			log.DurationMs = &v
		}
		log.Error = errMsg.String
		if createdByUserID.Valid {
			v := int(createdByUserID.Int64)
			log.CreatedByUserID = &v
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
