package azmk

import (
        "context"
        "crypto/tls"
        "database/sql"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "log/slog"
        "net/http"
        "strings"
        "time"
)

// ============================================================
// PR #116: AZMK Online Lending Service Provider
// ============================================================
//
// AZMK Online Lending Service inteqrasiyası. Bu provider aşağıdakı
// əməliyyatları dəstəkləyir:
//
//   1. KYC           — POST /kyc (PartnerData göndər → KYC ID qaytar)
//   2. KYC Verify    — GET  /kyc/{id} (VERIFIED status yoxla)
//   3. Partner       — POST /partner (PartnerData + kycId göndər → Partner ID qaytar)
//   4. Card          — POST /card (CardData göndər → Card ID qaytar)
//   5. App Create    — POST /application/create (LoanData göndər → Application ID qaytar)
//   6. Sign          — GET  /application/{id}/sign (already signed yoxla)
//   7. Disburse      — POST /application/disburse (LoanData + cardId göndər)
//
// Base URL nümunə: https://web.azmk.az:7077/LW_CREDIT_HOUSE/services/OnlineLendingService

// Provider is the interface for AZMK Online Lending operations.
type Provider interface {
        // KYC creates a KYC session and returns the KYC ID.
        KYC(ctx context.Context, req *KYCRequest) (string, error)

        // VerifyKYC checks if the KYC session is verified.
        VerifyKYC(ctx context.Context, kycID string) (bool, error)

        // RegisterPartner registers a partner and returns the Partner ID.
        RegisterPartner(ctx context.Context, req *PartnerRequest) (string, error)

        // RegisterCard registers a card and returns the Card ID.
        RegisterCard(ctx context.Context, req *CardRequest) (string, error)

        // CreateApplication creates a loan application and returns the Application ID.
        CreateApplication(ctx context.Context, req *ApplicationCreateRequest) (string, error)

        // CheckSign checks if the application contract is signed.
        CheckSign(ctx context.Context, applicationID string) (bool, error)

        // Disburse disburses the loan to the customer's card.
        Disburse(ctx context.Context, req *DisburseRequest) error
}

// ============================================================
// Request/Response Models
// ============================================================

// PartnerData is the common payload for KYC and Partner requests.
type PartnerData struct {
        AsanFinanceEmployeeInfo bool   `json:"asanfinanceEmployeeInfo"`
        AsanFinancePersonalInfo bool   `json:"asanfinancePersonalInfo"`
        FirstName               string `json:"firstName"`
        LastName                string `json:"lastName"`
        Mkr                     bool   `json:"mkr"`
        Mobile                  string `json:"mobile"`
        Pin                     string `json:"pin"`
        BranchCode              string `json:"branchCode"`
        Passport                string `json:"passport"`
        HomeAddress             string `json:"homeAddress"`
        // KycID is only used for Partner registration (not KYC).
        KycID string `json:"kycId,omitempty"`
}

// KYCRequest is the body for POST /kyc.
type KYCRequest struct {
        PartnerData PartnerData `json:"PartnerData"`
}

// PartnerRequest is the body for POST /partner.
type PartnerRequest struct {
        PartnerData PartnerData `json:"PartnerData"`
}

// CardData is the payload for card registration.
type CardData struct {
        PartnerID string `json:"partnerId"`
        Code      string `json:"code"`     // 16-digit card number
        Expiring  string `json:"expiring"` // "2030-01-01" (always)
}

// CardRequest is the body for POST /card.
type CardRequest struct {
        CardData CardData `json:"CardData"`
}

// LoanData is the payload for application create and disburse.
type LoanData struct {
        ClientID       string  `json:"clientId"`       // Partner ID
        ProductID      string  `json:"productId"`      // config-dən (məs. "L07")
        Amount         float64 `json:"amount"`         // total_amount (principal + commission)
        Term           int     `json:"term"`           // months
        BranchCode     string  `json:"branchCode"`     // config-dən (məs. "HO")
        InterestRate   float64 `json:"interestRate"`   // annual_interest_rate (məs. 55)
        DisbursementFee float64 `json:"disbursementFee"` // 0 (həmişə)
        LetterNumber   string  `json:"letterNumber"`   // boş
        // Disburse üçün:
        ApplicationID string `json:"applicationId,omitempty"` // Application create-dən qaytarılan ID
        CardID        string `json:"cardId,omitempty"`        // Card registration-dan qaytarılan ID
}

