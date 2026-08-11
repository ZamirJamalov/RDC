package model

import "time"

// ServiceAuditLog represents a log entry for an external service call.
// PR #163: Bütün xarici servis çağırışları (AZMK, LW, AKB) DB-yə yazılır.
type ServiceAuditLog struct {
	ID              int       `json:"id"`
	ApplicationID   *int      `json:"application_id,omitempty"`
	ServiceName     string    `json:"service_name"`     // AZMK_KYC, AZMK_KYC_VERIFY, AZMK_PARTNER, etc.
	Method          string    `json:"method"`           // POST, GET, PUT
	URL             string    `json:"url"`              // tam URL
	RequestBody     string    `json:"request_body,omitempty"`
	ResponseBody    string    `json:"response_body,omitempty"`
	StatusCode      *int      `json:"status_code,omitempty"`
	DurationMs      *int      `json:"duration_ms,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	CreatedByUserID *int      `json:"created_by_user_id,omitempty"`
}
