package mygov

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPProvider implements the MyGov Provider interface by calling the real
// MyGov API via HTTP. This is the production implementation.
//
// NOTE: endpoint paths are placeholders — update when real docs arrive.
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

const (
	pathGenerateLink = "/api/mygov/permission/generate"
	pathFetchData    = "/api/mygov/permission/data"
	pathEmployeeInfo = "/api/mygov/employee-info"
)

// GeneratePermissionLink calls MyGov to create a permission URL.
func (p *HTTPProvider) GeneratePermissionLink(ctx context.Context, fin string) (*PermissionLink, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+pathGenerateLink+"?fin="+url.QueryEscape(fin), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create MyGov request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MyGov request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MyGov returned HTTP %d", resp.StatusCode)
	}

	var link PermissionLink
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		return nil, fmt.Errorf("failed to decode MyGov response: %w", err)
	}
	return &link, nil
}

// FetchAuthorizedData retrieves the customer's authorized data from MyGov.
func (p *HTTPProvider) FetchAuthorizedData(ctx context.Context, token string) (*AuthorizedData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.baseURL+pathFetchData+"?token="+url.QueryEscape(token), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create MyGov request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MyGov request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MyGov returned HTTP %d", resp.StatusCode)
	}

	var data AuthorizedData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode MyGov response: %w", err)
	}
	return &data, nil
}

// Name returns "mygov-http".
func (p *HTTPProvider) Name() string { return "mygov-http" }

// GetEmployeeInfoByPin retrieves employment records from the MLSA service (PR #237).
// POST with the PIN in the body; response is the EmployeeInfoResponse JSON
// (Active/Deactive with Contract.SignDate in dd.mm.yyyy format).
// NOTE: path is a placeholder — update when real MyGov/MLSA docs arrive.
func (p *HTTPProvider) GetEmployeeInfoByPin(ctx context.Context, pin string) (*EmployeeInfoResponse, error) {
	body := fmt.Sprintf(`{"pin":%q}`, pin)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+pathEmployeeInfo, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create employee-info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("employee-info request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("employee-info: MyGov returned HTTP %d", resp.StatusCode)
	}

	var info EmployeeInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode employee-info response: %w", err)
	}
	return &info, nil
}
