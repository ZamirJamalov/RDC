package lw

// CustomerLoan represents a single loan record from LW.
// Reuses the same structure as the former MockLmsLoanRow for seamless migration.
type CustomerLoan struct {
	ID              int     `json:"id"`
	CustomerPIN     string  `json:"customer_pin"`
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

// CustomerLoansResponse is the aggregated response of all loans for a customer.
type CustomerLoansResponse struct {
	CustomerPIN      string         `json:"customer_pin"`
	HasExistingLoans bool           `json:"has_existing_loans"`
	LoanCount        int            `json:"loan_count"`
	Loans            []CustomerLoan `json:"loans"`
}

// LoanSetupRequest is used to set up mock loan data for a customer.
type LoanSetupRequest struct {
	CustomerPIN  string          `json:"customer_pin"`
	ScenarioName string          `json:"scenario_name"`
	Loans        []LoanSetupItem `json:"loans"`
}

// LoanSetupItem represents a single loan entry when setting up mock data.
type LoanSetupItem struct {
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

// PersonalInfoResponse contains customer personal information from DIN.
type PersonalInfoResponse struct {
	Fin          string `json:"fin"`
	Serial       string `json:"serial"`
	FullName     string `json:"full_name"`
	DateOfBirth  string `json:"date_of_birth"`
	PlaceOfBirth string `json:"place_of_birth"`
	Address      string `json:"address"`
}

// AkbScoreResponse contains the AKB credit score.
type AkbScoreResponse struct {
	Fin       string `json:"fin"`
	Score     int    `json:"score"`
	QueryDate string `json:"query_date"`
}

// AkbHistoryResponse contains the full AKB inquiry response.
type AkbHistoryResponse struct {
	ReportID       string         `json:"report_id"`
	ReportingDate  string         `json:"reporting_date"`
	Borrower       AkbBorrower    `json:"borrower"`
	Liabilities    []AkbLiability `json:"liabilities"`
	InquiryHistory []AkbInquiry   `json:"inquiry_history"`
	Balance        float64        `json:"balance"`
}

// AkbBorrower contains borrower information from AKB.
type AkbBorrower struct {
	DocumentNo        string `json:"document_no"`
	Fin               string `json:"fin"`
	Name              string `json:"name"`
	DateOfBirth       string `json:"date_of_birth"`
	PlaceOfBirth      string `json:"place_of_birth"`
	PersonType        string `json:"person_type"`
	FileDate          string `json:"file_date"`
	RegisteredAddress string `json:"registered_address"`
	Status            string `json:"status"`
}

// AkbLiability contains a single liability record from AKB.
type AkbLiability struct {
	ID                      string                `json:"id"`
	BankID                  string                `json:"bank_id"`
	BankName                string                `json:"bank_name"`
	CreditType              string                `json:"credit_type"`
	GrantedOn               string                `json:"granted_on"`
	LineAmount              float64               `json:"line_amount"`
	DaysInterestOverdue     int                   `json:"days_interest_overdue"`
	DaysMainSumOverdue      int                   `json:"days_main_sum_overdue"`
	ContractDueOn           string                `json:"contract_due_on"`
	InterestRate            float64               `json:"interest_rate"`
	OutstandingDebtMain     float64               `json:"outstanding_debt_main"`
	OutstandingDebtInterest float64               `json:"outstanding_debt_interest"`
	MonthlyPaymentAmount    float64               `json:"monthly_payment_amount"`
	Prolongations           int                   `json:"prolongations"`
	CreditStatus            string                `json:"credit_status"`
	Currency                string                `json:"currency"`
	History                 []AkbLiabilityHistory `json:"history"`
}

// AkbLiabilityHistory contains monthly overdue history for a liability.
type AkbLiabilityHistory struct {
	ReportingPeriod string `json:"reporting_period"`
	OverdueDays     int    `json:"overdue_days"`
	CreditStatus    string `json:"credit_status"`
}

// AkbInquiry contains a single inquiry record from AKB.
type AkbInquiry struct {
	OrgIDType   string `json:"org_id_type"`
	BankID      string `json:"bank_id"`
	BankName    string `json:"bank_name"`
	InquiryDate string `json:"inquiry_date"`
	PurposeID   string `json:"purpose_id"`
}

// AsanFinanceResponse contains income data from ASAN Finance.
type AsanFinanceResponse struct {
	Fin            string  `json:"fin"`
	OfficialIncome float64 `json:"official_income"`
	Currency       string  `json:"currency"`
	EmployerName   string  `json:"employer_name"`
	QueryDate      string  `json:"query_date"`
}

// ApproveLoanRequest is sent to LW to approve a loan.
type ApproveLoanRequest struct {
	ApplicationID int     `json:"application_id"`
	Amount        float64 `json:"amount"`
	CardNumber    string  `json:"card_number"`
	CreditLevel   string  `json:"credit_level"`
	TermMonths    int     `json:"term_months"`
}

// ApproveLoanResponse is returned by LW after loan approval.
type ApproveLoanResponse struct {
	ApplicationID  int    `json:"application_id"`
	ContractStatus string `json:"contract_status"`
	TransferStatus string `json:"transfer_status"`
	LmsLoanID      string `json:"lms_loan_id"`
}
