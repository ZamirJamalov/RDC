package sima

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"rdc-source/pkg/extlog" // PR #304: xarici çağırışların Loki log-u
)

// HTTPProvider implements the SIMA Provider interface by calling the real
// SIMA KYC service via HTTP. This is the production implementation.
//
// NOTE: The exact endpoint paths and request/response formats depend on the
// SIMA API documentation. The paths below are placeholders. When the real
// docs arrive, update the path constants — the handler structure stays same.
type HTTPProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewHTTPProvider creates a new HTTPProvider.
func NewHTTPProvider(baseURL, apiKey string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

// SIMA API endpoint paths (placeholders).
const (
	pathInitKyc  = "/api/sima/kyc/init"
	pathGetResult = "/api/sima/kyc/result"
)

// InitKyc starts a SIMA KYC session via HTTP.
func (p *HTTPProvider) InitKyc(ctx context.Context, appID int, fin string) (*InitResponse, error) {
	params := url.Values{}
	params.Set("application_id", fmt.Sprintf("%d", appID))
	params.Set("fin", fin)
	requestURL := p.baseURL + pathInitKyc + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SIMA init request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := p.client.Do(req)
	durationMs := int(time.Since(start).Milliseconds())
	if err != nil {
		extlog.Call("sima", "kyc_init", http.MethodPost, requestURL, "", 0, "", durationMs, err.Error())
		return nil, fmt.Errorf("SIMA init request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		extlog.Call("sima", "kyc_init", http.MethodPost, requestURL, "", resp.StatusCode, "", durationMs, err.Error())
		return nil, fmt.Errorf("failed to read SIMA init response: %w", err)
	}

	// PR #304: sorğu+cavab Loki-yə düşür
	extlog.Call("sima", "kyc_init", http.MethodPost, requestURL, "", resp.StatusCode, string(respBody), durationMs, "")

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("SIMA init returned HTTP %d", resp.StatusCode)
	}

	var result InitResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode SIMA init response: %w", err)
	}
	return &result, nil
}

// GetResult fetches the SIMA KYC result via HTTP.
func (p *HTTPProvider) GetResult(ctx context.Context, sessionID string) (*ResultResponse, error) {
	requestURL := p.baseURL + pathGetResult + "?session_id=" + sessionID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SIMA result request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := p.client.Do(req)
	durationMs := int(time.Since(start).Milliseconds())
	if err != nil {
		extlog.Call("sima", "kyc_result", http.MethodGet, requestURL, "", 0, "", durationMs, err.Error())
		return nil, fmt.Errorf("SIMA result request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		extlog.Call("sima", "kyc_result", http.MethodGet, requestURL, "", resp.StatusCode, "", durationMs, err.Error())
		return nil, fmt.Errorf("failed to read SIMA result response: %w", err)
	}

	// PR #304: sorğu+cavab Loki-yə düşür
	extlog.Call("sima", "kyc_result", http.MethodGet, requestURL, "", resp.StatusCode, string(respBody), durationMs, "")

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("SIMA result returned HTTP %d", resp.StatusCode)
	}

	var result ResultResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode SIMA result response: %w", err)
	}
	return &result, nil
}

// Name returns "sima-http".
func (p *HTTPProvider) Name() string { return "sima-http" }
