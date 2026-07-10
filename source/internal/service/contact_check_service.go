package service

import (
	"fmt"
	"rdc-source/internal/model"
)

// ContactCheckService validates the 3 contact phone numbers on a loan
// application (T-5.5).
//
// Rules:
//   - All 3 contact numbers must be present (non-empty)
//   - All 3 must be different from each other (no duplicates)
//   - None of the contacts can match the customer's own phone (if provided)
//
// This is a pure function (no DB access) — it receives the application
// and returns a check result. The caller (credit engine) persists the result.
type ContactCheckService struct{}

// NewContactCheckService creates a new ContactCheckService.
func NewContactCheckService() *ContactCheckService {
	return &ContactCheckService{}
}

// Check validates the 3 contact numbers and returns a check result.
// The customerPhone parameter is optional — when provided, contacts must
// not match it.
func (s *ContactCheckService) Check(app *model.LoanApplication, customerPhone string) model.ApplicationCheckResult {
	contacts := []string{app.Contact1Phone, app.Contact2Phone, app.Contact3Phone}

	// Rule 1: all 3 must be present
	for i, c := range contacts {
		if c == "" {
			return model.ApplicationCheckResult{
				CheckType: "contacts_check",
				Status:    model.CheckStatusFailed,
				Detail:    fmt.Sprintf("Contact phone %d is missing", i+1),
			}
		}
	}

	// Rule 2: all 3 must be different
	if contacts[0] == contacts[1] || contacts[0] == contacts[2] || contacts[1] == contacts[2] {
		return model.ApplicationCheckResult{
			CheckType: "contacts_check",
			Status:    model.CheckStatusFailed,
			Detail:    "Contact phone numbers must be different from each other",
		}
	}

	// Rule 3: none can match the customer's own phone
	if customerPhone != "" {
		for i, c := range contacts {
			if c == customerPhone {
				return model.ApplicationCheckResult{
					CheckType: "contacts_check",
					Status:    model.CheckStatusFailed,
					Detail:    fmt.Sprintf("Contact phone %d matches the customer's own phone", i+1),
				}
			}
		}
	}

	return model.ApplicationCheckResult{
		CheckType: "contacts_check",
		Status:    model.CheckStatusPassed,
		Detail:    "All 3 contact numbers are valid and distinct",
	}
}

// CheckAddress validates the actual address field (T-5.6).
// Currently only checks non-emptiness — a future version could validate
// against an address database.
func (s *ContactCheckService) CheckAddress(app *model.LoanApplication) model.ApplicationCheckResult {
	if app.ActualAddress == "" {
		return model.ApplicationCheckResult{
			CheckType: "address_check",
			Status:    model.CheckStatusFailed,
			Detail:    "Actual address is missing",
		}
	}
	if len(app.ActualAddress) < 10 {
		return model.ApplicationCheckResult{
			CheckType: "address_check",
			Status:    model.CheckStatusFailed,
			Detail:    "Actual address is too short (minimum 10 characters)",
		}
	}
	return model.ApplicationCheckResult{
		CheckType: "address_check",
		Status:    model.CheckStatusPassed,
		Detail:    "Actual address provided",
	}
}
