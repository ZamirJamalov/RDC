package otp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPProvider implements the OTP Provider interface by calling a real SMS
// gateway via HTTP. This is the production implementation.
//
// Currently configured for the Softline SMS gateway API:
//   GET http://gw.softline.az/sendsms?user=...&password=...&gsm=...&from=...&text=...
//   Response: errno=100&errtext=OK&message_id=526973&charge=1&balance=123
type HTTPProvider struct {
	baseURL string
	apiKey  string // used as password
	user    string
	sender  string
	client  *http.Client
}

// NewHTTPProvider creates a new HTTPProvider.
func NewHTTPProvider(baseURL, apiKey, user, sender string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		user:    user,
		sender:  sender,
		client:  &http.Client{Timeout: timeout},
	}
}

// Send delivers the OTP code via the SMS gateway.
// Softline API expects a GET request with query parameters and returns
// a URL-encoded response (not JSON).
func (p *HTTPProvider) Send(ctx context.Context, phone, code string) error {
	// Build the SMS text
	text := fmt.Sprintf("Your RDC verification code: %s. Do not share it with anyone.", code)

	// Build the URL with query parameters
	params := url.Values{}
	params.Set("user", p.user)
	params.Set("password", p.apiKey)
	params.Set("gsm", phone)
	params.Set("from", p.sender)
	params.Set("text", text)

	requestURL := p.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SMS request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("SMS gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read SMS gateway response: %w", err)
	}

	// Softline returns HTTP 200 even on errors — check the body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("SMS gateway returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse the URL-encoded response: errno=100&errtext=OK&message_id=...
	return parseSoftlineResponse(string(body))
}

// parseSoftlineResponse checks if the SMS was sent successfully.
// Softline response format: errno=100&errtext=OK&message_id=526973&charge=1&balance=123
// errno=100 means OK; any other value is an error.
func parseSoftlineResponse(body string) error {
	// Parse URL-encoded response
	values, err := url.ParseQuery(body)
	if err != nil {
		return fmt.Errorf("failed to parse SMS gateway response: %w (body: %s)", err, body)
	}

	errnoStr := values.Get("errno")
	errtext := values.Get("errtext")

	if errnoStr == "" {
		return fmt.Errorf("SMS gateway returned empty errno (body: %s)", body)
	}

	errno, err := strconv.Atoi(errnoStr)
	if err != nil {
		return fmt.Errorf("SMS gateway returned non-numeric errno %q: %w", errnoStr, err)
	}

	// errno=100 means OK
	if errno != 100 {
		return fmt.Errorf("SMS gateway error: errno=%d errtext=%s", errno, errtext)
	}

	return nil
}

// Name returns "http".
func (p *HTTPProvider) Name() string { return "http" }

// softlineErrorMessages maps Softline error codes to human-readable messages.
var softlineErrorMessages = map[int]string{
	0:   "Missing parameter or XML parse error",
	10:  "Configuration error",
	20:  "Invalid phone number or no valid message",
	25:  "Blacklisted phone number",
	30:  "Unauthorized destination network",
	40:  "Invalid username or password",
	50:  "Unauthorized sender name",
	60:  "Insufficient balance",
	80:  "Invalid validity period",
	85:  "Invalid delivery datetime",
	90:  "Exceeded message size limit",
	200: "Server error",
}

// softlineErrorMessage returns a human-readable message for a Softline error code.
func softlineErrorMessage(errno int) string {
	if msg, ok := softlineErrorMessages[errno]; ok {
		return msg
	}
	return fmt.Sprintf("Unknown error code %d", errno)
}

// softlineErrorText extracts the error text from the Softline response.
// This is used for logging.
func softlineErrorText(body string) string {
	values, err := url.ParseQuery(body)
	if err != nil {
		return body
	}
	errnoStr := values.Get("errno")
	errtext := values.Get("errtext")
	errno, _ := strconv.Atoi(errnoStr)
	return strings.TrimSpace(fmt.Sprintf("%s (%s)", errtext, softlineErrorMessage(errno)))
}
