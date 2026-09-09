package azmk

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rdc-source/pkg/extlog" // PR #304: xarici çağırışların Loki log-u
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
//   4b. Card List    — GET  /card/{partnerId} (partnerin köhnə kartları) — PR #313
//   5. App Create    — POST /application/create (LoanData göndər → Application ID qaytar)
//   6. Sign Status   — GET  /application/{id}/status (loanId, loanStatus, smsSent, signed) — PR #312
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

	// GetCards lists the cards previously registered under a partner.
	// PR #313: GET /card/{partnerId} — apply səhifəsində köhnə kart seçimi
	// üçün. Partner üçün kart yoxdursa (HTTP 404 "Invalidid") boş siyahı
	// qaytarır — bu xəta sayılmır.
	GetCards(ctx context.Context, partnerID string) ([]CardInfo, error)

	// CreateApplication creates a loan application and returns the Application ID.
	CreateApplication(ctx context.Context, req *ApplicationCreateRequest) (string, error)

	// GetApplicationStatus fetches the AZMK application status (sign + loan info).
	// PR #312: GET /application/{id}/status — replaces the old /sign endpoint.
	// The Signed field tells whether the customer signed the contract.
	GetApplicationStatus(ctx context.Context, applicationID string) (*ApplicationStatus, error)

	// Disburse disburses the loan to the customer's card.
	Disburse(ctx context.Context, req *DisburseRequest) error

	// SendPartnerPhones sends the 3 dashboard contact phone numbers to the
	// LW Loan Management System. PR #404: approve axınında, application
	// create-dən ƏVVƏL çağırılır — POST /partner/{partnerId}/phones.
	// Xəta qaytarsa approve bloklanır (ekspert kontaktları düzəlib
	// yenidən təsdiq edə bilər).
	SendPartnerPhones(ctx context.Context, partnerID string, req *PartnerPhonesRequest) error
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

// CardInfo is a single card entry from GET /card/{partnerId} (PR #313).
// Code AZMK tərəfindən maskalanır ("****-****-****-5559") — tam PAN gəlmir.
type CardInfo struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Code     string `json:"code"`
	Expiring string `json:"expiring"`
}

// CardsResponse is the response body of GET /card/{partnerId}.
type CardsResponse struct {
	Data []CardInfo `json:"data"`
}

// LoanData is the payload for application create and disburse.
type LoanData struct {
	ClientID        string  `json:"clientId"`        // Partner ID
	ProductID       string  `json:"productId"`       // config-dən (məs. "L07")
	Amount          float64 `json:"amount"`          // total_amount (principal + commission)
	Term            int     `json:"term"`            // months
	BranchCode      string  `json:"branchCode"`      // config-dən (məs. "HO")
	InterestRate    float64 `json:"interestRate"`    // KƏSR formatında göndərilir: 0.48 (= 48%), 0.30 (= 30%) — PR #311
	DisbursementFee float64 `json:"disbursementFee"` // credit_levels.commission / 100 (PR #349)
	LetterNumber    string  `json:"letterNumber"`    // boş
	// Disburse üçün (cardId həmçinin create-də göndərilir — PR #353):
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

// ApplicationStatus is the response for GET /application/{id}/status.
// PR #312: köhnə /sign endpoint-i əvəzinə bu struktur istifadə olunur.
// Nümunə cavab:
//
//	{"loanId":"HO0030210","loanStatus":"S002","smsSent":true,"signed":false}
type ApplicationStatus struct {
	LoanID     string `json:"loanId"`     // AZMK kredit hesab nömrəsi (məs. "HO0030210")
	LoanStatus string `json:"loanStatus"` // AZMK status kodu (məs. "S002")
	SMSSent    bool   `json:"smsSent"`    // imza SMS-i göndərilibmi
	Signed     bool   `json:"signed"`     // müştəri müqaviləni imzalayıb?
}

// PR #404: POST /partner/{partnerId}/phones — 3 kontakt nömrəsi LW-yə.
// PhoneEntry.Description = qohumluq dərəcəsi + ad + zəng qeydi (maks 100 simvol,
// service tərəfində qurulur — internal/service PartnerPhoneEntries).
type PhoneEntry struct {
	Number      string `json:"number"`      // "+994551110011" formatında (boşluqsuz)
	Description string `json:"description"` // "Atası | Zamir | zəng olundu, müsbət" (maks 100)
}

// PhoneData wraps the phone list (LW PhoneData obyekti).
type PhoneData struct {
	Data []PhoneEntry `json:"data"`
}

// PartnerPhonesRequest is the body for POST /partner/{partnerId}/phones.
type PartnerPhonesRequest struct {
	PhoneData PhoneData `json:"PhoneData"`
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
	auditDB *sql.DB
	appID   *int
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

// doRequestWithRetry executes an HTTP request with retry on connection errors.
// PR #264: AZMK server connection refused/timeout olanda avtomatik retry.
func (p *HTTPProvider) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	const maxRetries = 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// PR #267: req.Body io.ReadCloser-dir, *strings.Reader type assertion
		// işləmir (Close() yoxdur). Əvəzinə Go standart GetBody() istifadə et.
		if attempt > 0 && req.GetBody != nil {
			newBody, err := req.GetBody()
			if err == nil {
				req.Body = newBody
			}
		}

		resp, err := p.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "context deadline exceeded") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "i/o timeout") ||
			strings.Contains(errStr, "dial tcp")

		if !isRetryable || attempt == maxRetries {
			return nil, err
		}

		backoff := time.Duration(1<<attempt) * time.Second
		slog.Warn("AZMK request failed — retrying",
			"attempt", attempt+1,
			"max_retries", maxRetries+1,
			"backoff_ms", backoff.Milliseconds(),
			"error", errStr)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
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
	// PR #304: həmçinin Loki-yə yaz (slog → app.log → Promtail → Loki).
	// auditDB nil olsa belə Loki-yə yazılır (DB audit-ə asılı deyil).
	extlog.Call("azmk", serviceName, method, url, reqBody, statusCode, respBody, durationMs, errMsg)
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

