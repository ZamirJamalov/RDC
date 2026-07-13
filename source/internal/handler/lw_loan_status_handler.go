package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"rdc-source/internal/repository"
)

// lwLoanStatusCallbackRequest is the payload LW sends to RDC when a loan
// event occurs (contract signed, money transferred, etc.).
// Uses flexInt so application_id accepts both int and string (Postman variables).
type lwLoanStatusCallbackRequest struct {
	ApplicationID flexInt `json:"application_id"`
	EventStatus   string  `json:"event_status"`
	LmsLoanID     string  `json:"lms_loan_id,omitempty"`
	Detail        string  `json:"detail,omitempty"`
}

// LWLoanStatusHandler handles LW loan lifecycle callbacks and polling.
//
// Endpoints:
//   - POST /api/rdc/callback/lw-loan-status  — async callback from LW
//   - GET  /api/applications/{id}/loan-status — polling endpoint
type LWLoanStatusHandler struct {
	eventRepo *repository.LWLoanEventRepo
}

// NewLWLoanStatusHandler creates a new LWLoanStatusHandler.
func NewLWLoanStatusHandler(eventRepo *repository.LWLoanEventRepo) *LWLoanStatusHandler {
	return &LWLoanStatusHandler{eventRepo: eventRepo}
}

// Callback handles POST /api/rdc/callback/lw-loan-status.
// LW calls this endpoint when:
//   - Contract is signed (event_status = "contract_signed")
//   - Money is transferred (event_status = "transfer_completed")
//   - Failure occurs (event_status = "failed")
func (h *LWLoanStatusHandler) Callback(w http.ResponseWriter, r *http.Request) {
	var req lwLoanStatusCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLWLoanStatusError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	appID := req.ApplicationID.Int()
	if appID <= 0 {
		writeLWLoanStatusError(w, http.StatusBadRequest, "application_id must be a positive integer")
		return
	}

	if req.EventStatus == "" {
		writeLWLoanStatusError(w, http.StatusBadRequest, "event_status is required")
		return
	}

	// Persist the event
	if err := h.eventRepo.Create(r.Context(), appID, req.EventStatus, req.LmsLoanID, req.Detail, time.Now()); err != nil {
		slog.Error("failed to save LW loan event",
			"application_id", appID, "event_status", req.EventStatus, "error", err)
		writeLWLoanStatusError(w, http.StatusInternalServerError, "failed to save event: "+err.Error())
		return
	}

	slog.Info("LW loan event received",
		"application_id", appID,
		"event_status", req.EventStatus,
		"lms_loan_id", req.LmsLoanID,
		"detail", req.Detail)

	writeLWLoanStatusJSON(w, http.StatusOK, map[string]interface{}{
		"application_id": appID,
		"received":       true,
		"processed_at":   time.Now().Format(time.RFC3339),
	})
}

// GetStatus handles GET /api/applications/{id}/loan-status.
// Returns all LW loan events for the application, ordered by time.
func (h *LWLoanStatusHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	appID, err := strconv.Atoi(idStr)
	if err != nil || appID <= 0 {
		writeLWLoanStatusError(w, http.StatusBadRequest, "invalid application id")
		return
	}

	events, err := h.eventRepo.GetByApplicationID(r.Context(), appID)
	if err != nil {
		slog.Error("failed to get LW loan events", "application_id", appID, "error", err)
		writeLWLoanStatusError(w, http.StatusInternalServerError, "failed to get events")
		return
	}

	writeLWLoanStatusJSON(w, http.StatusOK, map[string]interface{}{
		"application_id": appID,
		"events":         events,
		"count":          len(events),
	})
}

// --- Helpers ---

func writeLWLoanStatusJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeLWLoanStatusError(w http.ResponseWriter, code int, message string) {
	slog.Warn("LW loan status error", "status_code", code, "message", message)
	writeLWLoanStatusJSON(w, code, map[string]string{"error": message})
}
