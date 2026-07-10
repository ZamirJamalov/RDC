package model

import "time"

// MyGov permission status constants.
const (
	MyGovStatusPending   = "pending"
	MyGovStatusGranted   = "granted"
	MyGovStatusFetched   = "fetched"
	MyGovStatusExpired   = "expired"
	MyGovStatusDenied    = "denied"
)

// MyGovPermission represents a MyGov data access permission record.
type MyGovPermission struct {
	ID             int
	ApplicationID  int
	CustomerPIN    string
	PermissionToken string
	LinkURL        string
	LinkExpiresAt  time.Time
	DataFetchedAt  *time.Time
	DataJSON       string
	Status         string
	CreatedAt      time.Time
}

// MyGovPermissionRequest is the request body for POST /api/mygov/permission-link.
type MyGovPermissionRequest struct {
	ApplicationID int    `json:"application_id"`
	CustomerPIN   string `json:"customer_pin"`
}

// MyGovPermissionResponse is returned by POST /api/mygov/permission-link.
type MyGovPermissionResponse struct {
	ApplicationID int    `json:"application_id"`
	URL           string `json:"url"`
	ExpiresAt     string `json:"expires_at"`
}