// PR #374: poll çağırışları üçün Loki-only audit — DB-yə yazmır.
// KYC status poll-u (SENT) hər 3 saniyədən bir təkrarlanır və 60 cəhdə qədər
// service_audit_logs cədvəlini şişirdirdi; Loki-də (6 ay retention, PR #369)
// hər çağırış onsuz da saxlanılır.
func (p *HTTPProvider) auditLogLokiOnly(_ context.Context, serviceName, method, url, reqBody, respBody string, statusCode int, durationMs int, errMsg string) {
	extlog.Call("azmk", serviceName, method, url, reqBody, statusCode, respBody, durationMs, errMsg)
}

// PR #374: yalnız DB-yə yaz — poll-un YEKUN statusu üçün (VERIFIED / xəta).
// Loki-ya onsuz da doGetVariant→auditLogLokiOnly yazıb — dublikat olmasın deyə.
func (p *HTTPProvider) auditDBInsert(ctx context.Context, serviceName, method, url, reqBody, respBody string, statusCode int, durationMs int, errMsg string) {
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

// httpError is a typed error for non-2xx AZMK responses. PR #420: retry
// wrapper status kodu bu tipdən oxuyur (string parse yox). Error mesajı
// əvvəlki ilə eynidir — loglar dəyişmir.
type httpError struct {
	Path   string
	Status int
	Body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("azmk: %s returned HTTP %d: %s", e.Path, e.Status, e.Body)
}

// transient reports whether the status is worth retrying: server-side /
// rate-limit responses (5xx, 429). 4xx cavablar (validasiya, auth və s.)
// retry-edilmir — təkrar sorğu nəticəni dəyişməz.
func (e *httpError) transient() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// PR #420: idempotent AZMK çağırışları üçün HTTP-level retry.
// PR #264 (doRequestWithRetry) yalnız connection/timeout xətalarını retry edir;
// bu wrapper əlavə olaraq HTTP 5xx/429 cavablarını da retry edir.
// YALNIZ idempotent əməliyyatlarda istifadə olunur:
//   - RegisterPartner (PUT, eyni məlumat → eyni ID qaytarır)
//   - RegisterCard, SendPartnerPhones (set semantikası)
//   - bütün GET-lər (doGetVariant daxilində)
//
// Qeyri-idempotent əməliyyatlar (disburse, application/create, KYC create)
// retry EDİLMİR — təkrar sorğu cüt əməliyyat/cüt SMS riski daşıyır.
func withAzmkRetry(ctx context.Context, path string, fn func() (string, error)) (string, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, err := fn()
		if err == nil {
			return body, nil
		}
		lastErr = err
		var httpErr *httpError
		if !errors.As(err, &httpErr) || !httpErr.transient() || attempt == maxAttempts {
			return "", err
		}
		// backoff: 500ms, 1s (transport retry daxilində əlavə 1s/2s var)
		backoff := time.Duration(500*(1<<(attempt-1))) * time.Millisecond
		slog.Warn("PR #420: AZMK transient HTTP error — retrying",
			"path", path,
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"status", httpErr.Status,
			"backoff_ms", backoff.Milliseconds())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", lastErr
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

// azmkServiceName — PR #431: path-dən SABİT servis adı törədir.
// Uzun hex/UUID seqmentləri (partner/card/application/kyc ID-ləri) çıxarılır ki,
// service_audit_logs və sağlamlıq panelində (GROUP BY service_name) hər ID üçün
// ayrı "servis" yaranmasın:
//
//	/partner/{32-hex}/phones      → AZMK_PARTNER_PHONES  (əvvəl: AZMK_PARTNER_{ID}_PHONES)
//	/card/{32-hex}                → AZMK_CARD            (əvvəl: AZMK_CARD_{ID})
//	/application/{id}/status      → AZMK_APPLICATION_STATUS
//	/application/create           → AZMK_APPLICATION_CREATE (dəyişməz)
func azmkServiceName(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	kept := make([]string, 0, len(parts))
	for _, seg := range parts {
		if isHexID(seg) {
			continue
		}
		kept = append(kept, strings.ToUpper(seg))
	}
	if len(kept) == 0 {
		return "AZMK"
	}
	return "AZMK_" + strings.Join(kept, "_")
}

// isHexID — path seqmenti 16+ simvollu yalnız-hex dirsə ID yer tutucusudur
// (UUID 32 hex, LW application id — hex format). Qısa seqmentlər (məs. "create",
// "status") adın hissəsi kimi qalır.
func isHexID(s string) bool {
	if len(s) < 16 {
		return false
	}
	for _, c := range s {
		hex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !hex {
			return false
		}
	}
	return true
}

// doRequest sends an HTTP request with the given method and returns the response body.
func (p *HTTPProvider) doRequest(ctx context.Context, method, path string, body interface{}) (string, error) {
	url := p.baseURL + path
	serviceName := azmkServiceName(path) // PR #431: ID seqmentlərindən təmizlənmiş sabit ad

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
	resp, err := p.doRequestWithRetry(ctx, req)
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
		return "", &httpError{Path: path, Status: resp.StatusCode, Body: respBodyStr} // PR #420: typed — retry wrapper üçün
	}

	// PR #163: audit log — uğurlu çağırış
	p.auditLog(ctx, serviceName, method, url, reqBodyStr, respBodyStr, resp.StatusCode, durationMs, "")

	return respBodyStr, nil
}

// doGet sends a GET request and returns the response body as string.
func (p *HTTPProvider) doGet(ctx context.Context, path string) (string, error) {
	return p.doGetVariant(ctx, path, true) // PR #374: default — DB audit ilə
}

// doGetVariant sends a GET request; dbAudit=false → audit yalnız Loki-ya yazılır
// (PR #374: poll çağırışları — məs. KYC status SENT — service_audit_logs
// cədvəlini hər 3 saniyədən bir şişirtməsin; Loki-də 6 ay retention var).
func (p *HTTPProvider) doGetVariant(ctx context.Context, path string, dbAudit bool) (string, error) {
	url := p.baseURL + path
	serviceName := azmkServiceName(path) // PR #431: ID seqmentlərindən təmizlənmiş sabit ad
	audit := p.auditLog
	if !dbAudit {
		audit = p.auditLogLokiOnly // PR #374
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		audit(ctx, serviceName, "GET", url, "", "", 0, 0, err.Error())
		return "", fmt.Errorf("azmk: failed to create request: %w", err)
	}
	p.setAuthHeaders(req)

	start := time.Now()
	resp, err := p.doRequestWithRetry(ctx, req)
	durationMs := int(time.Since(start).Milliseconds())
	if err != nil {
		audit(ctx, serviceName, "GET", url, "", "", 0, durationMs, err.Error())
		return "", fmt.Errorf("azmk: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		audit(ctx, serviceName, "GET", url, "", "", resp.StatusCode, durationMs, err.Error())
		return "", fmt.Errorf("azmk: failed to read response: %w", err)
	}

	respBodyStr := string(respBody)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := fmt.Sprintf("azmk: %s returned HTTP %d: %s", path, resp.StatusCode, respBodyStr)
		audit(ctx, serviceName, "GET", url, "", respBodyStr, resp.StatusCode, durationMs, errMsg)
		return "", &httpError{Path: path, Status: resp.StatusCode, Body: respBodyStr} // PR #420: typed — retry wrapper üçün
	}

	audit(ctx, serviceName, "GET", url, "", respBodyStr, resp.StatusCode, durationMs, "")
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
	body, err := p.doGetVariant(ctx, "/kyc/"+kycID, false) // PR #374: poll — Loki-only
	if err != nil {
		return false, err
	}

	// PR #155: "Invalidid" yoxlaması — plain string, JSON struktursuz
	if strings.Contains(body, "Invalidid") {
		p.auditDBInsert(ctx, "AZMK_KYC/"+kycID, "GET", p.baseURL+"/kyc/"+kycID, "", body, 200, 0, "invalid id") // PR #374: yekun xeta DB-de
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
		p.auditDBInsert(ctx, "AZMK_KYC/"+kycID, "GET", p.baseURL+"/kyc/"+kycID, "", body, 200, 0, "") // PR #374: yekun status DB-də qalır
		return true, nil
	case "SENT":
		return false, nil // hələ verify olunmayıb — polling davam etməli
	case "PIN_MISMATCH":
		// PR #163: PIN uyğun gəlmir — polling-i dayandır və error qaytar
		p.auditDBInsert(ctx, "AZMK_KYC/"+kycID, "GET", p.baseURL+"/kyc/"+kycID, "", body, 200, 0, "PIN_MISMATCH") // PR #374
		slog.Warn("AZMK KYC verify failed — PIN mismatch",
			"kyc_id", kycID, "response", body)
		return false, fmt.Errorf("KYC PIN uyğun gəlmir — göndərilən FIN kodu sənədlə uyğun deyil")
	default:
		return false, nil // naməlum status — təhlükəsiz olaraq false
	}
}

// RegisterPartner registers a partner and returns the Partner ID.
// PR #156: AZMK /partner endpoint PUT metodu tələb edir (POST yox).
// PR #420: 5xx/429-də retry — PUT idempotentdir (eyni məlumat → eyni ID).
func (p *HTTPProvider) RegisterPartner(ctx context.Context, req *PartnerRequest) (string, error) {
	body, err := withAzmkRetry(ctx, "/partner", func() (string, error) {
		return p.doPut(ctx, "/partner", req)
	})
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
// PR #420: 5xx/429-də retry — kart set semantikası daşıyır (eyni PAN →
// AZMK tərəfindən eyni kart qeydi saxlanılır). 4xx (məs. "Invalid code")
// retry olunmur — validasiya xətası təkrar sorğu ilə dəyişməz.
func (p *HTTPProvider) RegisterCard(ctx context.Context, req *CardRequest) (string, error) {
	body, err := withAzmkRetry(ctx, "/card", func() (string, error) {
		return p.doPost(ctx, "/card", req)
	})
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

// GetCards lists the cards registered under a partner (PR #313).
// GET /card/{partnerId} → {"data":[{"id","type","code","expiring"},...]}.
// AZMK mövcud olmayan partner üçün HTTP 404 ("Invalidid") qaytarır — bu
// normal haldır (heç kart qeyd edilməyib): boş siyahı qaytarılır.
func (p *HTTPProvider) GetCards(ctx context.Context, partnerID string) ([]CardInfo, error) {
	// PR #420: 5xx/429-də retry — GET idempotentdir. (404 retry-siz qalır —
	// “kart yoxdur” normal haldır və aşağıda boş siyahıya çevrilir.)
	body, err := withAzmkRetry(ctx, "/card/"+partnerID, func() (string, error) {
		return p.doGet(ctx, "/card/"+partnerID)
	})
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			slog.Info("PR #313: AZMK card list — no cards for partner (404)",
				"partner_id", partnerID)
			return []CardInfo{}, nil
		}
		return nil, err
	}
	var resp CardsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse card list response: %w", err)
	}
	slog.Info("PR #313: AZMK card list", "partner_id", partnerID, "count", len(resp.Data))
	return resp.Data, nil
}

