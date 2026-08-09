package handler

import (
        "encoding/json"
        "log/slog"
        "net/http"
        "strconv"

        "rdc-source/internal/middleware"
        "rdc-source/internal/service"
)

// InitApplication handles POST /api/applications/init.
// Customer fills in FIN, serial, and phone. An OTP is sent to the phone.
// The application is created with status "pending_customer".
// PR #149: Input validation added (FIN, serial, phone format).
func (h *ApplicationHandler) InitApplication(w http.ResponseWriter, r *http.Request) {
        var req service.InitApplicationRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "yanlış request body")
                return
        }

        // PR #149/#150: Input validation
        if !isValidPIN(req.CustomerPIN) {
                writeError(w, http.StatusBadRequest, "FIN kodu 7 simvol olmalıdır (yalnız hərf və rəqəm)")
                return
        }
        // PR #150: serial validation — combined format (prefix + 7 digits)
        if req.CustomerSerial != "" && !isValidSerial(req.CustomerSerial) {
                writeError(w, http.StatusBadRequest, "Seriya nömrəsi düzgün deyil (format: AZE/AA/AB + 7 rəqəm, məs: AZE1234567)")
                return
        }
        if !isValidPhone(req.CustomerPhone) {
                writeError(w, http.StatusBadRequest, "Telefon nömrəsi düzgün deyil (format: +994XXXXXXXXX)")
                return
        }

        app, err := h.service.InitApplication(r.Context(), &req)
        if err != nil {
                slog.Error("init application failed", "error", err)
                // PR #149: sanitize error
                writeError(w, http.StatusBadRequest, sanitizeError(err))
                return
        }

        writeJSON(w, http.StatusCreated, app)
}

// VerifyInitApplication handles POST /api/applications/init/verify.
// Customer enters the OTP code. If valid, application transitions to
// "pending_expert" status (waiting for expert to complete the application).
// PR #149: OTP attempt limit — 3 wrong attempts blocks the application.
func (h *ApplicationHandler) VerifyInitApplication(w http.ResponseWriter, r *http.Request) {
        // Use local struct with flexInt so application_id accepts both int and string
        var local struct {
                ApplicationID flexInt `json:"application_id"`
                Phone         string  `json:"phone"`
                OTPCode       string  `json:"otp_code"`
        }
        if err := json.NewDecoder(r.Body).Decode(&local); err != nil {
                writeError(w, http.StatusBadRequest, "yanlış request body")
                return
        }

        // PR #149: Input validation
        if !isValidPhone(local.Phone) {
                writeError(w, http.StatusBadRequest, "Telefon nömrəsi düzgün deyil")
                return
        }
        // OTP code: 6 digits
        if len(local.OTPCode) != 6 {
                writeError(w, http.StatusBadRequest, "OTP kodu 6 rəqəm olmalıdır")
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
                // PR #149: sanitize error
                writeError(w, http.StatusBadRequest, sanitizeError(err))
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

// UpdateContacts handles PUT /api/applications/{id}/contacts.
// PR #124: ekspert kontakt nömrələrini və yoxlanma statusunu saxlayır.
// pending_approval statusunda da işləyir (CompleteApplication-a alternativ).
func (h *ApplicationHandler) UpdateContacts(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req service.UpdateContactsRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        app, err := h.service.UpdateContacts(r.Context(), id, &req)
        if err != nil {
                slog.Error("update contacts failed", "application_id", id, "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        // PR #148: audit — hansı ekspert kontaktları yenilədi
        if user := middleware.PrincipalFromContext(r.Context()); user != nil {
                if err := h.service.SetContactsAudit(r.Context(), id, user.ID, user.Username); err != nil {
                        slog.Error("failed to set contacts audit", "application_id", id, "error", err)
                }
        }

        writeJSON(w, http.StatusOK, app)
}

// UpdateTimer handles PUT /api/applications/{id}/timer.
// PR #134: müraciət üzərində işləmə vaxtını saxlayır.
func (h *ApplicationHandler) UpdateTimer(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req struct {
                TimerSeconds int `json:"timer_seconds"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        if err := h.service.UpdateTimer(r.Context(), id, req.TimerSeconds); err != nil {
                slog.Error("update timer failed", "application_id", id, "error", err)
                writeError(w, http.StatusBadRequest, err.Error())
                return
        }

        // PR #148: audit — hansı ekspert timer-ı saxladı
        if user := middleware.PrincipalFromContext(r.Context()); user != nil {
                if err := h.service.SetTimerAudit(r.Context(), id, user.ID, user.Username); err != nil {
                        slog.Error("failed to set timer audit", "application_id", id, "error", err)
                }
        }

        writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
//
// PR #149: IDOR protection — verify that the application's customer_phone matches
// the phone in the request body. This prevents user A from confirming user B's
// application by changing the ID in the URL.
func (h *ApplicationHandler) CustomerConfirm(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil || id <= 0 {
                writeError(w, http.StatusBadRequest, "invalid application id")
                return
        }

        var req service.CustomerConfirmRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeError(w, http.StatusBadRequest, "yanlış request body")
                return
        }

        // PR #149: IDOR check — fetch application first, verify ownership
        existingApp, err := h.service.GetApplication(r.Context(), id)
        if err != nil {
                slog.Error("customer confirm: application not found", "application_id", id, "error", err)
                writeError(w, http.StatusNotFound, "müraciət tapılmadı")
                return
        }
        // Verify the phone in the request matches the application's customer_phone
        if existingApp.CustomerPhone != "" && req.CustomerPhone != existingApp.CustomerPhone {
                slog.Warn("IDOR attempt blocked",
                        "application_id", id,
                        "app_phone", existingApp.CustomerPhone,
                        "request_phone", req.CustomerPhone,
                )
                writeError(w, http.StatusForbidden, "bu müraciəti təsdiqləmək hüququnuz yoxdur")
                return
        }
        // Verify application is in correct status (pending_expert)
        if existingApp.Status != "pending_expert" {
                slog.Warn("customer confirm: wrong status", "application_id", id, "status", existingApp.Status)
                writeError(w, http.StatusBadRequest, "bu müraciət artıq təsdiqlənib və ya redd edilib")
                return
        }

        app, err := h.service.CustomerConfirmApplication(r.Context(), id, &req)
        if err != nil {
                slog.Error("customer confirm failed", "application_id", id, "error", err)
                // PR #149: sanitize error
                writeError(w, http.StatusBadRequest, sanitizeError(err))
                return
        }

        writeJSON(w, http.StatusOK, app)
}
