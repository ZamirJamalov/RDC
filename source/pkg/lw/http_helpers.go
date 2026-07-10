package lw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// getJSON sends a GET request and decodes the JSON response into target.
func (p *HTTPProvider) getJSON(ctx context.Context, path string, target interface{}) error {
	url := p.baseURL + path
	body, err := p.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// postJSON sends a POST request with a JSON body and decodes the response.
func (p *HTTPProvider) postJSON(ctx context.Context, path string, payload interface{}, target interface{}) error {
	url := p.baseURL + path
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}
	body, err := p.doRequest(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if target != nil && len(body) > 0 {
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

// doRequest sends an HTTP request with the API key header and returns the
// response body. Returns an error for non-2xx status codes.
func (p *HTTPProvider) doRequest(ctx context.Context, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LW returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
