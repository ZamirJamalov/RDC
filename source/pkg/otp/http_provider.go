package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HTTPProvider implements the OTP Provider interface by calling a real SMS
// gateway via HTTP. This is the production implementation — the MockProvider
// is used in dev/test.
//
// The provider is intentionally agnostic of the SMS gateway vendor (Clickatell,
// Twilio, local AZ provider, etc.). It sends a POST request with a JSON body
// containing the phone number and code. The exact request/response format
// depends on the SMS gateway — when you choose a vendor, you may need to:
//   1. Adjust the request body structure (SendSMSRequest)
//   2. Adjust the response parsing (SendSMSResponse)
//   3. Add vendor-specific auth (API key header, basic auth, etc.)
//
// The current implementation uses a generic JSON structure that works with
// most REST-based SMS gateways. Vendor-specific wrappers can be added later.
type HTTPProvider struct {
	baseURL string
	apiKey  string
	sender  string // sender ID shown on the customer's phone
	client  *http.Client
}

// NewHTTPProvider creates a new HTTPProvider with the given configuration.
func NewHTTPProvider(baseURL, apiKey, sender string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		sender:  sender,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// SendSMSRequest is the JSON body sent to the SMS gateway.
type SendSMSRequest struct {
	To      string `json:"to"`      // customer phone number (E.164 format: +994501234567)
	From    string `json:"from"`    // sender ID
	Message string `json:"message"` // full SMS text including the code
	Code    string `json:"code"`    // the OTP code (for gateways that use templates)
}

// SendSMSResponse is the expected response from the SMS gateway.
type SendSMSResponse struct {
	Success  bool   `json:"success"`
	MessageID string `json:"message_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Send delivers the OTP code via the SMS gateway.
func (p *HTTPProvider) Send(ctx context.Context, phone, code string) error {
	reqBody := SendSMSRequest{
		To:      phone,
		From:    p.sender,
		Message: fmt.Sprintf("Your RDC verification code: %s. Do not share it with anyone.", code),
		Code:    code,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal SMS request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/sms/send", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create SMS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("SMS gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	var smsResp SendSMSResponse
	if err := json.NewDecoder(resp.Body).Decode(&smsResp); err != nil {
		return fmt.Errorf("failed to decode SMS gateway response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !smsResp.Success {
		errMsg := smsResp.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("SMS gateway rejected the request: %s", errMsg)
	}

	return nil
}

// Name returns "http".
func (p *HTTPProvider) Name() string {
	return "http"
}
