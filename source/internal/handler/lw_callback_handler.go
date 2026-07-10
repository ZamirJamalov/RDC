package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// SimaResultCallbackRequest is the payload LW sends to RDC when the SIMA KYC
// process completes (asynchronously). The customer completes KYC on their
// phone; SIMA notifies LW; LW forwards the result to RDC via this callback.
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

// LWCallbackHandler handles asynchronous callbacks from LW (T-2.8, T-2.10).
//
// Currently handles:
//   - POST /api/rdc/callback/sima-result — SIMA KYC completion notification
//
// Future callbacks (when implemented):
//   - MyGov permission result
//   - ASAN Finance async query result
type LWCallbackHandler struct {
	// In a full implementation, this handler would depend on a SimaService
	// to persist the result and update the application status. For now, we
	// just log the callback and return 200 — the actual processing will be
	// added in Phase 4 (SIMA + MyGov).
}

// NewLWCallbackHandler creates a new LWCallbackHandler.
func NewLWCallbackHandler() *LWCallbackHandler {
	return &LWCallbackHandler{}
}

// SimaResult handles POST /api/rdc/callback/sima-result
//
// LW calls this endpoint when the SIMA KYC process completes. The handler:
//  1. Parses the callback payload
//  2. Logs the result (for audit trail)
//  3. Returns 200 OK to acknowledge receipt
//
// In Phase 4, this handler will also:
//  - Persist the result to a sima_sessions table
//  - Update the application status (kyc_completed / kyc_failed)
//  - Trigger the next pipeline step if all checks pass
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

	slog.Info("SIMA KYC callback received",
		"application_id", req.ApplicationID,
		"session_id", req.SessionID,
		"status", req.Status,
		"detail", req.Detail,
		"completed_at", req.CompletedAt,
	)

	// TODO (Phase 4): persist result to sima_sessions table
	// TODO (Phase 4): update application status based on SIMA result
	//   - "success" → continue pipeline (or mark as kyc_completed)
	//   - "failed" / "expired" → reject application with SIMA reason

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
