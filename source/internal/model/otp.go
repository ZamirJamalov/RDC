package model

// OTPSendRequest is the request body for POST /api/otp/send.
type OTPSendRequest struct {
	Phone string `json:"phone"` // customer phone number in E.164 format (+994501234567)
}

// OTPVerifyRequest is the request body for POST /api/otp/verify.
type OTPVerifyRequest struct {
	Phone string `json:"phone"` // customer phone number
	Code  string `json:"code"`  // 6-digit OTP code
}

// OTPSendResponse is returned by POST /api/otp/send.
type OTPSendResponse struct {
	Phone       string `json:"phone"`
	Sent        bool   `json:"sent"`
	ExpiresInS  int    `json:"expires_in_s"` // code validity in seconds
	RetryAfterS int    `json:"retry_after_s,omitempty"` // seconds until the next send is allowed
}

// OTPVerifyResponse is returned by POST /api/otp/verify.
type OTPVerifyResponse struct {
	Phone   string `json:"phone"`
	Valid   bool   `json:"valid"`
	Token   string `json:"token,omitempty"` // verification token — required to create an application
	Attempts int   `json:"attempts_remaining,omitempty"` // remaining verification attempts
}

// OTP code configuration constants.
const (
	OTPCodeLength    = 6     // 6-digit numeric code
	OTPCodeTTL       = 300   // 5 minutes (in seconds)
	OTPMaxAttempts   = 5     // max verification attempts per code
	OTPRateLimitPerMin = 1   // max 1 SMS per minute per phone number
	OTPRateLimitWindow = 60  // rate limit window in seconds
)
