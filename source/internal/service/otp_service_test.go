package service

import (
        "context"
        "testing"

        "rdc-source/internal/model"
)

// mockOTPProvider is a test-only otp.Provider that records calls and can
// simulate failures.
type mockOTPProvider struct {
        sendErr   error
        sendCalls []mockOTPSendCall
}

type mockOTPSendCall struct {
        Phone string
        Code  string
}

func (m *mockOTPProvider) Send(_ context.Context, phone, message string) error {
        m.sendCalls = append(m.sendCalls, mockOTPSendCall{Phone: phone, Code: message})
        return m.sendErr
}

func (m *mockOTPProvider) Name() string { return "mock-test" }

// TestGenerateOTPCode_Length verifies that the generated code has the correct
// length and contains only digits.
func TestGenerateOTPCode_Length(t *testing.T) {
        for _, length := range []int{1, 4, 6, 8} {
                code, err := generateOTPCode(length)
                if err != nil {
                        t.Fatalf("generateOTPCode(%d) error: %v", length, err)
                }
                if len(code) != length {
                        t.Errorf("code length = %d, want %d (code=%s)", len(code), length, code)
                }
                for _, c := range code {
                        if c < '0' || c > '9' {
                                t.Errorf("code contains non-digit: %c (code=%s)", c, code)
                        }
                }
        }
}

func TestGenerateOTPCode_InvalidLength(t *testing.T) {
        _, err := generateOTPCode(0)
        if err == nil {
                t.Error("expected error for length=0, got nil")
        }
        _, err = generateOTPCode(-1)
        if err == nil {
                t.Error("expected error for negative length, got nil")
        }
}

// TestGenerateOTPCode_Randomness verifies that two calls produce different
// codes (extremely high probability with 6 digits).
func TestGenerateOTPCode_Randomness(t *testing.T) {
        codes := make(map[string]bool)
        for i := 0; i < 100; i++ {
                code, err := generateOTPCode(6)
                if err != nil {
                        t.Fatalf("error: %v", err)
                }
                codes[code] = true
        }
        // With 100 6-digit codes, we should have at least 90 unique values
        // (birthday paradox makes collisions rare but possible).
        if len(codes) < 90 {
                t.Errorf("expected at least 90 unique codes out of 100, got %d", len(codes))
        }
}

// TestHashCode_Deterministic verifies that the same code always produces
// the same hash.
func TestHashCode_Deterministic(t *testing.T) {
        h1 := hashCode("123456")
        h2 := hashCode("123456")
        if h1 != h2 {
                t.Error("same code produced different hashes")
        }
}

func TestHashCode_DifferentCodes(t *testing.T) {
        h1 := hashCode("123456")
        h2 := hashCode("654321")
        if h1 == h2 {
                t.Error("different codes produced the same hash (collision)")
        }
}

func TestHashCode_Length(t *testing.T) {
        h := hashCode("123456")
        // SHA-256 = 32 bytes = 64 hex chars
        if len(h) != 64 {
                t.Errorf("hash length = %d, want 64", len(h))
        }
}

// TestGenerateToken verifies the verification token is a 64-char hex string.
func TestGenerateToken(t *testing.T) {
        token, err := generateToken()
        if err != nil {
                t.Fatalf("error: %v", err)
        }
        if len(token) != 64 {
                t.Errorf("token length = %d, want 64", len(token))
        }
}

func TestGenerateToken_Uniqueness(t *testing.T) {
        tokens := make(map[string]bool)
        for i := 0; i < 100; i++ {
                token, err := generateToken()
                if err != nil {
                        t.Fatalf("error: %v", err)
                }
                if tokens[token] {
                        t.Errorf("token collision at iteration %d", i)
                }
                tokens[token] = true
        }
}

// TestOTPService_SendOTP_EmptyPhone verifies that empty phone is rejected.
func TestOTPService_SendOTP_EmptyPhone(t *testing.T) {
        // This test doesn't need a real repo — the service checks phone != ""
        // before touching the repo.
        // We use nil repo — if the service tries to call it, the test will panic
        // (which is fine — we're verifying early return).
        provider := &mockOTPProvider{}
        svc := NewOTPService(provider, nil)

        _, err := svc.SendOTP(context.Background(), "")
        if err == nil {
                t.Error("expected error for empty phone, got nil")
        }
}

// TestOTPService_VerifyOTP_EmptyParams verifies that empty phone or code
// is rejected.
func TestOTPService_VerifyOTP_EmptyParams(t *testing.T) {
        svc := NewOTPService(&mockOTPProvider{}, nil)

        tests := []struct {
                name  string
                phone string
                code  string
        }{
                {"empty phone", "", "123456"},
                {"empty code", "+994501234567", ""},
                {"both empty", "", ""},
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        _, err := svc.VerifyOTP(context.Background(), tc.phone, tc.code)
                        if err == nil {
                                t.Error("expected error, got nil")
                        }
                })
        }
}

// TestOTPConstants verifies the OTP configuration constants have sensible values.
func TestOTPConstants(t *testing.T) {
        if model.OTPCodeLength != 6 {
                t.Errorf("OTPCodeLength = %d, want 6", model.OTPCodeLength)
        }
        if model.OTPCodeTTL != 300 {
                t.Errorf("OTPCodeTTL = %d, want 300 (5 min)", model.OTPCodeTTL)
        }
        if model.OTPMaxAttempts != 5 {
                t.Errorf("OTPMaxAttempts = %d, want 5", model.OTPMaxAttempts)
        }
        if model.OTPRateLimitPerMin != 1 {
                t.Errorf("OTPRateLimitPerMin = %d, want 1", model.OTPRateLimitPerMin)
        }
}
