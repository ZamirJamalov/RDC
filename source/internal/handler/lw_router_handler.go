package handler

import (
        "encoding/json"
        "log/slog"
        "net/http"
        "strconv"

        "rdc-source/pkg/lw"
)
// LWRouterHandler handles HTTP requests for LW router endpoints (T-2.1 to T-2.7).
//
// These endpoints expose the LW Provider's router methods as HTTP APIs so
// external clients (and the frontend) can query LW data directly through RDC.
// In mock mode, the provider returns canned responses; in real mode, the
// provider forwards each request to the real LW system via HTTP.
//
// Route groups:
//   - GET  /api/router/personal-info  — DIN personal info (T-2.1)
//   - GET  /api/router/akb-score      — AKB credit score (T-2.2)
//   - GET  /api/router/akb-history    — AKB full history (T-2.3)
//   - GET  /api/lw/blacklist          — LW blacklist check (T-2.4)
//   - GET  /api/router/asan-finance   — ASAN Finance official income (T-2.5)
//   - POST /api/lw/loans/approve      — push approved loan to LW (T-2.6)
//   - POST /api/router/sima/init      — SIMA KYC initiation (T-2.7)
type LWRouterHandler struct {
        lwProvider lw.Provider
}

// NewLWRouterHandler creates a new LWRouterHandler.
func NewLWRouterHandler(provider lw.Provider) *LWRouterHandler {
        return &LWRouterHandler{lwProvider: provider}
}

// PersonalInfo handles GET /api/router/personal-info?fin=...&serial=...
// Returns the customer's personal information from DIN (via LW router).
func (h *LWRouterHandler) PersonalInfo(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        serial := r.URL.Query().Get("serial")
        if fin == "" {
                writeLWRouterError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        resp, err := h.lwProvider.GetPersonalInfo(r.Context(), fin, serial)
        if err != nil {
                slog.Error("LW GetPersonalInfo failed", "fin", fin, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to fetch personal info from LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, resp)
}

// AkbScore handles GET /api/router/akb-score?fin=...&serial=...
// Returns the customer's AKB credit score (via LW router).
func (h *LWRouterHandler) AkbScore(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        serial := r.URL.Query().Get("serial")
        if fin == "" {
                writeLWRouterError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        resp, err := h.lwProvider.GetAkbScore(r.Context(), fin, serial)
        if err != nil {
                slog.Error("LW GetAkbScore failed", "fin", fin, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to fetch AKB score from LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, resp)
}

// AkbHistory handles GET /api/router/akb-history?fin=...&serial=...
// Returns the customer's full AKB credit history (via LW router).
func (h *LWRouterHandler) AkbHistory(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        serial := r.URL.Query().Get("serial")
        if fin == "" {
                writeLWRouterError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        resp, err := h.lwProvider.GetAkbHistory(r.Context(), fin, serial)
        if err != nil {
                slog.Error("LW GetAkbHistory failed", "fin", fin, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to fetch AKB history from LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, resp)
}

// Blacklist handles GET /api/lw/blacklist?fin=...
// Returns whether the customer is on the LW blacklist.
// Response: {"fin":"...","is_blacklisted":false}
func (h *LWRouterHandler) Blacklist(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeLWRouterError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        blacklisted, err := h.lwProvider.CheckBlacklist(r.Context(), fin)
        if err != nil {
                slog.Error("LW CheckBlacklist failed", "fin", fin, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to check blacklist on LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, map[string]interface{}{
                "fin":            fin,
                "is_blacklisted": blacklisted,
        })
}

// AsanFinance handles GET /api/router/asan-finance?fin=...
// Returns the customer's official income from ASAN Finance (via LW router).
func (h *LWRouterHandler) AsanFinance(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeLWRouterError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        resp, err := h.lwProvider.GetAsanFinance(r.Context(), fin)
        if err != nil {
                slog.Error("LW GetAsanFinance failed", "fin", fin, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to fetch ASAN Finance data from LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, resp)
}

// ApproveLoan handles POST /api/lw/loans/approve
// Pushes an approved loan to LW for contract signing and money transfer.
// Request body: lw.ApproveLoanRequest (application_id, amount, card_number, credit_level, term_months)
func (h *LWRouterHandler) ApproveLoan(w http.ResponseWriter, r *http.Request) {
        var req lw.ApproveLoanRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeLWRouterError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
                return
        }

        if req.ApplicationID <= 0 {
                writeLWRouterError(w, http.StatusBadRequest, "application_id must be a positive integer")
                return
        }

        resp, err := h.lwProvider.ApproveLoan(r.Context(), &req)
        if err != nil {
                slog.Error("LW ApproveLoan failed", "application_id", req.ApplicationID, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to approve loan on LW: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, resp)
}

// SimaInit handles POST /api/router/sima/init?application_id=...
// Initiates the SIMA KYC process for an application.
func (h *LWRouterHandler) SimaInit(w http.ResponseWriter, r *http.Request) {
        appIDStr := r.URL.Query().Get("application_id")
        if appIDStr == "" {
                writeLWRouterError(w, http.StatusBadRequest, "application_id query parameter is required")
                return
        }

        appID, err := strconv.Atoi(appIDStr)
        if err != nil || appID <= 0 {
                writeLWRouterError(w, http.StatusBadRequest, "application_id must be a positive integer")
                return
        }

        if err := h.lwProvider.InitSimaKyc(r.Context(), appID); err != nil {
                slog.Error("LW InitSimaKyc failed", "application_id", appID, "error", err)
                writeLWRouterError(w, http.StatusBadGateway, "failed to initiate SIMA KYC: "+err.Error())
                return
        }

        writeLWRouterJSON(w, http.StatusOK, map[string]interface{}{
                "application_id": appID,
                "status":         "initiated",
                "message":        "SIMA KYC process initiated successfully",
        })
}
