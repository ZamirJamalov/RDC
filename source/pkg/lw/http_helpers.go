package lw

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
)

// getJSON sends a GET request and decodes the JSON response into target.
// PR #199: HTML response (404/login page) detect edib aydın xəta mesajı qaytarır.
func (p *HTTPProvider) getJSON(ctx context.Context, path string, target interface{}) error {
        url := p.baseURL + path
        body, err := p.doRequest(ctx, http.MethodGet, url, nil)
        if err != nil {
                return err
        }
        if err := detectHTMLResponse(body); err != nil {
                return err
        }
        if err := json.Unmarshal(body, target); err != nil {
                return fmt.Errorf("failed to decode response: %w (body preview: %s)", err, previewBody(body))
        }
        return nil
}

// postJSON sends a POST request with a JSON body and decodes the response.
// PR #199: HTML response detect edib aydın xəta mesajı qaytarır.
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
                if err := detectHTMLResponse(body); err != nil {
                        return err
                }
                if err := json.Unmarshal(body, target); err != nil {
                        return fmt.Errorf("failed to decode response: %w (body preview: %s)", err, previewBody(body))
                }
        }
        return nil
}

// detectHTMLResponse checks if the response body is HTML instead of JSON.
// PR #199: LW server bəzən JSON yerinə HTML qaytarır (404, login səhifəsi və s.)
// Bu, JSON decode xətasının "invalid character '<'" kimi qarışıq görünməsinin qarşısını alır.
func detectHTMLResponse(body []byte) error {
        trimmed := strings.TrimSpace(string(body))
        if len(trimmed) == 0 {
                return fmt.Errorf("LW returned empty response body")
        }
        // HTML response-lar adətən <!DOCTYPE və ya <html> ilə başlayır
        if strings.HasPrefix(trimmed, "<!") || strings.HasPrefix(trimmed, "<html") ||
                strings.HasPrefix(trimmed, "<HTML") || strings.HasPrefix(trimmed, "<!DOCTYPE") {
                return fmt.Errorf("LW server HTML response qaytardı (JSON gözlənirdi) — LW endpoint mövcud deyil və ya auth tələb olunur (response: %s)", previewBody(body))
        }
        // Content-Type-dan asılı olmayaraq, əgər body < ilə başlayırsa, HTML-dir
        if strings.HasPrefix(trimmed, "<") {
                return fmt.Errorf("LW server HTML response qaytardı (JSON gözlənirdi) — LW endpoint mövcud deyil və ya auth tələb olunur (response: %s)", previewBody(body))
        }
        return nil
}

// previewBody returns first 200 chars of body for error messages.
func previewBody(body []byte) string {
        s := string(body)
        if len(s) > 200 {
                return s[:200] + "..."
        }
        return s
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
