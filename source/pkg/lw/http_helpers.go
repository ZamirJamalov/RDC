package lw

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"

        "rdc-source/pkg/extlog" // PR #304: xarici çağırışların Loki log-u
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
        body, err := p.doRequest(ctx, http.MethodPost, url, bodyBytes)
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
// PR #204: xəta mesajına LW vs AZMK izahı əlavə edildi.
// Bu, JSON decode xətasının "invalid character '<'" kimi qarışıq görünməsinin qarşısını alır.
func detectHTMLResponse(body []byte) error {
        trimmed := strings.TrimSpace(string(body))
        if len(trimmed) == 0 {
                return fmt.Errorf("LW returned empty response body")
        }
        // HTML response-lar adətən <!DOCTYPE və ya <html> ilə başlayır
        if strings.HasPrefix(trimmed, "<!") || strings.HasPrefix(trimmed, "<html") ||
                strings.HasPrefix(trimmed, "<HTML") || strings.HasPrefix(trimmed, "<!DOCTYPE") {
                return fmt.Errorf("LW server HTML qaytardı (JSON gözlənirdi). "+
                        "Səbəb: LW_USE_MOCK=false qurulub, amma LW server (LW_BASE_URL) mövcud deyil və ya auth tələb edir. "+
                        "Həll: .env faylında LW_USE_MOCK=true qurun (AZMK fərqli servisdır — AZMK uğurlu olsa belə LW ayrıca konfiqurasiya tələb edir). "+
                        "(response: %s)", previewBody(body))
        }
        // Content-Type-dan asılı olmayaraq, əgər body < ilə başlayırsa, HTML-dir
        if strings.HasPrefix(trimmed, "<") {
                return fmt.Errorf("LW server HTML qaytardı (JSON gözlənirdi). "+
                        "Səbəb: LW_USE_MOCK=false qurulub, amma LW server (LW_BASE_URL) mövcud deyil və ya auth tələb edir. "+
                        "Həll: .env faylında LW_USE_MOCK=true qurun (AZMK fərqli servisdır — AZMK uğurlu olsa belə LW ayrıca konfiqurasiya tələb edir). "+
                        "(response: %s)", previewBody(body))
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
// PR #304: body []byte kimi qəbul olunur (Loki log-u üçün); hər çağırış
// extlog ilə Loki-yə yazılır (service="lw").
func (p *HTTPProvider) doRequest(ctx context.Context, method, url string, body []byte) ([]byte, error) {
        var reqBody io.Reader
        if body != nil {
                reqBody = bytes.NewReader(body)
        }
        req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
        if err != nil {
                return nil, fmt.Errorf("failed to create request: %w", err)
        }

        req.Header.Set("Authorization", "Bearer "+p.apiKey)
        req.Header.Set("Accept", "application/json")
        if body != nil {
                req.Header.Set("Content-Type", "application/json")
        }

        start := time.Now()
        resp, err := p.client.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                extlog.Call("lw", lwOp(url), method, url, string(body), 0, "", durationMs, err.Error())
                return nil, fmt.Errorf("HTTP request failed: %w", err)
        }
        defer resp.Body.Close()

        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                extlog.Call("lw", lwOp(url), method, url, string(body), resp.StatusCode, "", durationMs, err.Error())
                return nil, fmt.Errorf("failed to read response body: %w", err)
        }

        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                errMsg := fmt.Sprintf("LW returned HTTP %d: %s", resp.StatusCode, string(respBody))
                extlog.Call("lw", lwOp(url), method, url, string(body), resp.StatusCode, string(respBody), durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }

        extlog.Call("lw", lwOp(url), method, url, string(body), resp.StatusCode, string(respBody), durationMs, "")
        return respBody, nil
}

// lwOp derives a short op name from the URL path for Loki logs (PR #304).
// Məs: https://host/api/router/init?x=1 → lw_api_router_init
func lwOp(rawurl string) string {
        u := strings.SplitN(rawurl, "?", 2)[0]
        u = strings.Trim(u, "/")
        u = strings.ReplaceAll(u, "/", "_")
        return "lw_" + u
}