// ApplicationCreateRequest is the body for POST /application/create.
type ApplicationCreateRequest struct {
        LoanData LoanData `json:"LoanData"`
}

// DisburseRequest is the body for POST /application/disburse.
type DisburseRequest struct {
        LoanData LoanData `json:"LoanData"`
}

// ============================================================
// HTTP Provider
// ============================================================

// HTTPProvider implements the AZMK Provider interface via real HTTP calls.
type HTTPProvider struct {
        baseURL    string
        username   string
        password   string
        timeout    time.Duration
        httpClient *http.Client
        // PR #163: audit log
        auditDB  *sql.DB
        appID    *int
}

// NewHTTPProvider creates a new AZMK HTTPProvider.
// PR #116: HTTPS with self-signed cert support (InsecureSkipVerify).
// PR #123: Basic Auth (username + password) dəstəyi.
func NewHTTPProvider(baseURL, username, password string, timeoutS int) *HTTPProvider {
        timeout := time.Duration(timeoutS) * time.Second
        return &HTTPProvider{
                baseURL:  strings.TrimRight(baseURL, "/"),
                username: username,
                password: password,
                timeout:  timeout,
                httpClient: &http.Client{
                        Timeout: timeout,
                        Transport: &http.Transport{
                                // PR #259: concurrency pool — default MaxIdleConnsPerHost=2 idi,
                                // 10 paralel AZMK çağırışda 8 yeni TLS handshake açırdı.
                                MaxIdleConns:        100,
                                MaxIdleConnsPerHost: 20,
                                MaxConnsPerHost:     50,
                                IdleConnTimeout:     90 * time.Second,
                                TLSClientConfig: &tls.Config{
                                        InsecureSkipVerify: true, // AZMK self-signed sertifikat üçün
                                },
                        },
                },
        }
}

// SetAuditDB sets the DB connection for audit logging.
// PR #163: hər AZMK HTTP çağırış üçün audit log yazmaq.
func (p *HTTPProvider) SetAuditDB(db *sql.DB, appID *int) {
        p.auditDB = db
        p.appID = appID
}

// SetAuditAppID sets the current application ID for audit logging.
// PR #168: hər müraciət üçün dinamik olaraq appID set etmək.
// PR #259: DEPRECATED — shared mutable state race yaradırdı. Əvəzinə
// context.WithValue + AppIDFromContext istifadə olunur. Backward-compat üçün saxlanılır.
func (p *HTTPProvider) SetAuditAppID(appID *int) {
        p.appID = appID
}

// contextKey type for context value keys (PR #259).
type contextKey string

// appIDKey is the context key for application ID (PR #259).
const appIDKey contextKey = "azmk_app_id"

// WithAppID returns a new context with the given application ID (PR #259).
// Thread-safe way to pass appID to auditLog without shared mutable state.
func WithAppID(ctx context.Context, appID *int) context.Context {
        return context.WithValue(ctx, appIDKey, appID)
}

// AppIDFromContext extracts the application ID from the context (PR #259).
func AppIDFromContext(ctx context.Context) *int {
        if v, ok := ctx.Value(appIDKey).(*int); ok {
                return v
        }
        return nil
}

