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

// ============================================================
// PR #110: Real AKB envelope structs (camelCase, deeply nested)
// ============================================================
//
// PR #55/52 used a flat, snake_case struct that worked for mock/stub data
// but does NOT match the real AKB router JSON format. Real AKB returns a
// deeply-nested envelope:
//
//   {
//     "result": 0,
//     "requestId": 123,
//     "message": "...",
//     "data": {
//       "Request": {
//         "inquiryResult": {
//           "reportId": "...",
//           "liabilities": { "liability": [...] },
//           "score": { "calculated": true, "point": 650, "response": "" },
//           "borrower": { "fin": "...", "name": "..." },
//           "balance": 0
//         },
//         "serviceResponse": { "code": "0", "message": "OK" }
//       }
//     }
//   }
//
// The structs below mirror this exact shape. The HTTPProvider now parses
// the real envelope and converts it to the flat AkbHistoryResponse /
// AkbScoreResponse via the ToHistoryResponse() / ToScoreResponse() adapter
// methods defined below.

// AkbEnvelope is the top-level wrapper for real AKB JSON responses.
type AkbEnvelope struct {
        Result    int    `json:"result"`
        RequestID int    `json:"requestId"`
        Message   string `json:"message"`
        Data      struct {
                Request struct {
                        InquiryResult   AkbInquiryResult `json:"inquiryResult"`
                        ServiceResponse AkbServiceResp    `json:"serviceResponse"`
                } `json:"Request"`
        } `json:"data"`
}

// AkbServiceResp contains the service response code and message.
// code == "0" means success; any other value indicates an error.
type AkbServiceResp struct {
        Code    string `json:"code"`
        Message string `json:"message"`
}

// AkbInquiryResult contains the actual inquiry data.
type AkbInquiryResult struct {
        ReportID      string        `json:"reportId"`
        ReportingDate string        `json:"reportingDate"`
        Borrower      AkbBorrowerV2 `json:"borrower"`
        Liabilities   struct {
                Liability []AkbLiabilityV2 `json:"liability"`
        } `json:"liabilities"`
        Score   AkbScoreV2 `json:"score"`
        Balance float64    `json:"balance"`
}

// AkbBorrowerV2 is the real AKB borrower structure (camelCase).
type AkbBorrowerV2 struct {
        DocumentNo        string `json:"documentNo"`
        Name              string `json:"name"`
        Fin               string `json:"fin"`
        DateOfBirth       string `json:"dateOfBirth"`
        PlaceOfBirth      string `json:"placeOfBirth"`
        PersonType        string `json:"personType"`
        Country           string `json:"country"`
        FileDate          string `json:"fileDate"`
        LocationCity      string `json:"locationCity"`
        RegisteredAddress string `json:"registeredAddress"`
        Status            string `json:"status"`
}

// AkbLiabilityV2 is the real AKB liability structure (camelCase).
type AkbLiabilityV2 struct {
        ID                      string               `json:"id"`
        BankID                  string               `json:"bankId"`
        BankName                string               `json:"bankName"`
        AccountNo               string               `json:"accountNo"`
        CreditType              string               `json:"creditType"`
        CreditTypeName          string               `json:"creditTypeName"`
        GrantedOn               string               `json:"grantedOn"`
        InitialAmount           float64              `json:"initialAmount"`
        LineAmount              float64              `json:"lineAmount"`
        DaysInterestOverdue     int                  `json:"daysInterestOverdue"`
        DaysMainSumOverdue      int                  `json:"daysMainSumOverdue"`
        ContractDueOn           string               `json:"contractDueOn"`
        InterestRate            float64              `json:"interestRate"`
        OutstandingDebtMain     float64              `json:"outstandingDebtMain"`
        OutstandingDebtInterest float64              `json:"outstandingDebtInterest"`
        MonthlyPaymentAmount    float64              `json:"monthlyPaymentAmount"`
        Prolongations           int                  `json:"prolongations"`
        CreditStatus            string               `json:"creditStatus"`
        CreditStatusName        string               `json:"creditStatusName"`
        Currency                string               `json:"currency"`
        History                 struct {
                HistoryItem []AkbHistoryItemV2 `json:"historyItem"`
        } `json:"history"`
}

