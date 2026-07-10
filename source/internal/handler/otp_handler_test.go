package handler

import (
        "context"
        "encoding/json"
        "errors"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "rdc-source/internal/model"
        "rdc-source/internal/service"
)

// mockOTPProviderForHandler is a minimal mock for testing OTPHandler.
type mockOTPProviderForHandler struct {
        sendErr error
        calls   []struct{ phone, code string }
}

func (m *mockOTPProviderForHandler) Send(_ context.Context, phone, code string) error {
        m.calls = append(m.calls, struct{ phone, code string }{phone, code})
        return m.sendErr
}

func (m *mockOTPProviderForHandler) Name() string { return "mock-test" }

// Ensure the imports are used (compile guards for test scaffolding).
var (
        _ = context.Background
        _ = model.OTPCodeLength
        _ = service.NewOTPService
        _ = errors.New
)

// TestOTPHandler_Send_Success verifies the happy path for POST /api/otp/send.
func TestOTPHandler_Send_Success(t *testing.T) {
        // Note: this test calls the handler directly, which requires an OTPService.
        // Since OTPService needs a real *repository.OTPRepo (concrete type, not
        // interface), we can't easily inject a mock repo here. For now, we test
        // only the request validation paths (which don't reach the service).
        //
        // Full handler tests require either:
        //   1. Extracting an OTPStore interface (like ApplicationStore), or
        //   2. Using a test database
        // Both are out of scope for this Phase 3 PR — the service-level tests
        // in otp_service_test.go cover the pure-function logic.

        // This is a placeholder — see TestOTPHandler_Send_InvalidBody below
        // for tests that don't need the service.
        t.Skip("full send test requires mock OTP repo — see TestOTPHandler_Send_InvalidBody for validation tests")
}

// TestOTPHandler_Send_InvalidBody verifies that malformed JSON returns 400.
func TestOTPHandler_Send_InvalidBody(t *testing.T) {
        // We can test validation without a real service — pass nil.
        // The handler will try to decode the body first, fail, and return 400
        // before touching the service.
        h := NewOTPHandler(nil)

        req := httptest.NewRequest("POST", "/api/otp/send", strings.NewReader("not json"))
        w := httptest.NewRecorder()

        h.Send(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// TestOTPHandler_Send_EmptyPhone verifies that empty phone returns 400.
func TestOTPHandler_Send_EmptyPhone(t *testing.T) {
        h := NewOTPHandler(nil)

        body := `{"phone":""}`
        req := httptest.NewRequest("POST", "/api/otp/send", strings.NewReader(body))
        w := httptest.NewRecorder()

        h.Send(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }

        var resp map[string]string
        json.NewDecoder(w.Body).Decode(&resp)
        if !strings.Contains(resp["error"], "phone is required") {
                t.Errorf("error = %q, want 'phone is required'", resp["error"])
        }
}

// TestOTPHandler_Verify_InvalidBody verifies that malformed JSON returns 400.
func TestOTPHandler_Verify_InvalidBody(t *testing.T) {
        h := NewOTPHandler(nil)

        req := httptest.NewRequest("POST", "/api/otp/verify", strings.NewReader("not json"))
        w := httptest.NewRecorder()

        h.Verify(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// TestOTPHandler_Verify_EmptyFields verifies that empty phone or code returns 400.
func TestOTPHandler_Verify_EmptyFields(t *testing.T) {
        h := NewOTPHandler(nil)

        tests := []struct {
                name string
                body string
        }{
                {"empty phone", `{"phone":"","code":"123456"}`},
                {"empty code", `{"phone":"+994501234567","code":""}`},
                {"both empty", `{"phone":"","code":""}`},
        }

        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        req := httptest.NewRequest("POST", "/api/otp/verify", strings.NewReader(tc.body))
                        w := httptest.NewRecorder()

                        h.Verify(w, req)

                        if w.Code != http.StatusBadRequest {
                                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
                        }
                })
        }
}

// Ensure the errors import is used (compile guard for future tests that need it).
var _ = errors.New