// CreateApplication creates a loan application and returns the Application ID.
// PR #420: retry YOXDUR — qeyri-idempotentdir (təkrar sorğu AZMK-da cüt
// application yarada bilər). Transport-level connection retry (PR #264)
// doRequestWithRetry daxilində qalır.
func (p *HTTPProvider) CreateApplication(ctx context.Context, req *ApplicationCreateRequest) (string, error) {
	body, err := p.doPost(ctx, "/application/create", req)
	if err != nil {
		return "", err
	}
	id, err := parseIDResponse(body)
	if err != nil {
		return "", err
	}
	slog.Info("PR #281: step 7 — AZMK Application created", "step", "7.application_create", "application_id", id)
	return id, nil
}

// GetApplicationStatus fetches the AZMK application status via
// GET /application/{id}/status. PR #312: replaces the old CheckSign
// (GET /application/{id}/sign) endpoint which was incorrect.
func (p *HTTPProvider) GetApplicationStatus(ctx context.Context, applicationID string) (*ApplicationStatus, error) {
	// PR #420: 5xx/429-də retry — GET idempotentdir.
	body, err := withAzmkRetry(ctx, "/application/"+applicationID+"/status", func() (string, error) {
		return p.doGet(ctx, "/application/"+applicationID+"/status")
	})
	if err != nil {
		return nil, err
	}
	var st ApplicationStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return nil, fmt.Errorf("parse application status response: %w", err)
	}
	slog.Info("PR #312: AZMK application status",
		"application_id", applicationID,
		"loan_id", st.LoanID,
		"loan_status", st.LoanStatus,
		"sms_sent", st.SMSSent,
		"signed", st.Signed)
	return &st, nil
}

