package service

import (
        "context"
        "fmt"

        "rdc-source/pkg/lw"
)

// mockLWProvider is a test-only implementation of lw.Provider.
// Each method returns a configurable value, allowing tests to inject
// specific customer loan histories, AKB scores, blacklist states, etc.
// without making real HTTP calls.
//
// Only GetCustomerLoans is exercised by the current service layer; the
// other methods exist to satisfy the lw.Provider interface and return
// sensible defaults (no error, empty result).
type mockLWProvider struct {
        // GetCustomerLoans
        loans    *lw.CustomerLoansResponse
        loansErr error

        // SetupCustomerLoans
        setupErr error

        // CheckBlacklist
        blacklisted    bool
        blacklistErr   error

        // GetAkbScore
        akbScore    *lw.AkbScoreResponse
        akbScoreErr error

        // GetPersonalInfo
        personalInfo    *lw.PersonalInfoResponse
        personalInfoErr error

        // GetAkbHistory
        akbHistory    *lw.AkbHistoryResponse
        akbHistoryErr error

        // ApproveLoan (T-1.1)
        approveLoanResp    *lw.ApproveLoanResponse
        approveLoanErr     error
        approveLoanCalls   []lw.ApproveLoanRequest // recording

        // Other router endpoints — return errors by default (mock mode)
        // Tests that need these should set the corresponding field.
}

func (m *mockLWProvider) GetCustomerLoans(_ context.Context, _ string) (*lw.CustomerLoansResponse, error) {
        if m.loansErr != nil {
                return nil, m.loansErr
        }
        if m.loans == nil {
                return &lw.CustomerLoansResponse{Loans: nil}, nil
        }
        return m.loans, nil
}

func (m *mockLWProvider) SetupCustomerLoans(_ context.Context, _ *lw.LoanSetupRequest) error {
        return m.setupErr
}

func (m *mockLWProvider) CheckBlacklist(_ context.Context, _ string) (bool, error) {
        return m.blacklisted, m.blacklistErr
}

func (m *mockLWProvider) GetAkbScore(_ context.Context, fin, _ string) (*lw.AkbScoreResponse, error) {
        if m.akbScoreErr != nil {
                return nil, m.akbScoreErr
        }
        if m.akbScore == nil {
                return &lw.AkbScoreResponse{Fin: fin, Score: 0}, nil
        }
        return m.akbScore, nil
}

// --- Stubs for unused router methods (return errors to make it obvious if a test
// accidentally exercises them without configuring a return value) ---

func (m *mockLWProvider) GetPersonalInfo(_ context.Context, fin, _ string) (*lw.PersonalInfoResponse, error) {
        if m.personalInfoErr != nil {
                return nil, m.personalInfoErr
        }
        if m.personalInfo == nil {
                // Default: return a 30-year-old customer (born 1996-01-01) so age check passes.
                // Tests that need a different age should set m.personalInfo explicitly.
                return &lw.PersonalInfoResponse{
                        Fin:         fin,
                        FullName:    "Mock Customer",
                        DateOfBirth: "1996-01-01",
                }, nil
        }
        return m.personalInfo, nil
}

func (m *mockLWProvider) GetAkbHistory(_ context.Context, fin, _ string) (*lw.AkbHistoryResponse, error) {
        if m.akbHistoryErr != nil {
                return nil, m.akbHistoryErr
        }
        if m.akbHistory == nil {
                // Default: empty AKB history (no liabilities) so existing tests pass.
                return &lw.AkbHistoryResponse{
                        ReportID:      fmt.Sprintf("MOCK-AKB-%s", fin),
                        ReportingDate: "2026-01-01",
                        Borrower:      lw.AkbBorrower{Fin: fin, Name: "Mock Customer", Status: "active"},
                        Liabilities:   []lw.AkbLiability{},
                }, nil
        }
        return m.akbHistory, nil
}

func (m *mockLWProvider) GetAsanFinance(_ context.Context, _ string) (*lw.AsanFinanceResponse, error) {
        return nil, errMockNotConfigured
}

func (m *mockLWProvider) InitSimaKyc(_ context.Context, _ int) error {
        return errMockNotConfigured
}

func (m *mockLWProvider) ApproveLoan(_ context.Context, req *lw.ApproveLoanRequest) (*lw.ApproveLoanResponse, error) {
        m.approveLoanCalls = append(m.approveLoanCalls, *req)
        if m.approveLoanErr != nil {
                return nil, m.approveLoanErr
        }
        if m.approveLoanResp == nil {
                return &lw.ApproveLoanResponse{
                        ApplicationID:  req.ApplicationID,
                        ContractStatus: "signed",
                        TransferStatus: "completed",
                        LmsLoanID:      "MOCK-LMS-001",
                }, nil
        }
        return m.approveLoanResp, nil
}

// GetLoanStatus returns a mock loan status (always completed).
func (m *mockLWProvider) GetLoanStatus(_ context.Context, appID int) (*lw.LoanStatusResponse, error) {
        return &lw.LoanStatusResponse{
                ApplicationID:  appID,
                ContractStatus: "signed",
                TransferStatus: "completed",
                LmsLoanID:      "MOCK-LMS-001",
        }, nil
}

// errMockNotConfigured is returned by mockLWProvider methods that have not
// been explicitly configured with a return value. This makes test failures
// obvious (a clear error rather than a silent nil).
var errMockNotConfigured = errMockNotConfiguredSentinel{}

type errMockNotConfiguredSentinel struct{}

func (errMockNotConfiguredSentinel) Error() string {
        return "mock LW provider: method not configured for this test"
}

// newMockLWProvider returns a mockLWProvider with safe defaults:
// no loans, no errors, not blacklisted, AKB score 0.
func newMockLWProvider() *mockLWProvider {
        return &mockLWProvider{}
}

// withLoans is a builder-style helper to set up the loans returned by
// GetCustomerLoans. Returns the receiver for chaining.
func (m *mockLWProvider) withLoans(loans []lw.CustomerLoan) *mockLWProvider {
        m.loans = &lw.CustomerLoansResponse{
                CustomerPIN:      "MOCK",
                HasExistingLoans: len(loans) > 0,
                LoanCount:        len(loans),
                Loans:            loans,
        }
        return m
}
