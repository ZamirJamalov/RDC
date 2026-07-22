package handler

import (
        "context"
        "encoding/json"
        "errors"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "rdc-source/pkg/lw"
)

// mockLWProviderForHandler is a minimal mock for testing LWRouterHandler.
// It implements only the methods the handler calls, returning configurable
// responses. (The full mockLWProvider in the service package can't be reused
// here because it's in a different package.)
type mockLWProviderForHandler struct {
        personalInfo    *lw.PersonalInfoResponse
        personalInfoErr error

        akbScore    *lw.AkbScoreResponse
        akbScoreErr error

        akbHistory    *lw.AkbHistoryResponse
        akbHistoryErr error

        blacklisted    bool
        blacklistErr   error

        // AZMK blacklist (PR #53)
        azmkBlacklisted    bool
        azmkBlacklistErr   error

        asanFinance    *lw.AsanFinanceResponse
        asanFinanceErr error

        simaInitErr error

        approveResp    *lw.ApproveLoanResponse
        approveErr     error
}

func (m *mockLWProviderForHandler) GetCustomerLoans(_ context.Context, _ string) (*lw.CustomerLoansResponse, error) {
        return nil, nil
}
func (m *mockLWProviderForHandler) SetupCustomerLoans(_ context.Context, _ *lw.LoanSetupRequest) error {
        return nil
}
func (m *mockLWProviderForHandler) CheckBlacklist(_ context.Context, _ string) (bool, error) {
        return m.blacklisted, m.blacklistErr
}
func (m *mockLWProviderForHandler) GetAzmkBlacklist(_ context.Context, _ string) (bool, error) {
        return m.azmkBlacklisted, m.azmkBlacklistErr
}
func (m *mockLWProviderForHandler) GetPersonalInfo(_ context.Context, _, _ string) (*lw.PersonalInfoResponse, error) {
        return m.personalInfo, m.personalInfoErr
}
func (m *mockLWProviderForHandler) GetAkbScore(_ context.Context, _, _ string) (*lw.AkbScoreResponse, error) {
        return m.akbScore, m.akbScoreErr
}
func (m *mockLWProviderForHandler) GetAkbHistory(_ context.Context, _, _ string) (*lw.AkbHistoryResponse, error) {
        return m.akbHistory, m.akbHistoryErr
}
func (m *mockLWProviderForHandler) GetAsanFinance(_ context.Context, _ string) (*lw.AsanFinanceResponse, error) {
        return m.asanFinance, m.asanFinanceErr
}
func (m *mockLWProviderForHandler) InitSimaKyc(_ context.Context, _ int) error {
        return m.simaInitErr
}
func (m *mockLWProviderForHandler) ApproveLoan(_ context.Context, _ *lw.ApproveLoanRequest) (*lw.ApproveLoanResponse, error) {
        return m.approveResp, m.approveErr
}

func (m *mockLWProviderForHandler) GetLoanStatus(_ context.Context, appID int) (*lw.LoanStatusResponse, error) {
        return &lw.LoanStatusResponse{ApplicationID: appID, ContractStatus: "signed", TransferStatus: "completed"}, nil
}

// --- PersonalInfo tests ---

func TestLWRouterHandler_PersonalInfo_Success(t *testing.T) {
        provider := &mockLWProviderForHandler{
                personalInfo: &lw.PersonalInfoResponse{Fin: "ABC123", FullName: "Test User"},
        }
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/router/personal-info?fin=ABC123&serial=ID123", nil)
        w := httptest.NewRecorder()

        h.PersonalInfo(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp lw.PersonalInfoResponse
        if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
                t.Fatalf("failed to decode response: %v", err)
        }
        if resp.Fin != "ABC123" {
                t.Errorf("Fin = %q, want ABC123", resp.Fin)
        }
}

