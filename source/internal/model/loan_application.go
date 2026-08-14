package model

// LoanApplication represents a loan application in the system.
type LoanApplication struct {
        ID                int     `json:"id"`
        PublicID          string  `json:"public_id"` // PR #191: UUID format public ID (UI və xarici servis üçün)
        CustomerPIN       string  `json:"customer_pin"`
        CustomerSerial    string  `json:"customer_serial,omitempty"`
        CustomerFullName  string  `json:"customer_full_name"`
        Amount            float64 `json:"amount"`
        TermMonths        int     `json:"term_months"`
        LoanPurpose       string  `json:"loan_purpose"`
        Status            string  `json:"status"` // pending, checking, approved, rejected
        CreditLevel       string  `json:"credit_level"`
        ApprovedAmount    float64 `json:"approved_amount"`
        ApprovedRate      float64 `json:"approved_rate"`                          // commission rate (NOT interest — see migration 021)
        TotalAmount       float64 `json:"total_amount,omitempty"`                 // Principal + Commission (sent to LW)
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
        // PR #128: kontakt şəxslərinin ad soyadı
        Contact1Name   string  `json:"contact1_name,omitempty"`
        Contact2Name   string  `json:"contact2_name,omitempty"`
        Contact3Name   string  `json:"contact3_name,omitempty"`
        Contact1Relation string `json:"contact1_relation,omitempty"` // PR #85: Ata, Ana, Qardaş, etc.
        Contact2Relation string `json:"contact2_relation,omitempty"`
        Contact3Relation string `json:"contact3_relation,omitempty"`
        // PR #124: kontakt yoxlanma statusu — NULL=yoxlanılmayıb, true=təsdiq, false=imtina
        Contact1Verified *bool `json:"contact1_verified,omitempty"`
        Contact2Verified *bool `json:"contact2_verified,omitempty"`
        Contact3Verified *bool `json:"contact3_verified,omitempty"`
        ActualAddress  string  `json:"actual_address,omitempty"` // factiki ünvan (T-5.6)

        // PR #58: customer-side confirmation flow.
        // CustomerConfirmedAt is set when the customer submits the customer-confirm
        // form on the public website (after selecting amount + entering card +
        // address + ticking the ownership checkbox). Empty until that point.
        CustomerConfirmedAt string `json:"customer_confirmed_at,omitempty"`
        // CardOwnershipConfirmed is the audit flag from the customer's checkbox
        // "I confirm this card belongs to me". Stored for legal/audit purposes.
        CardOwnershipConfirmed bool `json:"card_ownership_confirmed"`

        // PR #94: Discount / referral code.
        // DiscountCode is the code the customer entered in apply.html (optional).
        // It belongs to a different customer (self-use prevention enforced in service layer).
        // DiscountAmount is computed and stored on approval (commission discount applied).
        DiscountCode   string   `json:"discount_code,omitempty"`
        DiscountAmount *float64 `json:"discount_amount,omitempty"`

        // PR #116: AZMK Online Lending Service ID-ləri
        KycID           string `json:"kyc_id,omitempty"`           // KYC verify-dən qaytarılan ID
        PartnerID       string `json:"partner_id,omitempty"`       // Partner registration-dan qaytarılan ID
        CardID          string `json:"card_id,omitempty"`          // Card registration-dan qaytarılan ID
        LwApplicationID string `json:"lw_application_id,omitempty"` // Application create-dən qaytarılan ID

        // PR #134: Muraciət üzərində işləmə vaxtı (saniyə)
        TimerSeconds int `json:"timer_seconds,omitempty"`

        // PR #142: Hansı dashboard istifadəçisi tərəfindən təsdiq/redd edilib
        ProcessedByUserID   *int   `json:"processed_by_user_id,omitempty"`
        ProcessedByUsername string `json:"processed_by_username,omitempty"`

        // PR #148: Audit fields — hansı ekspert hansı əməliyyatı etdi
        ContactsUpdatedByUserID   *int   `json:"contacts_updated_by_user_id,omitempty"`
        ContactsUpdatedByUsername string  `json:"contacts_updated_by_username,omitempty"`
        ContactsUpdatedAt         string  `json:"contacts_updated_at,omitempty"`
        TimerUpdatedByUserID      *int   `json:"timer_updated_by_user_id,omitempty"`
        TimerUpdatedByUsername    string  `json:"timer_updated_by_username,omitempty"`
        MyGovCheckedByUserID      *int   `json:"mygov_checked_by_user_id,omitempty"`
        MyGovCheckedByUsername    string  `json:"mygov_checked_by_username,omitempty"`
        MyGovCheckedAt            string  `json:"mygov_checked_at,omitempty"`

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
