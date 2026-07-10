package model

import "time"

// SIMA session status constants.
const (
	SimaStatusPending = "pending"
	SimaStatusSuccess = "success"
	SimaStatusFailed  = "failed"
	SimaStatusExpired = "expired"
)

// SimaSession represents a SIMA KYC session stored in the database.
type SimaSession struct {
	ID            int
	ApplicationID int
	SessionID     string
	Fin           string
	Status        string // pending, success, failed, expired
	Detail        string
	URL           string
	StartedAt     time.Time
	CompletedAt   *time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

// SimaInitRequest is the request body for POST /api/router/sima/init.
type SimaInitRequest struct {
	ApplicationID int    `json:"application_id"`
	Fin           string `json:"fin"`
}

// SimaInitResponse is returned by POST /api/router/sima/init.
type SimaInitResponse struct {
	ApplicationID int    `json:"application_id"`
	SessionID     string `json:"session_id"`
	URL           string `json:"url"`
	ExpiresAt     string `json:"expires_at"`
}