// AkbHistoryItemV2 is a single monthly history entry (camelCase).
type AkbHistoryItemV2 struct {
        OverdueDays     int    `json:"overdueDays"`
        ReportingPeriod string `json:"reportingPeriod"` // "yyyy-MM" format
        CreditStatus    string `json:"creditStatus"`
}

// AkbScoreV2 is the real AKB score structure (camelCase, inside inquiryResult).
type AkbScoreV2 struct {
        Calculated bool   `json:"calculated"`
        Point      int    `json:"point"`
        Response   string `json:"response"`
}

// ============================================================
// Adapter methods: convert real envelope → flat struct (backward compat)
// ============================================================

// ToHistoryResponse converts the real AKB envelope to the flat
// AkbHistoryResponse that credit_engine.go expects.
func (e *AkbEnvelope) ToHistoryResponse() *AkbHistoryResponse {
        if e == nil {
                return nil
        }
        ir := e.Data.Request.InquiryResult

        resp := &AkbHistoryResponse{
                ReportID:      ir.ReportID,
                ReportingDate: ir.ReportingDate,
                Borrower: AkbBorrower{
                        DocumentNo:        ir.Borrower.DocumentNo,
                        Fin:               ir.Borrower.Fin,
                        Name:              ir.Borrower.Name,
                        DateOfBirth:       ir.Borrower.DateOfBirth,
                        PlaceOfBirth:      ir.Borrower.PlaceOfBirth,
                        PersonType:        ir.Borrower.PersonType,
                        FileDate:          ir.Borrower.FileDate,
                        RegisteredAddress: ir.Borrower.RegisteredAddress,
                        Status:            ir.Borrower.Status,
                },
                Balance: ir.Balance,
        }

        // Map liabilities
        for _, lib := range ir.Liabilities.Liability {
                akbLib := AkbLiability{
                        ID:                      lib.ID,
                        BankID:                  lib.BankID,
                        BankName:                lib.BankName,
                        CreditType:              lib.CreditType,
                        GrantedOn:               lib.GrantedOn,
                        LineAmount:              lib.LineAmount,
                        DaysInterestOverdue:     lib.DaysInterestOverdue,
                        DaysMainSumOverdue:      lib.DaysMainSumOverdue,
                        ContractDueOn:           lib.ContractDueOn,
                        InterestRate:            lib.InterestRate,
                        OutstandingDebtMain:     lib.OutstandingDebtMain,
                        OutstandingDebtInterest: lib.OutstandingDebtInterest,
                        MonthlyPaymentAmount:    lib.MonthlyPaymentAmount,
                        Prolongations:           lib.Prolongations,
                        CreditStatus:            lib.CreditStatus,
                        Currency:                lib.Currency,
                }
                // Map history items
                for _, h := range lib.History.HistoryItem {
                        akbLib.History = append(akbLib.History, AkbLiabilityHistory{
                                ReportingPeriod: h.ReportingPeriod,
                                OverdueDays:     h.OverdueDays,
                                CreditStatus:    h.CreditStatus,
                        })
                }
                resp.Liabilities = append(resp.Liabilities, akbLib)
        }

        return resp
}

// ToScoreResponse converts the real AKB envelope to the flat
// AkbScoreResponse that credit_engine.go expects.
func (e *AkbEnvelope) ToScoreResponse(fin string) *AkbScoreResponse {
        if e == nil {
                return nil
        }
        score := e.Data.Request.InquiryResult.Score
        return &AkbScoreResponse{
                Fin:    fin,
                Return: &AkbScoreReturn{
                        Response: score.Response,
                        Point:    score.Point,
                },
        }
}

// IsServiceError returns true if the AKB service returned an error code.
// code == "0" means success; any other value (or non-empty message with
// empty code) indicates an error.
func (e *AkbEnvelope) IsServiceError() bool {
        if e == nil {
                return false
        }
        code := e.Data.Request.ServiceResponse.Code
        return code != "" && code != "0" && code != "200"
}

// ServiceErrorMessage returns the error message from serviceResponse.
func (e *AkbEnvelope) ServiceErrorMessage() string {
        if e == nil {
                return ""
        }
        return e.Data.Request.ServiceResponse.Message
}
