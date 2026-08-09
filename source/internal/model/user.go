package model

import "time"

// User roles for the RDC dashboard.
const (
	RoleAdmin  = "admin"
	RoleExpert = "expert"
)

// IsValidRole returns true if the given role string is a recognized user role.
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleExpert
}

// User represents an authenticated dashboard user (expert or admin).
type User struct {
	ID                  int       `json:"id"`
	Username            string    `json:"username"`
	PasswordHash        string    `json:"-"`                   // never serialized
	Role                string    `json:"role"`                // "admin" or "expert"
	IsActive            bool      `json:"is_active"`
	IsLocked            bool      `json:"is_locked"`
	LockedUntil         *time.Time `json:"locked_until,omitempty"`
	FailedLoginAttempts int       `json:"failed_login_attempts"`
	LastLoginAt         *time.Time `json:"last_login_at,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Session represents an active user session (authenticated token).
type Session struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Token     string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
