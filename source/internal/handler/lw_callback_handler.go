package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"rdc-source/internal/service"
)

// SimaResultCallbackRequest is the payload LW sends to RDC when the SIMA KYC
// process completes (asynchronously).
//
// Status values: "success", "failed", "expired"
type SimaResultCallbackRequest struct {
	ApplicationID int    `json:"application_id"`
	SessionID     string `json:"session_id"`
	Status        string `json:"status"` // "success", "failed", "expired"
	Detail        string `json:"detail,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"` // RFC3339
}

// SimaResultCallbackResponse confirms receipt of the callback.
type SimaResultCallbackResponse struct {
	ApplicationID int    `json:"application_id"`
	Received      bool   `json:"received"`
	ProcessedAt   string `json:"processed_at"`
}

// LWCallbackHandler handles asynchronous callbacks from LW (T-2.8, T-2.10, T-4.6).
//
// Currently handles:
//   - POST /api/rdc/callback/sima-result — SIMA KYC completion notification
//
// The handler depends on SimaService to persist the result and update the
// application status.
type LWCallbackHandler struct {
	simaService *service.SimaService
}

// NewLWCallbackHandler creates a new LWCallbackHandler.
func NewLWCallbackHandler(simaService *service.SimaService) *LWCallbackHandler {
	return &LWCallbackHandler{simaService: simaService}
}

// SimaResult handles POST /api/rdc/callback/sima-result (T-4.6).
//
// The handler:
//  1. Parses the callback payload
//  2. Calls SimaService.HandleCallback to persist the result
//  3. Returns 200 OK to acknowledge receipt
//
// In a future phase, the handler will also trigger the next pipeline step
// if all checks pass.
func (h *LWCallbackHandler) SimaResult(w http.ResponseWriter, r *http.Request) {
	var req SimaResultCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("SIMA callback: invalid JSON body", "error", err)
		writeCallbackError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.ApplicationID <= 0 {
		writeCallbackError(w, http.StatusBadRequest, "application_id must be a positive integer")
		return
	}

	if req.SessionID == "" {
		writeCallbackError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	// Persist the result via SimaService (T-4.6)
	if err := h.simaService.HandleCallback(r.Context(),
		req.ApplicationID, req.SessionID, req.Status, req.Detail); err != nil {
		slog.Error("failed to process SIMA callback",
			"application_id", req.ApplicationID,
			"session_id", req.SessionID,
			"error", err)
		writeCallbackError(w, http.StatusInternalServerError, "failed to process callback: "+err.Error())
		return
	}

	slog.Info("SIMA KYC callback received and processed",
		"application_id", req.ApplicationID,
		"session_id", req.SessionID,
		"status", req.Status,
		"detail", req.Detail,
		"completed_at", req.CompletedAt)

	writeCallbackJSON(w, http.StatusOK, SimaResultCallbackResponse{
		ApplicationID: req.ApplicationID,
		Received:      true,
		ProcessedAt:   time.Now().Format(time.RFC3339),
	})
}

// --- Helpers ---

func writeCallbackJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeCallbackError(w http.ResponseWriter, code int, message string) {
	slog.Warn("LW callback error", "status_code", code, "message", message)
	writeCallbackJSON(w, code, map[string]string{"error": message})
}
