package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// OTPHandler handles HTTP requests for OTP endpoints (T-3.8).
//
// Endpoints:
//   - POST /api/otp/send   — send a 6-digit code via SMS
//   - POST /api/otp/verify — verify the code and get a verification token
type OTPHandler struct {
	svc *service.OTPService
}

// NewOTPHandler creates a new OTPHandler.
func NewOTPHandler(svc *service.OTPService) *OTPHandler {
	return &OTPHandler{svc: svc}
}

// Send handles POST /api/otp/send.
// Request body: {"phone": "+994501234567"}
// Response: {"phone":"...","sent":true,"expires_in_s":300}
//
// If the rate limit is exceeded, returns sent=false with retry_after_s.
func (h *OTPHandler) Send(w http.ResponseWriter, r *http.Request) {
	var req model.OTPSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOTPError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Phone == "" {
		writeOTPError(w, http.StatusBadRequest, "phone is required")
		return
	}

	resp, err := h.svc.SendOTP(r.Context(), req.Phone)
	if err != nil {
		slog.Error("OTP send failed", "phone", req.Phone, "error", err)
		writeOTPError(w, http.StatusInternalServerError, "failed to send OTP: "+err.Error())
		return
	}

	// If rate-limited, return 429 (Too Many Requests)
	if !resp.Sent {
		writeOTPJSON(w, http.StatusTooManyRequests, resp)
		return
	}

	writeOTPJSON(w, http.StatusOK, resp)
}

// Verify handles POST /api/otp/verify.
// Request body: {"phone": "+994501234567", "code": "123456"}
// Response: {"phone":"...","valid":true,"token":"abc123..."}
//
// The token must be included when creating a loan application to prove the
// phone number was verified.
func (h *OTPHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req model.OTPVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOTPError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Phone == "" || req.Code == "" {
		writeOTPError(w, http.StatusBadRequest, "phone and code are required")
		return
	}

	resp, err := h.svc.VerifyOTP(r.Context(), req.Phone, req.Code)
	if err != nil {
		slog.Error("OTP verify failed", "phone", req.Phone, "error", err)
		writeOTPError(w, http.StatusInternalServerError, "failed to verify OTP: "+err.Error())
		return
	}

	if !resp.Valid {
		// Return 200 with valid=false — the client uses the response body
		// to show "wrong code, X attempts remaining" rather than treating
		// it as a server error.
		writeOTPJSON(w, http.StatusOK, resp)
		return
	}

	writeOTPJSON(w, http.StatusOK, resp)
}

// --- Helpers ---

func writeOTPJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeOTPError(w http.ResponseWriter, code int, message string) {
	slog.Warn("OTP error", "status_code", code, "message", message)
	writeOTPJSON(w, code, map[string]string{"error": message})
}
