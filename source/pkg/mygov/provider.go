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
//
// PR #64: extended with WorkHistory, DisabilityGroup, IsPensioner, PensionType
// to support the employment verification (6-month tenure rule) and pension
// verification (1st-group disability auto-reject) flows described in PR #63.
type AuthorizedData struct {
        Fin            string    `json:"fin"`
        FullName       string    `json:"full_name"`
        OfficialIncome float64   `json:"official_income"`
        EmployerName   string    `json:"employer_name"`
        Address        string    `json:"address"`
        FetchedAt      time.Time `json:"fetched_at"`

        // PR #64: WorkHistory is the customer's employment record from MyGov.
        // The first entry is the current job (EndDate == nil); subsequent entries
        // are previous jobs in reverse-chronological order. Used by the
        // employment-verification flow (PR #65) to compute the 6-month tenure rule:
        //   - if current job tenure >= 6 months → pass
        //   - else if previous job + gap <= 29 days → combined tenure considered
        //   - else → reject
        WorkHistory []WorkPlace `json:"work_history,omitempty"`

        // PR #64: DisabilityGroup indicates the customer's disability group
        // (if any) from the pension registry.
        //   0 = no disability
        //   1 = 1st group (severe) → auto-reject per business rule
        //   2 = 2nd group
        //   3 = 3rd group
        DisabilityGroup int `json:"disability_group,omitempty"`

        // PR #64: IsPensioner is true when the customer receives any pension.
        // When true, PensionType indicates the kind. Used by the pension-verification
        // flow (PR #65): if IsPensioner && DisabilityGroup == 1 → auto-reject.
        IsPensioner bool   `json:"is_pensioner"`
        PensionType string `json:"pension_type,omitempty"` // "age", "disability", "survivor"
}

// WorkPlace represents a single employment record in the customer's work history.
// The first entry in AuthorizedData.WorkHistory is the current job (EndDate nil).
type WorkPlace struct {
        EmployerName string     `json:"employer_name"`
        StartDate    time.Time  `json:"start_date"`
        EndDate      *time.Time `json:"end_date,omitempty"` // nil = currently employed
        Position     string     `json:"position,omitempty"`
}
