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
        DelayDays       int     `json:"delay_days"`
        LevelAtClose    string  `json:"level_at_close"`
        ClosedAt        string  `json:"closed_at"`
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
        DelayDays       int     `json:"delay_days"`
        LevelAtClose    string  `json:"level_at_close"`
        ClosedAt        string  `json:"closed_at"`
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

// AkbScoreResponse mirrors the LW router JSON response, which is a direct
// conversion of the AKB SOAP XML (tag names preserved).
//
// AKB SOAP structure (per business, PR #55):
//
//   <soap:Envelope>
//     <soap:Body>
//       <ns2:getBorrowerScoreResponse xmlns:ns2="http://inquiryws.mkr.risk.az/">
//         <return>
//           <response>AB</response>   ← stop factor code (empty when no stop factor)
//           <point>1</point>          ← score: 1 = stop factor present, >1 = real score
//         </return>
//       </ns2:getBorrowerScoreResponse>
//     </soap:Body>
//   </soap:Envelope>
//
// LW converts this SOAP to JSON, preserving the XML tag names. The resulting
// JSON looks like:
//
//   Stop factor present:
//     {"return": {"response": "AB", "point": 1}}
//
//   No stop factor:
//     {"return": {"response": "",     "point": 750}}
//
// Rules (PR #55):
//   - point == 1 → stop factor is present; response holds the 2-letter code
//   - point >  1 → real credit score; response is empty
//   - Only one stop factor code is returned at a time (never multiple)
type AkbScoreResponse struct {
        // Fin is set by the LW router (not part of the AKB SOAP body) so the
        // caller can correlate the response with the request.
        Fin       string `json:"fin"`

        // QueryDate is set by the LW router (echoes the inquiry date).
        QueryDate string `json:"query_date,omitempty"`

        // Return mirrors the AKB SOAP <return> element. LW preserves the tag
        // name verbatim during SOAP→JSON conversion.
        Return *AkbScoreReturn `json:"return,omitempty"`
}

// AkbScoreReturn mirrors the AKB SOAP <return> element containing the score
// and stop factor code.
type AkbScoreReturn struct {
        // Response holds the 2-letter stop factor code when Point == 1
        // (e.g. "AB", "TY"). Empty string when no stop factor is present.
        Response string `json:"response,omitempty"`

        // Point is the AKB credit score.
        //   1  → stop factor present (see Response)
        //   >1 → real credit score
        Point int `json:"point"`
}

// Helper accessors on AkbScoreResponse keep the decision logic readable and
// centralize the "score == 1 means stop factor" rule.

// HasStopFactor returns true when AKB signalled a stop factor (Point == 1).
// Returns false when the response is nil (LW error / unavailable).
func (r *AkbScoreResponse) HasStopFactor() bool {
        return r != nil && r.Return != nil && r.Return.Point == 1
}

// StopFactorCode returns the 2-letter stop factor code when HasStopFactor is
// true, otherwise empty string.
func (r *AkbScoreResponse) StopFactorCode() string {
        if r.HasStopFactor() {
                return r.Return.Response
        }
        return ""
}

// Score returns the real AKB credit score. When a stop factor is present
// (Point == 1), returns 0 because the "real" score is not available — the
// caller should check HasStopFactor first and treat the application as
// rejected on stop-factor grounds. When no stop factor, returns Point.
//
// Returns 0 when the response is nil (LW error / unavailable) — the caller
// treats 0 as "no AKB information" and falls back to the request-supplied
// score (fail-soft).
func (r *AkbScoreResponse) Score() int {
        if r == nil || r.Return == nil {
                return 0
        }
        if r.Return.Point == 1 {
                // Stop factor present — the real score is not meaningful.
                return 0
        }
        return r.Return.Point
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

// LoanStatusResponse is returned by GetLoanStatus (polling).
// Represents the current state of the loan in the LW system.
type LoanStatusResponse struct {
        ApplicationID  int    `json:"application_id"`
        ContractStatus string `json:"contract_status"` // pending, signed, failed
        TransferStatus string `json:"transfer_status"` // pending, completed, failed
        LmsLoanID      string `json:"lms_loan_id"`
        Detail         string `json:"detail,omitempty"`
}
