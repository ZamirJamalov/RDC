package model

// LoanApplication represents a loan application in the system.
type LoanApplication struct {
        ID                int     `json:"id"`
        CustomerPIN       string  `json:"customer_pin"`
        CustomerSerial    string  `json:"customer_serial,omitempty"`
        CustomerFullName  string  `json:"customer_full_name"`
        Amount            float64 `json:"amount"`
        TermMonths        int     `json:"term_months"`
        LoanPurpose       string  `json:"loan_purpose"`
        Status            string  `json:"status"` // pending, checking, approved, rejected
        CreditLevel       string  `json:"credit_level"`
        ApprovedAmount    float64 `json:"approved_amount"`
        ApprovedRate      float64 `json:"approved_rate"`
        RejectionReasonID *int    `json:"rejection_reason_id,omitempty"`
        RejectionReason   string  `json:"rejection_reason,omitempty"`
        AkbScore          int     `json:"akb_score,omitempty"`
        CardNumber        string  `json:"card_number"` // 16-digit card number (required)
        CustomerPhone     string  `json:"customer_phone,omitempty"` // OTP-verified phone for MyGov SMS

        // T-5.3: additional fields for income verification + contacts + address
        OfficialIncome float64 `json:"official_income,omitempty"` // from ASAN Finance (T-5.1)
        Contact1Phone  string  `json:"contact1_phone,omitempty"`  // 3 contact numbers (T-5.5)
        Contact2Phone  string  `json:"contact2_phone,omitempty"`
        Contact3Phone  string  `json:"contact3_phone,omitempty"`
        ActualAddress  string  `json:"actual_address,omitempty"` // factiki ünvan (T-5.6)

        CreatedAt string `json:"created_at"`
        UpdatedAt string `json:"updated_at"`
}

// CreateApplicationRequest is the request body for creating a new loan application.
type CreateApplicationRequest struct {
        CustomerPIN      string  `json:"customer_pin"`
        CustomerFullName string  `json:"customer_full_name"`
        Amount           float64 `json:"amount"`
        TermMonths       int     `json:"term_months"`
        LoanPurpose      string  `json:"loan_purpose"`
        AkbScore         int     `json:"akb_score,omitempty"`

        // CardNumber is required for loan application (16-digit card number).
        CardNumber      string  `json:"card_number"`

        // CustomerPhone is the OTP-verified phone number, used for MyGov SMS.
        CustomerPhone   string  `json:"customer_phone,omitempty"`

        // T-5.3: optional fields collected during application init
        Contact1Phone string `json:"contact1_phone,omitempty"`
        Contact2Phone string `json:"contact2_phone,omitempty"`
        Contact3Phone string `json:"contact3_phone,omitempty"`
        ActualAddress string `json:"actual_address,omitempty"`
}

// ApplicationStatusResponse contains the full status of an application including checks and decision.
type ApplicationStatusResponse struct {
        ApplicationID int                      `json:"application_id"`
        Status        string                   `json:"status"`
        CreditLevel   string                   `json:"credit_level"`
        Checks        []ApplicationCheckResult `json:"checks"`
        Decision      *DecisionResult          `json:"decision,omitempty"`
}

// ApplicationCheckResult represents the result of a single check on an application.
type ApplicationCheckResult struct {
        CheckType string `json:"check_type"`
        Status    string `json:"status"` // passed, failed, pending
        Detail    string `json:"detail"`
        CheckedAt string `json:"checked_at"`
}

// DecisionResult represents the final credit decision for an application.
type DecisionResult struct {
        Decision        string  `json:"decision"` // approved, rejected
        ApprovedAmount  float64 `json:"approved_amount,omitempty"`
        ApprovedRate    float64 `json:"approved_rate,omitempty"`
        RejectionReason string  `json:"rejection_reason,omitempty"`
        DecidedAt       string  `json:"decided_at"`
}
