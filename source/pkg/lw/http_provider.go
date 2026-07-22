package lw

import (
        "context"
        "fmt"
        "log/slog"
        "net/http"
        "time"
)

// HTTPProvider implements the LW Provider interface by making real HTTP calls
// to the LW system. This is the production implementation — the MockProvider
// is used in dev/test.
//
// All methods follow the same pattern:
//  1. Build the request (URL + query params + JSON body if POST)
//  2. Add the API key header
//  3. Send the request with the configured timeout
//  4. Check the HTTP status code
//  5. Decode the JSON response
//
// Error handling: HTTP 4xx/5xx from LW are wrapped in an error and returned.
// Network errors (timeout, DNS failure) are also returned as errors. The
// caller (credit engine) decides whether to retry or fail-open.
//
// NOTE: The exact endpoint paths and request/response formats depend on the
// LW API documentation, which is not yet available. The paths below are
// placeholders based on the flow diagram. When the real docs arrive, update
// the path constants and any field name mismatches — the handler structure
// stays the same.
type HTTPProvider struct {
        baseURL string
        apiKey  string
        client  *http.Client
}

// NewHTTPProvider creates a new HTTPProvider with the given configuration.
// The timeout is applied to every HTTP call.
func NewHTTPProvider(baseURL, apiKey string, timeout time.Duration) *HTTPProvider {
        return &HTTPProvider{
                baseURL: baseURL,
                apiKey:  apiKey,
                client: &http.Client{
                        Timeout: timeout,
                },
        }
}

// LW API endpoint paths (placeholders — update when real docs arrive).
const (
        pathGetCustomerLoans = "/api/lw/loans"           // GET ?pin=...
        pathSetupCustomerLoans = "/api/lw/loans/setup"   // POST
        pathCheckBlacklist   = "/api/lw/blacklist"       // GET ?fin=...
        pathGetAzmkBlacklist = "/api/router/azmk-blacklist" // GET ?fin=... (PR #53)
        pathGetPersonalInfo  = "/api/router/personal-info" // GET ?fin=...&serial=...
        pathGetAkbScore      = "/api/router/akb-score"   // GET ?fin=...&serial=...
        pathGetAkbHistory    = "/api/router/akb-history" // GET ?fin=...&serial=...
        pathGetAsanFinance   = "/api/router/asan-finance" // GET ?fin=...
        pathInitSimaKyc      = "/api/router/sima/init"   // POST ?application_id=...
        pathApproveLoan      = "/api/lw/loans/approve"   // POST
)

// --- Loan Data ---

// GetCustomerLoans fetches all loans for a customer from LW.
func (p *HTTPProvider) GetCustomerLoans(ctx context.Context, pin string) (*CustomerLoansResponse, error) {
        var resp CustomerLoansResponse
        err := p.getJSON(ctx, pathGetCustomerLoans+"?pin="+pin, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetCustomerLoans: %w", err)
        }
        return &resp, nil
}

// SetupCustomerLoans sets up mock loan data (dev/test only — not available in real LW).
func (p *HTTPProvider) SetupCustomerLoans(ctx context.Context, req *LoanSetupRequest) error {
        return fmt.Errorf("http provider: SetupCustomerLoans is not available in real LW mode (dev/test only)")
}

// --- Blacklist ---

// CheckBlacklist checks if a customer is on the LW blacklist.
type blacklistResponse struct {
        Fin           string `json:"fin"`
        IsBlacklisted bool   `json:"is_blacklisted"`
}

func (p *HTTPProvider) CheckBlacklist(ctx context.Context, fin string) (bool, error) {
        var resp blacklistResponse
        err := p.getJSON(ctx, pathCheckBlacklist+"?fin="+fin, &resp)
        if err != nil {
                return false, fmt.Errorf("http provider: CheckBlacklist: %w", err)
        }
        return resp.IsBlacklisted, nil
}

// --- AZMK Blacklist (PR #53) ---