func TestLWRouterHandler_PersonalInfo_MissingFin(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("GET", "/api/router/personal-info", nil)
        w := httptest.NewRecorder()

        h.PersonalInfo(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

func TestLWRouterHandler_PersonalInfo_ProviderError(t *testing.T) {
        provider := &mockLWProviderForHandler{
                personalInfoErr: errors.New("LW unreachable"),
        }
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/router/personal-info?fin=ABC123", nil)
        w := httptest.NewRecorder()

        h.PersonalInfo(w, req)

        if w.Code != http.StatusBadGateway {
                t.Errorf("status = %d, want %d (BadGateway)", w.Code, http.StatusBadGateway)
        }
}

// --- AkbScore tests ---

func TestLWRouterHandler_AkbScore_Success(t *testing.T) {
        provider := &mockLWProviderForHandler{
                akbScore: &lw.AkbScoreResponse{Fin: "ABC123", Score: 750},
        }
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/router/akb-score?fin=ABC123", nil)
        w := httptest.NewRecorder()

        h.AkbScore(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp lw.AkbScoreResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if resp.Score != 750 {
                t.Errorf("Score = %d, want 750", resp.Score)
        }
}

func TestLWRouterHandler_AkbScore_MissingFin(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("GET", "/api/router/akb-score", nil)
        w := httptest.NewRecorder()

        h.AkbScore(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// --- Blacklist tests ---

func TestLWRouterHandler_Blacklist_NotBlacklisted(t *testing.T) {
        provider := &mockLWProviderForHandler{blacklisted: false}
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/lw/blacklist?fin=ABC123", nil)
        w := httptest.NewRecorder()

        h.Blacklist(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp map[string]interface{}
        json.NewDecoder(w.Body).Decode(&resp)
        if resp["is_blacklisted"] != false {
                t.Errorf("is_blacklisted = %v, want false", resp["is_blacklisted"])
        }
}

func TestLWRouterHandler_Blacklist_IsBlacklisted(t *testing.T) {
        provider := &mockLWProviderForHandler{blacklisted: true}
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/lw/blacklist?fin=ABC123", nil)
        w := httptest.NewRecorder()

        h.Blacklist(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp map[string]interface{}
        json.NewDecoder(w.Body).Decode(&resp)
        if resp["is_blacklisted"] != true {
                t.Errorf("is_blacklisted = %v, want true", resp["is_blacklisted"])
        }
}

func TestLWRouterHandler_Blacklist_MissingFin(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("GET", "/api/lw/blacklist", nil)
        w := httptest.NewRecorder()

        h.Blacklist(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// --- AsanFinance tests ---

func TestLWRouterHandler_AsanFinance_Success(t *testing.T) {
        provider := &mockLWProviderForHandler{
                asanFinance: &lw.AsanFinanceResponse{Fin: "ABC123", OfficialIncome: 1500.0},
        }
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("GET", "/api/router/asan-finance?fin=ABC123", nil)
        w := httptest.NewRecorder()

        h.AsanFinance(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp lw.AsanFinanceResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if resp.OfficialIncome != 1500.0 {
                t.Errorf("OfficialIncome = %v, want 1500", resp.OfficialIncome)
        }
}

// --- ApproveLoan tests ---

func TestLWRouterHandler_ApproveLoan_Success(t *testing.T) {
        provider := &mockLWProviderForHandler{
                approveResp: &lw.ApproveLoanResponse{
                        ApplicationID:  42,
                        ContractStatus: "signed",
                        TransferStatus: "completed",
                        LmsLoanID:      "LMS-001",
                },
        }
        h := NewLWRouterHandler(provider)

        body := `{"application_id":42,"amount":500,"credit_level":"elite","term_months":6}`
        req := httptest.NewRequest("POST", "/api/lw/loans/approve", strings.NewReader(body))
        w := httptest.NewRecorder()

        h.ApproveLoan(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp lw.ApproveLoanResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if resp.ContractStatus != "signed" {
                t.Errorf("ContractStatus = %q, want signed", resp.ContractStatus)
        }
}

func TestLWRouterHandler_ApproveLoan_InvalidBody(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("POST", "/api/lw/loans/approve", strings.NewReader("not json"))
        w := httptest.NewRecorder()

        h.ApproveLoan(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

func TestLWRouterHandler_ApproveLoan_MissingApplicationID(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        body := `{"amount":500,"credit_level":"elite","term_months":6}`
        req := httptest.NewRequest("POST", "/api/lw/loans/approve", strings.NewReader(body))
        w := httptest.NewRecorder()

        h.ApproveLoan(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// --- SimaInit tests ---

func TestLWRouterHandler_SimaInit_Success(t *testing.T) {
        provider := &mockLWProviderForHandler{}
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("POST", "/api/router/sima/init?application_id=42", nil)
        w := httptest.NewRecorder()

        h.SimaInit(w, req)

        if w.Code != http.StatusOK {
                t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var resp map[string]interface{}
        json.NewDecoder(w.Body).Decode(&resp)
        if resp["status"] != "initiated" {
                t.Errorf("status = %v, want initiated", resp["status"])
        }
}

func TestLWRouterHandler_SimaInit_MissingAppID(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("POST", "/api/router/sima/init", nil)
        w := httptest.NewRecorder()

        h.SimaInit(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

func TestLWRouterHandler_SimaInit_InvalidAppID(t *testing.T) {
        h := NewLWRouterHandler(&mockLWProviderForHandler{})

        req := httptest.NewRequest("POST", "/api/router/sima/init?application_id=abc", nil)
        w := httptest.NewRecorder()

        h.SimaInit(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

func TestLWRouterHandler_SimaInit_ProviderError(t *testing.T) {
        provider := &mockLWProviderForHandler{simaInitErr: errors.New("SIMA service unavailable")}
        h := NewLWRouterHandler(provider)

        req := httptest.NewRequest("POST", "/api/router/sima/init?application_id=42", nil)
        w := httptest.NewRecorder()

        h.SimaInit(w, req)

        if w.Code != http.StatusBadGateway {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
        }
}
