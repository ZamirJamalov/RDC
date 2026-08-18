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

	// GetEmployeeInfoByPin retrieves the customer's employment records from
	// the MLSA (Məşğulluq) service by PIN. Used by the EMPLOYMENT_TENURE
	// cutoff check (PR #237): Active[].Contract.SignDate → today duration
	// is compared against the 6-month threshold.
	GetEmployeeInfoByPin(ctx context.Context, pin string) (*EmployeeInfoResponse, error)

	// Name returns a human-readable identifier ("mock", "mygov-http").
	Name() string
}

// PermissionLink contains the URL and token for customer permission.
type PermissionLink struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
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

// --- PR #237: GetEmployeeInfoByPin (MLSA employment records) ---

// EmployeeInfoResponse mirrors the real MLSA GetEmployeeInfoByPin response.
// See PR #237: EMPLOYMENT_TENURE cutoff checks Active[].Contract.SignDate.
//
// Example (abridged):
//
//	{
//	  "result": 1, "requestId": "1", "message": "SUCCESS",
//	  "data": {
//		"Status": {"Name": "Successful", "Code": 0, "Message": ""},
//		"Response": {
//		  "Active": [{
//			"Employer": {"Voen": "1701618531", "Name": "..."},
//			"Employee": {"WorkPlaceType": {"Label": "1", "Description": "Əsas"}, ...},
//			"Contract": {"SignDate": "01.07.2026", "BeginDate": "01.07.2026", "EndDate": "01.10.2026", ...}
//		  }],
//		  "Deactive": [...]
//		}
//	  }
//	}
type EmployeeInfoResponse struct {
	Result    int               `json:"result"`
	RequestID string            `json:"requestId"`
	Message   string            `json:"message"`
	Data      *EmployeeInfoData `json:"data"`
}

// EmployeeInfoData wraps the Status + Response fields.
type EmployeeInfoData struct {
	Status   *EmployeeStatus  `json:"Status"`
	Response *EmployeeRecords `json:"Response"`
}

// EmployeeStatus is the MLSA service status block.
type EmployeeStatus struct {
	Name    string `json:"Name"`
	Code    int    `json:"Code"`
	Message string `json:"Message"`
}

// EmployeeRecords contains the active and deactive employment records.
type EmployeeRecords struct {
	Active   []EmploymentRecord `json:"Active"`
	Deactive []EmploymentRecord `json:"Deactive"`
}

// EmploymentRecord is a single workplace entry (employer + employee + contract).
type EmploymentRecord struct {
	Employer *EmployerInfo `json:"Employer"`
	Employee *EmployeeInfo `json:"Employee"`
	Contract *ContractInfo `json:"Contract"`
}

// EmployerInfo describes the employer (VOEN, name, address, ...).
type EmployerInfo struct {
	Voen         string            `json:"Voen"`
	Name         string            `json:"Name"`
	WorkerCount  int               `json:"WorkerCount"`
	LegalAddress string            `json:"LegalAddress"`
	PropertyType *LabelDescription `json:"PropertyType"`
}

// EmployeeInfo describes the employee-side details of the workplace.
type EmployeeInfo struct {
	WorkPlaceType          *LabelDescription `json:"WorkPlaceType"` // Label "1" = Əsas (main job)
	Salary                 float64           `json:"Salary"`
	Surname                string            `json:"Surname"`
	WorkPlace              string            `json:"WorkPlace"`
	Position               string            `json:"Position"`
	WorkCasualType         *LabelDescription `json:"WorkCasualType"`
	Phone                  string            `json:"Phone"`
	Patronymic             string            `json:"Patronymic"`
	Name                   string            `json:"Name"`
	PositionLabourContract string            `json:"PositionLabourContract"`
	SSN                    string            `json:"SSN"`
}

// ContractInfo describes the labour contract. SignDate (imza tarixi) is the
// anchor for the EMPLOYMENT_TENURE check; BeginDate is the fallback.
type ContractInfo struct {
	Invalidation  *LabelDescription `json:"Invalidation"`
	EndDate       string            `json:"EndDate"` // "01.10.2026" (dd.mm.yyyy)
	Number        string            `json:"Number"`
	Status        *LabelDescription `json:"Status"` // Label "1" = Qüvvədədir
	PeriodType    *LabelDescription `json:"PeriodType"`
	SignDate      string            `json:"SignDate"` // "01.07.2026" (dd.mm.yyyy)
	NextEndDate   string            `json:"NextEndDate"`
	InsertDate    string            `json:"InsertDate"`
	BeginDate     string            `json:"BeginDate"` // "01.07.2026" (dd.mm.yyyy)
	TerminateDate string            `json:"TerminateDate"`
}

// LabelDescription is a generic MLSA {"Label": "1", "Description": "..."} pair.
type LabelDescription struct {
	Label       string `json:"Label"`
	Description string `json:"Description"`
}

// --- PR #242: GetPensionInfoByPin (pension/disability records) ---

// PensionInfoResponse mirrors the GetPensionInfoByPin response envelope.
// Used by the DISABILITY_GROUP1 cutoff check: DisabilityGroup == 1 → auto-reject.
//
// Example (abridged):
//
//	{
//	  "result": 1, "requestId": "1", "message": "SUCCESS",
//	  "data": {
//	    "Response": {
//		"DisabilityGroup": 0,
//		"IsPensioner": false,
//		"PensionType": ""
//	    }
//	  }
//	}
type PensionInfoResponse struct {
	Result    int              `json:"result"`
	RequestID string           `json:"requestId"`
	Message   string           `json:"message"`
	Data      *PensionInfoData `json:"data"`
}

// PensionInfoData wraps the Response field.
type PensionInfoData struct {
	Response *PensionRecord `json:"Response"`
}

// PensionRecord holds the disability/pension status used by DISABILITY_GROUP1.
type PensionRecord struct {
	DisabilityGroup int    `json:"DisabilityGroup"` // 0 = yoxdur, 1 = 1-ci qrup (imtina), 2/3 = digər qruplar
	IsPensioner     bool   `json:"IsPensioner"`
	PensionType     string `json:"PensionType"` // "age", "disability", "survivor"
}

// PensionInfoFromAuthorizedData builds a PensionInfoResponse from stored MyGov
// AuthorizedData — fallback path when the AZMK pension service is unavailable
// (PR #242: permission-token axını tələb olunmadan PIN üzərindən yoxlama).
func PensionInfoFromAuthorizedData(data *AuthorizedData) *PensionInfoResponse {
	if data == nil {
		return nil
	}
	return &PensionInfoResponse{
		Result:  1,
		Message: "SUCCESS",
		Data: &PensionInfoData{
			Response: &PensionRecord{
				DisabilityGroup: data.DisabilityGroup,
				IsPensioner:     data.IsPensioner,
				PensionType:     data.PensionType,
			},
		},
	}
}

// IsMainJob reports whether the record is the main (Əsas) workplace —
// WorkPlaceType.Label == "1".
func (r *EmploymentRecord) IsMainJob() bool {
	return r != nil && r.Employee != nil && r.Employee.WorkPlaceType != nil && r.Employee.WorkPlaceType.Label == "1"
}