// Disburse disburses the loan to the customer's card.
// PR #420: retry YOXDUR — qeyri-idempotentdir (cüt köçürmə riski). Transport-level
// connection retry (PR #264) qalır; HTTP 5xx cavabında dərhal uğursuz sayılır.
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

// SendPartnerPhones sends the 3 dashboard contact phone numbers to LW via
// POST /partner/{partnerId}/phones. PR #404 — approve axınında application
// create-dən ƏVVƏL çağırılır. Xəta qaytarırsa approve bloklanır və ekspertə
// xəta mətni göstərilir (kontaktları düzəlib yenidən təsdiq edə bilər).
// doPost audit log-ları (service_audit_logs + Loki) avtomatik yazır.
func (p *HTTPProvider) SendPartnerPhones(ctx context.Context, partnerID string, req *PartnerPhonesRequest) error {
	if partnerID == "" {
		return fmt.Errorf("azmk: partner id is required for phones")
	}
	// PR #420: 5xx/429-də retry — phones set semantikasıdır (idempotent).
	_, err := withAzmkRetry(ctx, "/partner/"+partnerID+"/phones", func() (string, error) {
		return p.doPost(ctx, "/partner/"+partnerID+"/phones", req)
	})
	if err != nil {
		return err
	}
	slog.Info("PR #404: partner phones sent to LW",
		"partner_id", partnerID,
		"count", len(req.PhoneData.Data))
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

// GetCards — PR #313 mock: bir kart qaytarır (ID RegisterCard mock-u ilə eynidir,
// seçilmiş-kart axınını test etmək üçün uyğundur).
func (m *MockProvider) GetCards(_ context.Context, partnerID string) ([]CardInfo, error) {
	cards := []CardInfo{
		{ID: "MOCK-CARD-0001", Type: "CARD", Code: "****-****-****-1111", Expiring: "2030-01-01"},
	}
	slog.Info("mock AZMK card list", "partner_id", partnerID, "count", len(cards))
	return cards, nil
}

func (m *MockProvider) CreateApplication(_ context.Context, _ *ApplicationCreateRequest) (string, error) {
	id := "MOCK-APP-0001"
	slog.Info("mock AZMK Application create", "application_id", id)
	return id, nil
}

func (m *MockProvider) GetApplicationStatus(_ context.Context, applicationID string) (*ApplicationStatus, error) {
	st := &ApplicationStatus{
		LoanID:     "MOCK-LOAN-0001",
		LoanStatus: "S002",
		SMSSent:    true,
		Signed:     true, // mock: həmişə imzalanıb — worker dərhal disburse edir
	}
	slog.Info("mock AZMK application status",
		"application_id", applicationID,
		"loan_id", st.LoanID,
		"loan_status", st.LoanStatus,
		"sms_sent", st.SMSSent,
		"signed", st.Signed)
	return st, nil
}

func (m *MockProvider) Disburse(_ context.Context, req *DisburseRequest) error {
	slog.Info("mock AZMK Disburse",
		"application_id", req.LoanData.ApplicationID,
		"card_id", req.LoanData.CardID)
	return nil
}

// SendPartnerPhones — PR #404 mock: uğur sayılır, heç nə göndərilmir.
func (m *MockProvider) SendPartnerPhones(_ context.Context, partnerID string, req *PartnerPhonesRequest) error {
	slog.Info("mock AZMK partner phones",
		"partner_id", partnerID,
		"count", len(req.PhoneData.Data))
	return nil
}
