package lw

import (
        "context"
        "database/sql"
        "fmt"
)

// MockProvider implements the LW Provider interface using the local database.
// This replaces the former MockLMS provider and repo.
type MockProvider struct {
        db *sql.DB
}

// NewMockProvider creates a new MockProvider backed by the given database.
func NewMockProvider(db *sql.DB) *MockProvider {
        return &MockProvider{db: db}
}

// GetCustomerLoans retrieves all loans for a customer from the mock_lms_loans table.
func (p *MockProvider) GetCustomerLoans(ctx context.Context, pin string) (*CustomerLoansResponse, error) {
        rows, err := p.db.QueryContext(ctx, `
                SELECT id, customer_pin, scenario_name, lms_loan_id, loan_type, amount, term_months,
                       start_date, end_date, status, remaining_amount, was_on_time, early_completion,
                       delay_days, level_at_close, closed_at
                FROM mock_lms_loans
                WHERE customer_pin = ?`, pin)
        if err != nil {
                return nil, fmt.Errorf("lw mock: failed to query customer loans: %w", err)
        }
        defer rows.Close()

        var loans []CustomerLoan
        for rows.Next() {
                var loan CustomerLoan
                var scenarioName sql.NullString
                var levelAtClose, closedAt sql.NullString
                err := rows.Scan(
                        &loan.ID,
                        &loan.CustomerPIN,
                        &scenarioName,
                        &loan.LmsLoanID,
                        &loan.LoanType,
                        &loan.Amount,
                        &loan.TermMonths,
                        &loan.StartDate,
                        &loan.EndDate,
                        &loan.Status,
                        &loan.RemainingAmount,
                        &loan.WasOnTime,
                        &loan.EarlyCompletion,
                        &loan.DelayDays,
                        &levelAtClose,
                        &closedAt,
                )
                if err != nil {
                        return nil, fmt.Errorf("lw mock: failed to scan loan row: %w", err)
                }
                loan.LevelAtClose = levelAtClose.String
                loan.ClosedAt = closedAt.String
                loans = append(loans, loan)
        }

        if err = rows.Err(); err != nil {
                return nil, fmt.Errorf("lw mock: error iterating loan rows: %w", err)
        }

        return &CustomerLoansResponse{
                CustomerPIN:      pin,
                HasExistingLoans: len(loans) > 0,
                LoanCount:        len(loans),
                Loans:            loans,
        }, nil
}

