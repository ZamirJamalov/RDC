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

// GetServiceHealth — PR #421: xarici servis sağlamlıq aqreqasiyası.
// Bir GROUP BY sorğu: hər service_name üçün son uğurlu/uğursuz çağırış vaxtı,
// pəncərədəki çağırış sayları və gecikmələr (son hours saat üzrə).
// Loki/extlog ilə paralel olaraq DB-də audit yazan servislar əhatə olunur
// (AZMK OnlineLending, AZMK CustomerData, VideoRecord və s.).
func (r *ServiceAuditLogRepo) GetServiceHealth(ctx context.Context, hours int) ([]model.ServiceHealth, error) {
	if hours <= 0 || hours > 24*30 {
		hours = 24
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT service_name,
		       MAX(CASE WHEN (error IS NULL OR error = '') AND status_code >= 200 AND status_code < 300 THEN created_at END) AS last_success_at,
		       MAX(CASE WHEN (error IS NOT NULL AND error <> '') OR status_code >= 400 THEN created_at END) AS last_failure_at,
		       COUNT(*) AS total_calls,
		       SUM(CASE WHEN (error IS NULL OR error = '') AND status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END) AS ok_calls,
		       SUM(CASE WHEN (error IS NOT NULL AND error <> '') OR status_code >= 400 THEN 1 ELSE 0 END) AS failed_calls,
		       ISNULL(AVG(CAST(duration_ms AS FLOAT)), 0) AS avg_duration_ms,
		       ISNULL(MAX(CASE WHEN rn = 1 THEN duration_ms END), 0) AS last_duration_ms
		FROM (
		    SELECT service_name, error, status_code, duration_ms, created_at,
		           ROW_NUMBER() OVER (PARTITION BY service_name ORDER BY created_at DESC) AS rn
		    FROM service_audit_logs
		    WHERE created_at >= DATEADD(HOUR, ?/*hours*/ * -1, GETDATE())
		) t
		GROUP BY service_name
		ORDER BY service_name`, hours)
	if err != nil {
		return nil, fmt.Errorf("failed to query service health: %w", err)
	}
	defer rows.Close()

	var healths []model.ServiceHealth
	for rows.Next() {
		var h model.ServiceHealth
		var okCalls, failedCalls sql.NullInt64
		var avgDur sql.NullFloat64
		if err := rows.Scan(
			&h.ServiceName, &h.LastSuccessAt, &h.LastFailureAt,
			&h.TotalCalls, &okCalls, &failedCalls, &avgDur, &h.LastDurationMs,
		); err != nil {
			return nil, fmt.Errorf("failed to scan service health: %w", err)
		}
		h.OkCalls = int(okCalls.Int64)
		h.FailedCalls = int(failedCalls.Int64)
		h.AvgDurationMs = int(avgDur.Float64)
		h.ComputeStatus()
		healths = append(healths, h)
	}
	return healths, rows.Err()
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
