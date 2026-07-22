package handler

import (
        "encoding/json"
        "log/slog"
        "net/http"
        "strconv"

        "rdc-source/internal/service"
)

// InitApplication handles POST /api/applications/init.
// Customer fills in FIN, serial, and phone. An OTP is sent to the phone.
// The application is created with status "pending_customer".
func (h *ApplicationHandler) InitApplication(w http.ResponseWriter, r *http.Request) {
        var req service.InitApplicationRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        app, err := h.service.InitApplication(r.Context(), &req)
        if err != nil {
                slog.Error("init application failed", "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        writeJSON(w, http.StatusCreated, app)
}

// VerifyInitApplication handles POST /api/applications/init/verify.
// Customer enters the OTP code. If valid, application transitions to
// "pending_expert" status (waiting for expert to complete the application).
func (h *ApplicationHandler) VerifyInitApplication(w http.ResponseWriter, r *http.Request) {
        // Use local struct with flexInt so application_id accepts both int and string
        var local struct {
                ApplicationID flexInt `json:"application_id"`
                Phone         string  `json:"phone"`
                OTPCode       string  `json:"otp_code"`
        }
        if err := json.NewDecoder(r.Body).Decode(&local); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        req := &service.VerifyInitApplicationRequest{
                ApplicationID: local.ApplicationID.Int(),
                Phone:         local.Phone,
                OTPCode:       local.OTPCode,
        }

        app, err := h.service.VerifyInitApplication(r.Context(), req)
        if err != nil {
                slog.Error("verify init application failed", "application_id", req.ApplicationID, "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        writeJSON(w, http.StatusOK, app)
}

// CompleteApplication handles PUT /api/applications/{id}/complete.
// Expert fills in the remaining details (name, amount, term, card, contacts,
// address) and triggers the credit engine.
func (h *ApplicationHandler) CompleteApplication(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req service.CompleteApplicationRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        app, err := h.service.CompleteApplication(r.Context(), id, &req)
        if err != nil {
                slog.Error("complete application failed", "application_id", id, "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        writeJSON(w, http.StatusOK, app)
}

// CustomerConfirm handles POST /api/applications/{id}/customer-confirm (PR #58).
// Customer (on the public website) confirms their credit offer by:
//   - selecting an amount from the offered range
//   - entering their 16-digit card number
//   - ticking the "this card belongs to me" checkbox
//   - entering their actual residential address
//
// Backend then fetches full_name (PersonalInfo) and akb_score (AKB) from LW
// router, derives term_months from the offer, and saves everything. Application
// stays in pending_expert — the expert will later add contact phones via
// CompleteApplication.
func (h *ApplicationHandler) CustomerConfirm(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req service.CustomerConfirmRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        app, err := h.service.CustomerConfirmApplication(r.Context(), id, &req)
        if err != nil {
                slog.Error("customer confirm failed", "application_id", id, "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        writeJSON(w, http.StatusOK, app)
}