// azmkBlacklistResponse mirrors the LW router response for AZMK blacklist checks.
// LW forwards the request to the AZMK external service and returns the result.
type azmkBlacklistResponse struct {
        Fin           string `json:"fin"`
        IsBlacklisted bool   `json:"is_blacklisted"`
}

// GetAzmkBlacklist checks if a customer is on the AZMK (Central Credit Register
// of Azerbaijan) blacklist, via the LW router. Rule 5 (PR #53): if on the AZMK
// blacklist, the application must be rejected.
func (p *HTTPProvider) GetAzmkBlacklist(ctx context.Context, fin string) (bool, error) {
        var resp azmkBlacklistResponse
        err := p.getJSON(ctx, pathGetAzmkBlacklist+"?fin="+fin, &resp)
        if err != nil {
                return false, fmt.Errorf("http provider: GetAzmkBlacklist: %w", err)
        }
        return resp.IsBlacklisted, nil
}

// --- External Router ---

// GetPersonalInfo fetches personal info from DIN via LW router.
func (p *HTTPProvider) GetPersonalInfo(ctx context.Context, fin, serial string) (*PersonalInfoResponse, error) {
        var resp PersonalInfoResponse
        err := p.getJSON(ctx, pathGetPersonalInfo+"?fin="+fin+"&serial="+serial, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetPersonalInfo: %w", err)
        }
        return &resp, nil
}

// GetAkbScore fetches the AKB credit score via LW router.
func (p *HTTPProvider) GetAkbScore(ctx context.Context, fin, serial string) (*AkbScoreResponse, error) {
        var resp AkbScoreResponse
        err := p.getJSON(ctx, pathGetAkbScore+"?fin="+fin+"&serial="+serial, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetAkbScore: %w", err)
        }
        return &resp, nil
}

// GetAkbHistory fetches the full AKB credit history via LW router.
func (p *HTTPProvider) GetAkbHistory(ctx context.Context, fin, serial string) (*AkbHistoryResponse, error) {
        var resp AkbHistoryResponse
        err := p.getJSON(ctx, pathGetAkbHistory+"?fin="+fin+"&serial="+serial, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetAkbHistory: %w", err)
        }
        return &resp, nil
}

// GetAsanFinance fetches official income from ASAN Finance via LW router.
func (p *HTTPProvider) GetAsanFinance(ctx context.Context, fin string) (*AsanFinanceResponse, error) {
        var resp AsanFinanceResponse
        err := p.getJSON(ctx, pathGetAsanFinance+"?fin="+fin, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetAsanFinance: %w", err)
        }
        return &resp, nil
}

// InitSimaKyc initiates the SIMA KYC process via LW router.
func (p *HTTPProvider) InitSimaKyc(ctx context.Context, appID int) error {
        url := fmt.Sprintf("%s%s?application_id=%d", p.baseURL, pathInitSimaKyc, appID)
        _, err := p.doRequest(ctx, http.MethodPost, url, nil)
        if err != nil {
                return fmt.Errorf("http provider: InitSimaKyc: %w", err)
        }
        slog.Info("SIMA KYC initiated", "application_id", appID)
        return nil
}

// --- LW Operations ---

// ApproveLoan pushes an approved loan to LW for contract signing and transfer.
func (p *HTTPProvider) ApproveLoan(ctx context.Context, req *ApproveLoanRequest) (*ApproveLoanResponse, error) {
        var resp ApproveLoanResponse
        err := p.postJSON(ctx, pathApproveLoan, req, &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: ApproveLoan: %w", err)
        }
        return &resp, nil
}

// GetLoanStatus fetches the current status of a loan in the LW system.
// Used for polling (fallback when async callbacks don't arrive).
func (p *HTTPProvider) GetLoanStatus(ctx context.Context, appID int) (*LoanStatusResponse, error) {
        var resp LoanStatusResponse
        err := p.getJSON(ctx, fmt.Sprintf("%s/api/lw/loans/%d/status", p.baseURL, appID), &resp)
        if err != nil {
                return nil, fmt.Errorf("http provider: GetLoanStatus: %w", err)
        }
        return &resp, nil
}
