// Package videorecord provides a client for the Kvadrat Lab video identity verification service.
// PR #188: Müştəri kredit təsdiq etməzdən əvvəl video identifikasiya keçməlidir.
//
// Service: https://videodemo.kvadrat-lab.com
// Endpoints:
//   POST /api/orders        — create video record order, returns redirect_url
//   POST /api/orders/status — poll status by app_ids, returns recorded flag
//
// Both endpoints require HTTP Basic Auth (username: bokt, password: ecbb82...).
package videorecord

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"rdc-source/internal/model"
)

// Provider is the interface for the video record service.
type Provider interface {
	// CreateOrder sends a POST /api/orders request and returns the redirect_url.
	CreateOrder(ctx context.Context, req *model.CreateVideoOrderRequest) (*model.CreateVideoOrderResponse, error)
	// CheckStatus sends a POST /api/orders/status request and returns recorded flags.
	CheckStatus(ctx context.Context, appIDs []string) (*model.VideoOrderStatusResponse, error)
	// SetAuditDB injects the DB connection for service_audit_logs writes.
	SetAuditDB(db *sql.DB, appID *int)
	// SetAuditAppID updates the application ID used in audit logs (called per-request).
	SetAuditAppID(appID *int)
}

// HTTPProvider is the real implementation that calls the video service over HTTP.
type HTTPProvider struct {
	baseURL    string
	username   string
	password   string
	timeout    time.Duration
	httpClient *http.Client
	auditDB    *sql.DB
	appID      *int
}

// NewHTTPProvider creates a new HTTPProvider.
func NewHTTPProvider(baseURL, username, password string, timeoutS int) *HTTPProvider {
	timeout := time.Duration(timeoutS) * time.Second
	return &HTTPProvider{
		baseURL:    baseURL,
		username:   username,
		password:   password,
		timeout:    timeout,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// SetAuditDB injects the DB for service_audit_logs writes.
func (p *HTTPProvider) SetAuditDB(db *sql.DB, appID *int) {
	p.auditDB = db
	p.appID = appID
}

// SetAuditAppID updates the application ID used in audit logs.
func (p *HTTPProvider) SetAuditAppID(appID *int) {
	p.appID = appID
}

// CreateOrder sends a POST /api/orders request.
func (p *HTTPProvider) CreateOrder(ctx context.Context, req *model.CreateVideoOrderRequest) (*model.CreateVideoOrderResponse, error) {
	jsonBody, _ := json.Marshal(req)
	url := p.baseURL + "/api/orders"
	serviceName := "VIDEO_RECORD_CREATE_ORDER"

	respBody, statusCode, durationMs, err := p.doRequest(ctx, http.MethodPost, url, jsonBody)
	if err != nil {
		p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), "", statusCode, durationMs, err.Error())
		return nil, fmt.Errorf("video record create order request failed: %w", err)
	}

	var resp model.CreateVideoOrderResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), string(respBody), statusCode, durationMs, fmt.Sprintf("decode error: %v", err))
		return nil, fmt.Errorf("failed to decode video record response: %w", err)
	}

	if resp.Success != 1 {
		errMsg := fmt.Sprintf("video service error: %s (success=%d)", resp.Message, resp.Success)
		p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), string(respBody), statusCode, durationMs, errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), string(respBody), statusCode, durationMs, "")
	slog.Info("video record order created",
		"app_id", req.AppID,
		"redirect_url", resp.RedirectURL,
		"duration_ms", durationMs)
	return &resp, nil
}

// CheckStatus sends a POST /api/orders/status request.
func (p *HTTPProvider) CheckStatus(ctx context.Context, appIDs []string) (*model.VideoOrderStatusResponse, error) {
	reqBody := model.VideoOrderStatusRequest{AppIDs: appIDs}
	jsonBody, _ := json.Marshal(reqBody)
	url := p.baseURL + "/api/orders/status"
	serviceName := "VIDEO_RECORD_CHECK_STATUS"

	respBody, statusCode, durationMs, err := p.doRequest(ctx, http.MethodPost, url, jsonBody)
	if err != nil {
		p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), "", statusCode, durationMs, err.Error())
		return nil, fmt.Errorf("video record status check request failed: %w", err)
	}

	var resp model.VideoOrderStatusResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), string(respBody), statusCode, durationMs, fmt.Sprintf("decode error: %v", err))
		return nil, fmt.Errorf("failed to decode video status response: %w", err)
	}

	p.auditLog(serviceName, http.MethodPost, url, string(jsonBody), string(respBody), statusCode, durationMs, "")
	return &resp, nil
}

// doRequest is the shared HTTP helper with Basic Auth.
func (p *HTTPProvider) doRequest(ctx context.Context, method, url string, body []byte) ([]byte, int, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.username != "" && p.password != "" {
		auth := p.username + ":" + p.password
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}

	start := time.Now()
	resp, err := p.httpClient.Do(req)
	durationMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return nil, 0, durationMs, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, durationMs, nil
}

// auditLog writes a row to service_audit_logs.
func (p *HTTPProvider) auditLog(serviceName, method, url, reqBody, respBody string, statusCode int, durationMs int, errMsg string) {
	if p.auditDB == nil {
		return
	}
	_, err := p.auditDB.Exec(`
		INSERT INTO service_audit_logs
			(application_id, service_name, method, url, request_body, response_body, status_code, duration_ms, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.appID, serviceName, method, url, reqBody, respBody, statusCode, durationMs, errMsg)
	if err != nil {
		slog.Warn("failed to write video audit log", "error", err, "service", serviceName)
	}
}

// MockProvider is a no-op mock for dev/test without real HTTP calls.
type MockProvider struct{}

// NewMockProvider creates a new MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// SetAuditDB noop.
func (m *MockProvider) SetAuditDB(_ *sql.DB, _ *int) {}

// SetAuditAppID noop.
func (m *MockProvider) SetAuditAppID(_ *int) {}

// CreateOrder returns a mock success response with a fake redirect_url.
func (m *MockProvider) CreateOrder(_ context.Context, req *model.CreateVideoOrderRequest) (*model.CreateVideoOrderResponse, error) {
	return &model.CreateVideoOrderResponse{
		Success:     1,
		Message:     "Order created successfully (mock)",
		RedirectURL: "https://videodemo.kvadrat-lab.com/record/mock-mock-mock/",
	}, nil
}

// CheckStatus returns recorded=true after a configurable delay (always true for mock simplicity).
func (m *MockProvider) CheckStatus(_ context.Context, appIDs []string) (*model.VideoOrderStatusResponse, error) {
	results := make([]model.VideoOrderStatusResult, 0, len(appIDs))
	for _, id := range appIDs {
		results = append(results, model.VideoOrderStatusResult{
			AppID:    id,
			Recorded: true,
		})
	}
	return &model.VideoOrderStatusResponse{Results: results}, nil
}
