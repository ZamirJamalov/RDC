package handler

import (
        "encoding/json"
        "log/slog"
        "net/http"
        "strconv"

        "rdc-source/internal/model"
        "rdc-source/internal/service"
)

// ExpertHandler handles HTTP requests for the credit expert (operator) panel (T-5.7).
//
// Endpoints:
//   - GET  /api/expert/queue             — list pending_approval applications
//   - GET  /api/expert/{id}              — get application details for review
//   - PUT  /api/expert/{id}/approve      — approve with MyGov data
//   - PUT  /api/expert/{id}/reject       — reject with reason
type ExpertHandler struct {
        appSvc *service.ApplicationService
}

// NewExpertHandler creates a new ExpertHandler.
func NewExpertHandler(appSvc *service.ApplicationService) *ExpertHandler {
        return &ExpertHandler{appSvc: appSvc}
}

// Queue handles GET /api/expert/queue.
// Returns all applications in pending_approval status, awaiting manual review.
// Ordered by oldest first (FIFO — experts should review the oldest first).
// Response: [{"id":1,"customer_pin":"...","amount":500,...}, ...]
func (h *ExpertHandler) Queue(w http.ResponseWriter, r *http.Request) {
        apps, err := h.appSvc.ListPendingApproval(r.Context())
        if err != nil {
                slog.Error("expert queue: failed to list pending applications", "error", err)
                writeExpertError(w, http.StatusInternalServerError, "failed to list pending applications")
                return
        }

        writeExpertJSON(w, http.StatusOK, map[string]interface{}{
                "status":       "pending_approval",
                "count":        len(apps),
                "applications": apps,
        })
}

// GetApplication handles GET /api/expert/{id}.
// Returns full application details for the expert to review.
func (h *ExpertHandler) GetApplication(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeExpertError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        app, err := h.appSvc.GetApplication(r.Context(), id)
        if err != nil {
                writeExpertError(w, http.StatusNotFound, err.Error())
                return
        }

        writeExpertJSON(w, http.StatusOK, app)
}

// ApproveRequest is the body for PUT /api/expert/{id}/approve.
type ApproveRequest struct {
        CreditLevel string `json:"credit_level"` // required: new/trusted/valuable/elite
}

// Approve handles PUT /api/expert/{id}/approve.
// Approves a pending_approval application. Requires credit_level in the body.
func (h *ExpertHandler) Approve(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeExpertError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req ApproveRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeExpertError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        if req.CreditLevel == "" {
                writeExpertError(w, http.StatusBadRequest, "credit_level is required")
                return
        }

        if !model.IsValidCreditLevel(req.CreditLevel) {
                writeExpertError(w, http.StatusBadRequest, "credit_level must be one of: new, trusted, valuable, elite")
                return
        }

        // Reuse the existing UpdateStatus service method
        app, err := h.appSvc.UpdateStatus(r.Context(), id, &service.UpdateStatusRequest{
                Status:      model.StatusApproved,
                CreditLevel: req.CreditLevel,
        })
        if err != nil {
                slog.Error("expert approve failed", "application_id", id, "error", err)
                writeExpertError(w, http.StatusBadRequest, err.Error())
                return
        }

        slog.Info("application approved by expert", "application_id", id, "credit_level", req.CreditLevel)
        writeExpertJSON(w, http.StatusOK, app)
}

// RejectRequest is the body for PUT /api/expert/{id}/reject.
type RejectRequest struct {
        Reason string `json:"reason"` // optional: rejection reason
}

// Reject handles PUT /api/expert/{id}/reject.
// Rejects a pending_approval application.
func (h *ExpertHandler) Reject(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeExpertError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req RejectRequest
        // Body is optional — if not provided, defaults to "Manually rejected"
        if r.ContentLength > 0 {
                if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                        writeExpertError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                        return
                }
        }

        app, err := h.appSvc.UpdateStatus(r.Context(), id, &service.UpdateStatusRequest{
                Status: model.StatusRejected,
        })
        if err != nil {
                slog.Error("expert reject failed", "application_id", id, "error", err)
                writeExpertError(w, http.StatusBadRequest, err.Error())
                return
        }

        reason := req.Reason
        if reason == "" {
                reason = "Manually rejected by expert"
        }
        slog.Info("application rejected by expert", "application_id", id, "reason", reason)
        writeExpertJSON(w, http.StatusOK, app)
}

// --- Helpers ---

func writeExpertJSON(w http.ResponseWriter, code int, data interface{}) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(code)
        json.NewEncoder(w).Encode(data)
}

func writeExpertError(w http.ResponseWriter, code int, message string) {
        slog.Warn("expert handler error", "status_code", code, "message", message)
        writeExpertJSON(w, code, map[string]string{"error": message})
}