// auditLog writes a service call audit log to the database.
// PR #259: appID context-dən oxunur — shared mutable state race aradan qaldırıldı.
func (p *HTTPProvider) auditLog(ctx context.Context, serviceName, method, url, reqBody, respBody string, statusCode int, durationMs int, errMsg string) {
        if p.auditDB == nil {
                return // audit logging disabled
        }
        appID := AppIDFromContext(ctx) // PR #259: context-dən oxu (thread-safe)
        _, err := p.auditDB.ExecContext(ctx, `
                INSERT INTO service_audit_logs
                        (application_id, service_name, method, url, request_body, response_body, status_code, duration_ms, error)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
                appID, serviceName, method, url, reqBody, respBody, statusCode, durationMs, errMsg)
        if err != nil {
                slog.Warn("failed to write audit log", "error", err, "service", serviceName)
        }
}
// PR #123: AZMK servisi username/password tələb edir.
func (p *HTTPProvider) setAuthHeaders(req *http.Request) {
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := base64.StdEncoding.EncodeToString([]byte(p.username + ":" + p.password))
                req.Header.Set("Authorization", "Basic "+auth)
        }
}

// doPost sends a POST request and returns the response body as string.
func (p *HTTPProvider) doPost(ctx context.Context, path string, body interface{}) (string, error) {
        return p.doRequest(ctx, http.MethodPost, path, body)
}

// doPut sends a PUT request and returns the response body as string.
// PR #156: AZMK /partner endpoint PUT metodu tələb edir.
func (p *HTTPProvider) doPut(ctx context.Context, path string, body interface{}) (string, error) {
        return p.doRequest(ctx, http.MethodPut, path, body)
}

// doRequest sends an HTTP request with the given method and returns the response body.
func (p *HTTPProvider) doRequest(ctx context.Context, method, path string, body interface{}) (string, error) {
        url := p.baseURL + path
        serviceName := "AZMK_" + strings.ToUpper(strings.Trim(path, "/"))

        var reqBodyStr string
        var reqBody *strings.Reader
        if body != nil {
                jsonBody, err := json.Marshal(body)
                if err != nil {
                        return "", fmt.Errorf("azmk: failed to marshal request: %w", err)
                }
                reqBodyStr = string(jsonBody)
                reqBody = strings.NewReader(reqBodyStr)
        }

        req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
        if err != nil {
                p.auditLog(ctx, serviceName, method, url, reqBodyStr, "", 0, 0, err.Error())
                return "", fmt.Errorf("azmk: failed to create request: %w", err)
        }
        p.setAuthHeaders(req)

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog(ctx, serviceName, method, url, reqBodyStr, "", 0, durationMs, err.Error())
                return "", fmt.Errorf("azmk: HTTP request failed: %w", err)
        }
        defer resp.Body.Close()

        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                p.auditLog(ctx, serviceName, method, url, reqBodyStr, "", resp.StatusCode, durationMs, err.Error())
                return "", fmt.Errorf("azmk: failed to read response: %w", err)
        }

        respBodyStr := string(respBody)

        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                errMsg := fmt.Sprintf("azmk: %s returned HTTP %d: %s", path, resp.StatusCode, respBodyStr)
                p.auditLog(ctx, serviceName, method, url, reqBodyStr, respBodyStr, resp.StatusCode, durationMs, errMsg)
                return "", fmt.Errorf("%s", errMsg)
        }

        // PR #163: audit log — uğurlu çağırış
        p.auditLog(ctx, serviceName, method, url, reqBodyStr, respBodyStr, resp.StatusCode, durationMs, "")

        return respBodyStr, nil
}

// doGet sends a GET request and returns the response body as string.
func (p *HTTPProvider) doGet(ctx context.Context, path string) (string, error) {
        url := p.baseURL + path
        serviceName := "AZMK_" + strings.ToUpper(strings.Trim(path, "/"))

        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                p.auditLog(ctx, serviceName, "GET", url, "", "", 0, 0, err.Error())
                return "", fmt.Errorf("azmk: failed to create request: %w", err)
        }
        p.setAuthHeaders(req)

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog(ctx, serviceName, "GET", url, "", "", 0, durationMs, err.Error())
                return "", fmt.Errorf("azmk: HTTP request failed: %w", err)
        }
        defer resp.Body.Close()

        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                p.auditLog(ctx, serviceName, "GET", url, "", "", resp.StatusCode, durationMs, err.Error())
                return "", fmt.Errorf("azmk: failed to read response: %w", err)
        }

        respBodyStr := string(respBody)

        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                errMsg := fmt.Sprintf("azmk: %s returned HTTP %d: %s", path, resp.StatusCode, respBodyStr)
                p.auditLog(ctx, serviceName, "GET", url, "", respBodyStr, resp.StatusCode, durationMs, errMsg)
                return "", fmt.Errorf("%s", errMsg)
        }

        p.auditLog(ctx, serviceName, "GET", url, "", respBodyStr, resp.StatusCode, durationMs, "")
        return respBodyStr, nil
}

// parseIDResponse extracts the ID from AZMK responses.
// AZMK returns either a plain string ID or {"id": "..."} JSON.
func parseIDResponse(body string) (string, error) {
        body = strings.TrimSpace(body)
        body = strings.Trim(body, `"`)

        // Try JSON first
        var jsonResp struct {
                ID string `json:"id"`
        }
        if err := json.Unmarshal([]byte(body), &jsonResp); err == nil && jsonResp.ID != "" {
                return jsonResp.ID, nil
        }

        // Plain string
        if body != "" {
                return body, nil
        }

        return "", fmt.Errorf("azmk: could not parse ID from response: %s", body)
}

// ============================================================
// Provider methods
// ============================================================

// KYC creates a KYC session and returns the KYC ID.
func (p *HTTPProvider) KYC(ctx context.Context, req *KYCRequest) (string, error) {
        body, err := p.doPost(ctx, "/kyc", req)
        if err != nil {
                return "", err
        }
        id, err := parseIDResponse(body)
        if err != nil {
                return "", err
        }
        slog.Info("AZMK KYC created", "kyc_id", id)
        return id, nil
}

// VerifyKYC checks if the KYC session is verified.
// PR #155: AZMK GET /kyc/{id} response formats:
//   - {"status": "SENT"}     — KYC göndərilib, hələ verify olunmayıb
//   - {"status": "VERIFIED"} — müştəri verify edib
//   - "Invalidid"             — yanlış KYC ID (plain string, JSON struktursuz)
//
// VerifyKYC true qaytarır yalnız "VERIFIED" halında.
// "SENT" halında false qaytarır (hələ verify olunmayıb — polling davam etməli).
// "Invalidid" halında error qaytarır.
func (p *HTTPProvider) VerifyKYC(ctx context.Context, kycID string) (bool, error) {
        body, err := p.doGet(ctx, "/kyc/"+kycID)
        if err != nil {
                return false, err
        }

        // PR #155: "Invalidid" yoxlaması — plain string, JSON struktursuz
        if strings.Contains(body, "Invalidid") {
                slog.Warn("AZMK KYC verify failed — invalid ID", "kyc_id", kycID, "response", body)
                return false, fmt.Errorf("AZMK KYC invalid id: %s", kycID)
        }

        // Status parse — JSON format: {"status": "SENT"} və ya {"status": "VERIFIED"}
        var resp struct {
                Status string `json:"status"`
        }
        if err := json.Unmarshal([]byte(body), &resp); err != nil {
                // JSON parse xətası — fallback to string contains (backward compatible)
                slog.Warn("AZMK KYC verify: failed to parse JSON, using string match",
                        "kyc_id", kycID, "response", body, "error", err)
                verified := strings.Contains(strings.ToUpper(body), "VERIFIED")
                slog.Info("AZMK KYC verify", "kyc_id", kycID, "verified", verified, "response", body)
                return verified, nil
        }

        slog.Info("AZMK KYC verify", "kyc_id", kycID, "status", resp.Status, "response", body)

        switch strings.ToUpper(resp.Status) {
        case "VERIFIED":
                return true, nil
        case "SENT":
                return false, nil // hələ verify olunmayıb — polling davam etməli
        case "PIN_MISMATCH":
                // PR #163: PIN uyğun gəlmir — polling-i dayandır və error qaytar
                slog.Warn("AZMK KYC verify failed — PIN mismatch",
                        "kyc_id", kycID, "response", body)
                return false, fmt.Errorf("KYC PIN uyğun gəlmir — göndərilən FIN kodu sənədlə uyğun deyil")
        default:
                return false, nil // naməlum status — təhlükəsiz olaraq false
        }
}

// RegisterPartner registers a partner and returns the Partner ID.
// PR #156: AZMK /partner endpoint PUT metodu tələb edir (POST yox).
func (p *HTTPProvider) RegisterPartner(ctx context.Context, req *PartnerRequest) (string, error) {
        body, err := p.doPut(ctx, "/partner", req)
        if err != nil {
                return "", err
        }
        id, err := parseIDResponse(body)
        if err != nil {
                return "", err
        }
        slog.Info("AZMK Partner registered", "partner_id", id)
        return id, nil
}

// RegisterCard registers a card and returns the Card ID.
func (p *HTTPProvider) RegisterCard(ctx context.Context, req *CardRequest) (string, error) {
        body, err := p.doPost(ctx, "/card", req)
        if err != nil {
                return "", err
        }
        id, err := parseIDResponse(body)
        if err != nil {
                return "", err
        }
        slog.Info("AZMK Card registered", "card_id", id)
        return id, nil
}

// CreateApplication creates a loan application and returns the Application ID.
func (p *HTTPProvider) CreateApplication(ctx context.Context, req *ApplicationCreateRequest) (string, error) {
        body, err := p.doPost(ctx, "/application/create", req)
        if err != nil {
                return "", err
        }
        id, err := parseIDResponse(body)
        if err != nil {
                return "", err
        }
        slog.Info("AZMK Application created", "application_id", id)
        return id, nil
}

// CheckSign checks if the application contract is signed.
func (p *HTTPProvider) CheckSign(ctx context.Context, applicationID string) (bool, error) {
        body, err := p.doGet(ctx, "/application/"+applicationID+"/sign")
        if err != nil {
                return false, err
        }
        signed := strings.Contains(strings.ToLower(body), "already signed") ||
                strings.Contains(strings.ToLower(body), "signed")
        slog.Info("AZMK Sign check", "application_id", applicationID, "signed", signed, "response", body)
        return signed, nil
}

// Disburse disburses the loan to the customer's card.
func (p *HTTPProvider) Disburse(ctx context.Context, req *DisburseRequest) error {
        _, err := p.doPost(ctx, "/application/disburse", req)
        if err != nil {
                return err
        }
        slog.Info("AZMK Disburse completed",
                "application_id", req.LoanData.ApplicationID,
                "card_id", req.LoanData.CardID)
        return nil
}

// ============================================================
// Mock Provider (test üçün)
// ============================================================

// MockProvider implements the AZMK Provider interface with mock responses.
type MockProvider struct{}

func NewMockProvider() *MockProvider { return &MockProvider{} }

func (m *MockProvider) KYC(_ context.Context, _ *KYCRequest) (string, error) {
        id := "MOCK-KYC-0001"
        slog.Info("mock AZMK KYC", "kyc_id", id)
        return id, nil
}

func (m *MockProvider) VerifyKYC(_ context.Context, kycID string) (bool, error) {
        slog.Info("mock AZMK KYC verify", "kyc_id", kycID, "verified", true)
        return true, nil
}

func (m *MockProvider) RegisterPartner(_ context.Context, _ *PartnerRequest) (string, error) {
        id := "MOCK-PARTNER-0001"
        slog.Info("mock AZMK Partner", "partner_id", id)
        return id, nil
}

func (m *MockProvider) RegisterCard(_ context.Context, _ *CardRequest) (string, error) {
        id := "MOCK-CARD-0001"
        slog.Info("mock AZMK Card", "card_id", id)
        return id, nil
}

func (m *MockProvider) CreateApplication(_ context.Context, _ *ApplicationCreateRequest) (string, error) {
        id := "MOCK-APP-0001"
        slog.Info("mock AZMK Application create", "application_id", id)
        return id, nil
}

func (m *MockProvider) CheckSign(_ context.Context, applicationID string) (bool, error) {
        slog.Info("mock AZMK Sign check", "application_id", applicationID, "signed", true)
        return true, nil
}

func (m *MockProvider) Disburse(_ context.Context, req *DisburseRequest) error {
        slog.Info("mock AZMK Disburse",
                "application_id", req.LoanData.ApplicationID,
                "card_id", req.LoanData.CardID)
        return nil
}