// SetupCustomerLoans replaces all existing loans for a customer with the new set.
func (p *MockProvider) SetupCustomerLoans(ctx context.Context, req *LoanSetupRequest) error {
        tx, err := p.db.BeginTx(ctx, nil)
        if err != nil {
                return fmt.Errorf("lw mock: failed to begin transaction: %w", err)
        }
        defer tx.Rollback()

        // Delete existing loans for this customer
        _, err = tx.ExecContext(ctx, "DELETE FROM mock_lms_loans WHERE customer_pin = ?", req.CustomerPIN)
        if err != nil {
                return fmt.Errorf("lw mock: failed to delete existing loans: %w", err)
        }

        // Insert new loans
        for _, loan := range req.Loans {
                _, err = tx.ExecContext(ctx, `
                        INSERT INTO mock_lms_loans
                                (customer_pin, scenario_name, lms_loan_id, loan_type, amount, term_months,
                                 start_date, end_date, status, remaining_amount, was_on_time, early_completion,
                                 delay_days, level_at_close, closed_at)
                        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
                        req.CustomerPIN,
                        req.ScenarioName,
                        loan.LmsLoanID,
                        loan.LoanType,
                        loan.Amount,
                        loan.TermMonths,
                        loan.StartDate,
                        loan.EndDate,
                        loan.Status,
                        loan.RemainingAmount,
                        loan.WasOnTime,
                        loan.EarlyCompletion,
                        loan.DelayDays,
                        loan.LevelAtClose,
                        loan.ClosedAt,
                )
                if err != nil {
                        return fmt.Errorf("lw mock: failed to insert loan %s: %w", loan.LmsLoanID, err)
                }
        }

        if err = tx.Commit(); err != nil {
                return fmt.Errorf("lw mock: failed to commit transaction: %w", err)
        }

        return nil
}

// CheckBlacklist checks if a customer is blacklisted.
// Mock implementation: always returns false (not blacklisted).
func (p *MockProvider) CheckBlacklist(ctx context.Context, fin string) (bool, error) {
        // Mock: no blacklist in local DB — always allow
        return false, nil
}

// GetAzmkBlacklist checks if a customer is on the AZMK blacklist (PR #53).
// Mock implementation: always returns false (not on AZMK blacklist).
// In real mode, the HTTPProvider routes this to the AZMK external service via LW.
func (p *MockProvider) GetAzmkBlacklist(ctx context.Context, fin string) (bool, error) {
        // Mock: no AZMK blacklist in local DB — always allow
        return false, nil
}

// GetPersonalInfo returns mock personal info.
// Mock implementation: returns a placeholder response with the FIN echoed back.
// In real mode, the HTTPProvider fetches this from DIN via LW router.
func (p *MockProvider) GetPersonalInfo(ctx context.Context, fin, serial string) (*PersonalInfoResponse, error) {
        return &PersonalInfoResponse{
                Fin:          fin,
                Serial:       serial,
                FullName:     "Mock Customer (FIN: " + fin + ")",
                DateOfBirth:  "1990-01-01",
                PlaceOfBirth: "Baku, Azerbaijan",
                Address:      "Mock Address, Baku",
        }, nil
}

// GetAkbScore returns a mock AKB score.
// Mock implementation: returns score 0 (no override) unless the score is
// pre-configured in the mock_lms_loans table. StopFactors is empty by default.
func (p *MockProvider) GetAkbScore(ctx context.Context, fin, serial string) (*AkbScoreResponse, error) {
        return &AkbScoreResponse{
                Fin:       fin,
                Score:     0, // 0 means "no override" — the engine falls back to the request-supplied score
                QueryDate: "",
        }, nil
}

// GetAkbHistory returns mock AKB history.
// Mock implementation: returns an empty history response with the FIN echoed.
func (p *MockProvider) GetAkbHistory(ctx context.Context, fin, serial string) (*AkbHistoryResponse, error) {
        return &AkbHistoryResponse{
                ReportID:      fmt.Sprintf("MOCK-AKB-%s", fin),
                ReportingDate: "2026-01-01",
                Borrower: AkbBorrower{
                        Fin:   fin,
                        Name:  "Mock Customer",
                        Status: "active",
                },
                Liabilities:    []AkbLiability{},
                InquiryHistory: []AkbInquiry{},
                Balance:        0,
        }, nil
}

// GetAsanFinance returns mock income data.
// Mock implementation: returns a placeholder income response.
func (p *MockProvider) GetAsanFinance(ctx context.Context, fin string) (*AsanFinanceResponse, error) {
        return &AsanFinanceResponse{
                Fin:            fin,
                OfficialIncome: 0,
                Currency:       "AZN",
                EmployerName:   "Mock Employer",
                QueryDate:      "2026-01-01",
        }, nil
}

// InitSimaKyc initiates SIMA KYC process.
// Mock implementation: logs and returns nil (success). In real mode, the
// HTTPProvider sends an init request to SIMA via LW router.
func (p *MockProvider) InitSimaKyc(ctx context.Context, appID int) error {
        // Mock: no-op success. Real implementation will call LW's SIMA init endpoint.
        return nil
}

// ApproveLoan sends an approval request to LW.
// Mock implementation: returns a mock success response.
func (p *MockProvider) ApproveLoan(ctx context.Context, req *ApproveLoanRequest) (*ApproveLoanResponse, error) {
        // Mock: simulate successful approval
        return &ApproveLoanResponse{
                ApplicationID:  req.ApplicationID,
                ContractStatus: "signed",
                TransferStatus: "completed",
                LmsLoanID:      fmt.Sprintf("LW-MOCK-%d", req.ApplicationID),
        }, nil
}

// GetLoanStatus fetches the current status of a loan in the LW system.
// Mock implementation: always returns "completed" status.
func (p *MockProvider) GetLoanStatus(_ context.Context, appID int) (*LoanStatusResponse, error) {
        return &LoanStatusResponse{
                ApplicationID:  appID,
                ContractStatus: "signed",
                TransferStatus: "completed",
                LmsLoanID:      fmt.Sprintf("LW-MOCK-%d", appID),
        }, nil
}
