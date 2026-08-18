package mygov

import (
        "context"
        "fmt"
        "log/slog"
        "strings"
        "time"
)

// MockProvider implements the MyGov Provider interface by returning canned
// responses. Used in dev/test environments.
type MockProvider struct{}

// NewMockProvider creates a new MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// GeneratePermissionLink returns a mock permission URL.
func (p *MockProvider) GeneratePermissionLink(_ context.Context, fin string) (*PermissionLink, error) {
        token := fmt.Sprintf("MOCK-MYGOV-%s-%d", fin, time.Now().Unix())
        slog.Info("mock MyGov permission link generated", "fin", fin, "token", token)
        return &PermissionLink{
                Token:     token,
                URL:       fmt.Sprintf("https://mock-mygov.example.com/permit/%s", token),
                ExpiresAt: time.Now().Add(30 * time.Minute),
        }, nil
}

// FetchAuthorizedData returns mock official data.
// PR #64: includes sample WorkHistory and pension fields.
func (p *MockProvider) FetchAuthorizedData(_ context.Context, token string) (*AuthorizedData, error) {
        slog.Info("mock MyGov data fetched", "token", token)
        now := time.Now()
        // Simulate a customer who started their current job 8 months ago
        // (passes the 6-month tenure rule in PR #65).
        currentJobStart := now.AddDate(0, -8, 0)
        return &AuthorizedData{
                Fin:            "MOCK",
                FullName:       "Mock Customer",
                OfficialIncome: 1500.0,
                EmployerName:   "Mock Employer LLC",
                Address:        "Mock Address, Baku",
                FetchedAt:      now,
                WorkHistory: []WorkPlace{
                        {
                                EmployerName: "Mock Employer LLC",
                                StartDate:    currentJobStart,
                                EndDate:      nil, // currently employed
                                Position:     "Software Engineer",
                        },
                },
                DisabilityGroup: 0, // no disability
                IsPensioner:     false,
                PensionType:     "",
        }, nil
}

// Name returns "mock".
func (p *MockProvider) Name() string { return "mock" }

// GetEmployeeInfoByPin returns mock MLSA employment records (PR #237).
// FIN-based scenarios (like the AZMK customer-data mock):
//
//	default            → 1 active record, SignDate 8 months ago (passes 6-month rule)
//	PIN starts NOJOB   → Active empty (no workplace info → reject)
//	PIN starts SHORT   → 1 active record, SignDate 3 months ago (reject)
//	PIN starts OLDJOB  → 1 active record, SignDate 3 months ago + 1 deactive
//	                     record that ended recently (for UI inspection)
func (p *MockProvider) GetEmployeeInfoByPin(_ context.Context, pin string) (*EmployeeInfoResponse, error) {
        slog.Info("mock MyGov GetEmployeeInfoByPin", "pin", pin)
        now := time.Now()

        switch {
        case strings.HasPrefix(pin, "NOJOB"):
                return &EmployeeInfoResponse{
                        Result: 1, RequestID: "1", Message: "SUCCESS",
                        Data: &EmployeeInfoData{
                                Status:   &EmployeeStatus{Name: "Successful", Code: 0},
                                Response: &EmployeeRecords{Active: nil, Deactive: nil},
                        },
                }, nil
        case strings.HasPrefix(pin, "SHORT"):
                return &EmployeeInfoResponse{
                        Result: 1, RequestID: "1", Message: "SUCCESS",
                        Data: &EmployeeInfoData{
                                Status: &EmployeeStatus{Name: "Successful", Code: 0},
                                Response: &EmployeeRecords{
                                        Active: []EmploymentRecord{
                                                mockActiveRecord(now.AddDate(0, -3, 0)),
                                        },
                                },
                        },
                }, nil
        default:
                return &EmployeeInfoResponse{
                        Result: 1, RequestID: "1", Message: "SUCCESS",
                        Data: &EmployeeInfoData{
                                Status: &EmployeeStatus{Name: "Successful", Code: 0},
                                Response: &EmployeeRecords{
                                        Active: []EmploymentRecord{
                                                mockActiveRecord(now.AddDate(0, -8, 0)),
                                        },
                                },
                        },
                }, nil
        }
}

// mockActiveRecord builds a realistic Active employment record with the given
// contract SignDate/BeginDate (dd.mm.yyyy format, like the real MLSA response).
func mockActiveRecord(signDate time.Time) EmploymentRecord {
        endDate := signDate.AddDate(0, 6, 0) // Müddətli contract: +6 months
        return EmploymentRecord{
                Employer: &EmployerInfo{
                        Voen:         "1701618531",
                        Name:         "MOCK ŞİRKƏT MMC",
                        WorkerCount:  98,
                        LegalAddress: "BAKI ŞƏHƏRİ, NİZAMİ, ev 68",
                },
                Employee: &EmployeeInfo{
                        WorkPlaceType: &LabelDescription{Label: "1", Description: "Əsas"},
                        Salary:        1500.0,
                        Surname:       "MOCKLU",
                        Name:          "MOCK",
                        Patronymic:    "MOCK OĞLU",
                        Position:      "Aparıcı mütəxəssis",
                        WorkPlace:     "MOCK ŞİRKƏT MMC",
                        SSN:           "0000000000000",
                },
                Contract: &ContractInfo{
                        SignDate:   signDate.Format("02.01.2006"),
                        BeginDate:  signDate.Format("02.01.2006"),
                        EndDate:    endDate.Format("02.01.2006"),
                        Number:     "MOCK-0001",
                        Status:     &LabelDescription{Label: "1", Description: "Qüvvədədir"},
                        PeriodType: &LabelDescription{Label: "0", Description: "Müddətli"},
                },
        }
}
