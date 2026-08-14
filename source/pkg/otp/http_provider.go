package otp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPProvider implements the OTP Provider interface by calling a real SMS
// gateway via HTTP. This is the production implementation.
//
// PR #196: parametrik SMS provayder dəstəyi.
// Əvvəl yalnız Softline üçün hardcoded idi (GET, errno=100, URL-encoded response).
// İndi hər parametr konfiqurasiya oluna bilər:
//   - HTTP method (GET/POST)
//   - URL param adları (user, password, gsm, from, text)
//   - Response success sahə və dəyəri (errno=100)
//   - Error mətni sahəsi (errtext)
//
// Bu, hər hansı SMS provayderi (Softline, Twilio, MessageBird və s.) dəstəkləməyə imkan verir.
type HTTPProvider struct {
	baseURL string
	apiKey  string // used as password
	user    string
	sender  string
	client  *http.Client
	// PR #196: parametrik sahələr
	httpMethod   string // GET və ya POST
	paramUser    string
	paramPassword string
	paramPhone   string
	paramSender  string
	paramText    string
	successField string // response-də success yoxlanan sahə
	successValue string // success dəyəri
	errorField   string // response-də error mətni sahəsi
}

// NewHTTPProvider creates a new HTTPProvider.
// PR #196: parametrik sahələr əlavə edildi.
func NewHTTPProvider(baseURL, apiKey, user, sender string, timeout time.Duration,
	httpMethod, paramUser, paramPassword, paramPhone, paramSender, paramText,
	successField, successValue, errorField string) *HTTPProvider {
	// Default dəyərlər (boş gələrsə)
	if httpMethod == "" {
		httpMethod = "GET"
	}
	if paramUser == "" {
		paramUser = "user"
	}
	if paramPassword == "" {
		paramPassword = "password"
	}
	if paramPhone == "" {
		paramPhone = "gsm"
	}
	if paramSender == "" {
		paramSender = "from"
	}
	if paramText == "" {
		paramText = "text"
	}
	if successField == "" {
		successField = "errno"
	}
	if successValue == "" {
		successValue = "100"
	}
	if errorField == "" {
		errorField = "errtext"
	}

	return &HTTPProvider{
		baseURL:       baseURL,
		apiKey:        apiKey,
		user:          user,
		sender:        sender,
		client:        &http.Client{Timeout: timeout},
		httpMethod:    strings.ToUpper(httpMethod),
		paramUser:     paramUser,
		paramPassword: paramPassword,
		paramPhone:    paramPhone,
		paramSender:   paramSender,
		paramText:     paramText,
		successField:  successField,
		successValue:  successValue,
		errorField:    errorField,
	}
}

// Send delivers the given message via the SMS gateway.
// PR #196: parametrik URL param adları və HTTP method istifadə edir.
func (p *HTTPProvider) Send(ctx context.Context, phone, message string) error {
	// Build the URL with query parameters (parametrik adlarla)
	params := url.Values{}
	params.Set(p.paramUser, p.user)
	params.Set(p.paramPassword, p.apiKey)
	params.Set(p.paramPhone, phone)
	params.Set(p.paramSender, p.sender)
	params.Set(p.paramText, message)

	var req *http.Request
	var err error

	if p.httpMethod == "POST" {
		// POST: params body-da
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, strings.NewReader(params.Encode()))
		if err != nil {
			return fmt.Errorf("failed to create SMS request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		// GET (default): params URL-də
		requestURL := p.baseURL + "?" + params.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create SMS request: %w", err)
		}
	}

	// Debug: log the exact URL being sent (mask password for security)
	debugParams := fmt.Sprintf("%s=%s&%s=***&%s=%s&%s=%s",
		p.paramUser, p.user, p.paramPassword, p.paramPhone, phone, p.paramSender, p.sender)
	slog.Info("SMS request", "method", p.httpMethod, "base_url", p.baseURL, "params", debugParams, "provider_user", p.user)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("SMS gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read SMS gateway response: %w", err)
	}

	// SMS gateways return HTTP 200 even on errors — check the body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("SMS gateway returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse the URL-encoded response: errno=100&errtext=OK&message_id=...
	// PR #196: parametrik success_field və success_value istifadə edir
	return parseSMSResponse(string(body), p.successField, p.successValue, p.errorField)
}

// parseSMSResponse checks if the SMS was sent successfully.
// PR #196: parametrik success_field və success_value istifadə edir.
// Əvvəl hardcoded errno=100 idi, indi hər hansı sahə/dəyər yoxlana bilər.
func parseSMSResponse(body, successField, successValue, errorField string) error {
	// Parse URL-encoded response
	values, err := url.ParseQuery(body)
	if err != nil {
		return fmt.Errorf("failed to parse SMS gateway response: %w (body: %s)", err, body)
	}

	fieldValue := values.Get(successField)
	if fieldValue == "" {
		return fmt.Errorf("SMS gateway returned empty %s (body: %s)", successField, body)
	}

	// Success yoxlanışı: fieldValue == successValue
	if fieldValue != successValue {
		errText := values.Get(errorField)
		return fmt.Errorf("SMS gateway error: %s=%s %s=%s", successField, fieldValue, errorField, errText)
	}

	return nil
}

// Name returns "http".
func (p *HTTPProvider) Name() string { return "http" }

// softlineErrorMessages maps Softline error codes to human-readable messages.
// PR #196: bu map yalnız Softline üçün istifadə olunur, amma kod artıq parametrik olduğu
// üçün başqa provayderlər üçün bu map lazım deyil (error_field-dən oxunur).
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
