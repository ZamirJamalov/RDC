package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ServiceCacheRepo handles cache lookups for external service responses.
// PR #205: service_audit_logs cədvəlindən son uğurlu response oxuyur.
type ServiceCacheRepo struct {
	db *sql.DB
}

// NewServiceCacheRepo creates a new ServiceCacheRepo.
func NewServiceCacheRepo(db *sql.DB) *ServiceCacheRepo {
	return &ServiceCacheRepo{db: db}
}

// GetCacheDays returns the cache_days for a service.
// Returns 0 if service not found or cache disabled.
func (r *ServiceCacheRepo) GetCacheDays(ctx context.Context, serviceName string) (int, error) {
	var cacheDays int
	err := r.db.QueryRowContext(ctx, `
		SELECT cache_days FROM service_cache_config WHERE service_name = ?`, serviceName).Scan(&cacheDays)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // service not configured → no cache
		}
		return 0, fmt.Errorf("failed to query cache_days for %s: %w", serviceName, err)
	}
	return cacheDays, nil
}

// GetCachedResponse returns the most recent successful response_body for a service
// for the given customer_pin, if it was logged within cacheDays.
// PR #205: customer_pin ilə axtarır (eyni müştərinin əvvəlki sorğusu).
//
// Returns:
//   - (response_body, true, nil) if cached response found within cacheDays
//   - ("", false, nil) if no cached response or expired
//   - ("", false, err) on DB error
func (r *ServiceCacheRepo) GetCachedResponse(ctx context.Context, serviceName, customerPIN string, cacheDays int) (string, bool, error) {
	if cacheDays <= 0 {
		return "", false, nil
	}

	// cache_days ərzində bu müştəri üçün son uğurlu response oxu
	// request_body-dan customer_pin extract etmək çətindir, ona görə
	// application_id vasitəsilə əlaqəli müraciətləri tapırıq
	var responseBody string
	cutoffTime := time.Now().AddDate(0, 0, -cacheDays)

	// service_audit_logs-dan son uğurlu (error boş və response_body dolu) row oxu
	// eyni customer_pin-li müraciətlər üçün
	err := r.db.QueryRowContext(ctx, `
		SELECT TOP 1 sal.response_body
		FROM service_audit_logs sal
		INNER JOIN loan_applications la ON sal.application_id = la.id
		WHERE sal.service_name = ?
		  AND la.customer_pin = ?
		  AND (sal.error IS NULL OR sal.error = '')
		  AND sal.response_body IS NOT NULL
		  AND LEN(sal.response_body) > 0
		  AND sal.created_at >= ?
		ORDER BY sal.created_at DESC`,
		serviceName, customerPIN, cutoffTime,
	).Scan(&responseBody)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil // no cached response
		}
		return "", false, fmt.Errorf("failed to query cached response for %s: %w", serviceName, err)
	}

	return responseBody, true, nil
}

// LogCacheHit writes a marker row into service_audit_logs when a cached
// response is used instead of a physical external call (PR #379).
// Cədvəldə cache istifadəsini görünür edir: method = 'CACHE', duration_ms = 0.
// request_body = customer_pin, response_body = cached cavabın özü.
// Qeyd: error boş və response_body dolu olduğu üçün bu row gələcək cache
// axtarışlarında da tapılır — cache pəncərəsi sliding mənada yenilənir.
func (r *ServiceCacheRepo) LogCacheHit(ctx context.Context, appID *int, serviceName, customerPIN, responseBody string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO service_audit_logs
			(application_id, service_name, method, url, request_body, response_body,
			 status_code, duration_ms, error)
		VALUES (?, ?, 'CACHE', '', ?, ?, 200, 0, '')`,
		appID, serviceName, customerPIN, responseBody)
	if err != nil {
		return fmt.Errorf("failed to log cache hit for %s: %w", serviceName, err)
	}
	return nil
}
