package mygov

import (
	"context"
	"time"
)

// Provider defines the interface for MyGov data access operations.
// MyGov is Azerbaijan's e-government portal — customers grant permission
// for RDC to access their official data (income, employment, etc.).
//
// Flow:
//  1. RDC calls GeneratePermissionLink(fin) → MyGov returns a permission URL
//  2. Customer opens the URL and grants permission
//  3. MyGov notifies RDC (callback) OR RDC polls FetchAuthorizedData(token)
//  4. RDC stores the fetched data for the credit engine to use
type Provider interface {
	// GeneratePermissionLink creates a permission URL for the customer.
	// The customer opens this URL and grants RDC access to their data.
	GeneratePermissionLink(ctx context.Context, fin string) (*PermissionLink, error)

	// FetchAuthorizedData retrieves the customer's authorized data using the
	// permission token. Called after the customer grants permission.
	FetchAuthorizedData(ctx context.Context, token string) (*AuthorizedData, error)

	// Name returns a human-readable identifier ("mock", "mygov-http").
	Name() string
}

// PermissionLink contains the URL and token for customer permission.
type PermissionLink struct {
	Token      string    `json:"token"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// AuthorizedData contains the customer's official data from MyGov.
type AuthorizedData struct {
	Fin            string  `json:"fin"`
	FullName       string  `json:"full_name"`
	OfficialIncome float64 `json:"official_income"`
	EmployerName   string  `json:"employer_name"`
	Address        string  `json:"address"`
	FetchedAt      time.Time `json:"fetched_at"`
}
