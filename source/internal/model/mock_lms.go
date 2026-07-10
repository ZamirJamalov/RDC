package model

// MockLmsSetupRequest is used to set up mock loan data for a customer in the LMS.
type MockLmsSetupRequest struct {
	CustomerPIN  string             `json:"customer_pin"`
	ScenarioName string             `json:"scenario_name"`
	Loans        []MockLmsLoanInput `json:"loans"`
}

// MockLmsLoanInput represents a single loan entry when setting up mock data.
type MockLmsLoanInput struct {
	LmsLoanID       string  `json:"lms_loan_id"`
	LoanType        string  `json:"loan_type"`
	Amount          float64 `json:"amount"`
	TermMonths      int     `json:"term_months"`
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date"`
	Status          string  `json:"status"`
	RemainingAmount float64 `json:"remaining_amount"`
	WasOnTime       bool    `json:"was_on_time"`
	EarlyCompletion bool    `json:"early_completion"`
}

// MockLMSCustomerLoans is the aggregated response of all loans for a customer.
type MockLMSCustomerLoans struct {
	CustomerPIN      string           `json:"customer_pin"`
	HasExistingLoans bool             `json:"has_existing_loans"`
	LoanCount        int              `json:"loan_count"`
	Loans            []MockLmsLoanRow `json:"loans"`
}

// MockLmsLoanRow represents a single loan record fetched from the mock LMS.
type MockLmsLoanRow struct {
	ID               int     `json:"id"`
	CustomerPIN      string  `json:"customer_pin"`
	ScenarioName     string  `json:"scenario_name"`
	LmsLoanID        string  `json:"lms_loan_id"`
	LoanType         string  `json:"loan_type"`
	Amount           float64 `json:"amount"`
	TermMonths       int     `json:"term_months"`
	StartDate        string  `json:"start_date"`
	EndDate          string  `json:"end_date"`
	Status           string  `json:"status"`
	RemainingAmount  float64 `json:"remaining_amount"`
	WasOnTime        bool    `json:"was_on_time"`
	EarlyCompletion  bool    `json:"early_completion"`
}