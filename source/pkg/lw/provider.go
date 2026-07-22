package lw

import "context"

// Provider defines the unified interface for all LW interactions.
// In mock mode, data comes from the local database.
// In real mode, data comes from the LW system via HTTP.
type Provider interface {
        // --- Loan Data (formerly MockLMS) ---
        GetCustomerLoans(ctx context.Context, pin string) (*CustomerLoansResponse, error)
        SetupCustomerLoans(ctx context.Context, req *LoanSetupRequest) error

        // --- Blacklist (LW own database) ---
        CheckBlacklist(ctx context.Context, fin string) (bool, error)

        // --- AZMK Blacklist (Central Credit Register of Azerbaijan) ---
        // Routed via LW to the AZMK external service. Rule 5 (PR #53):
        // if a customer is on the AZMK blacklist, the application must be rejected.
        GetAzmkBlacklist(ctx context.Context, fin string) (bool, error)

        // --- External Router (LW routes to external services) ---
        GetPersonalInfo(ctx context.Context, fin, serial string) (*PersonalInfoResponse, error)
        GetAkbScore(ctx context.Context, fin, serial string) (*AkbScoreResponse, error)
        GetAkbHistory(ctx context.Context, fin, serial string) (*AkbHistoryResponse, error)
        GetAsanFinance(ctx context.Context, fin string) (*AsanFinanceResponse, error)
        InitSimaKyc(ctx context.Context, appID int) error

        // --- LW Operations ---
        ApproveLoan(ctx context.Context, req *ApproveLoanRequest) (*ApproveLoanResponse, error)

        // GetLoanStatus fetches the current status of a loan in the LW system.
        // Used for polling (fallback when async callbacks don't arrive).
        GetLoanStatus(ctx context.Context, applicationID int) (*LoanStatusResponse, error)
}
