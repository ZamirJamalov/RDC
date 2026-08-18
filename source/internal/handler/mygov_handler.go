package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"rdc-source/internal/middleware"
	"rdc-source/internal/service"
)

// MyGovHandler handles HTTP requests for MyGov endpoints (T-4.11).
type MyGovHandler struct {
	svc    *service.MyGovService
	appSvc *service.ApplicationService // PR #148: for audit
}

// NewMyGovHandler creates a new MyGovHandler.
func NewMyGovHandler(svc *service.MyGovService, appSvc *service.ApplicationService) *MyGovHandler {
	return &MyGovHandler{svc: svc, appSvc: appSvc}
}

// myGovPermissionRequest is the request body for POST /api/mygov/permission-link.
// Uses flexInt so application_id accepts both int and string (Postman variables).
type myGovPermissionRequest struct {
	ApplicationID flexInt `json:"application_id"`
	CustomerPIN   string  `json:"customer_pin"`
}

// PermissionLink handles POST /api/mygov/permission-link.
// Request body: {"application_id":42,"customer_pin":"ABC123"}
// application_id accepts both int (1) and string ("1") — needed for Postman variables.
func (h *MyGovHandler) PermissionLink(w http.ResponseWriter, r *http.Request) {
	var req myGovPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMyGovError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	appID := req.ApplicationID.Int()
	if appID <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "application_id must be a positive integer")
		return
	}
	if req.CustomerPIN == "" {
		writeMyGovError(w, http.StatusBadRequest, "customer_pin is required")
		return
	}

	resp, err := h.svc.GenerateLink(r.Context(), appID, req.CustomerPIN)
	if err != nil {
		slog.Error("MyGov GenerateLink failed",
			"application_id", appID,
			"customer_pin", req.CustomerPIN,
			"error", err)
		writeMyGovError(w, http.StatusBadGateway, "failed to generate MyGov permission link: "+err.Error())
		return
	}

	writeMyGovJSON(w, http.StatusOK, resp)
}

// FetchData handles POST /api/mygov/fetch-data?application_id=42
func (h *MyGovHandler) FetchData(w http.ResponseWriter, r *http.Request) {
	appIDStr := r.URL.Query().Get("application_id")
	if appIDStr == "" {
		writeMyGovError(w, http.StatusBadRequest, "application_id query parameter is required")
		return
	}

	appID, err := strconv.Atoi(appIDStr)
	if err != nil || appID <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "application_id must be a positive integer")
		return
	}

	if err := h.svc.FetchData(r.Context(), appID); err != nil {
		slog.Error("MyGov FetchData failed", "application_id", appID, "error", err)
		writeMyGovError(w, http.StatusBadGateway, "failed to fetch MyGov data: "+err.Error())
		return
	}

	writeMyGovJSON(w, http.StatusOK, map[string]interface{}{
		"application_id": appID,
		"status":         "fetched",
		"message":        "MyGov data fetched and stored successfully",
	})
}

// PR #65: Employment verification endpoints

// RequestEmployment handles POST /api/applications/{id}/mygov-employment-request.
// Generates a MyGov permission link and sends it via SMS to the customer.
func (h *MyGovHandler) RequestEmployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "invalid application id")
		return
	}

	resp, err := h.svc.RequestEmploymentVerification(r.Context(), id)
	if err != nil {
		slog.Error("employment verification request failed", "application_id", id, "error", err)
		writeMyGovError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeMyGovJSON(w, http.StatusOK, resp)
}

// VerifyEmployment handles POST /api/applications/{id}/mygov-employment-verify.
// Runs the EMPLOYMENT_TENURE cutoff from the MLSA GetEmployeeInfoByPin response
// (PR #237 — PIN üzərindən, permission token tələb olunmur).
// Auto-rejects the application if the rule fails.
func (h *MyGovHandler) VerifyEmployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "invalid application id")
		return
	}

	// Run the tenure check (GetEmployeeInfoByPin by PIN — no permission flow needed)
	resp, err := h.svc.VerifyEmployment(r.Context(), id)
	if err != nil {
		slog.Error("employment verify failed", "application_id", id, "error", err)
		writeMyGovError(w, http.StatusBadRequest, err.Error())
		return
	}

	// PR #148: audit — hansı ekspert MyGov yoxlaması etdi
	if h.appSvc != nil {
		if user := middleware.PrincipalFromContext(r.Context()); user != nil {
			if err := h.appSvc.SetMyGovAudit(r.Context(), id, user.ID, user.Username); err != nil {
				slog.Error("failed to set mygov audit", "application_id", id, "error", err)
			}
		}
	}

	writeMyGovJSON(w, http.StatusOK, resp)
}

// PR #65: Pension verification endpoints

// RequestPension handles POST /api/applications/{id}/mygov-pension-request.
// Generates a MyGov permission link and sends it via SMS to the customer.
func (h *MyGovHandler) RequestPension(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "invalid application id")
		return
	}

	resp, err := h.svc.RequestPensionVerification(r.Context(), id)
	if err != nil {
		slog.Error("pension verification request failed", "application_id", id, "error", err)
		writeMyGovError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeMyGovJSON(w, http.StatusOK, resp)
}

// VerifyPension handles POST /api/applications/{id}/mygov-pension-verify.
// Runs the DISABILITY_GROUP1 cutoff from the GetPensionInfoByPin response
// (PR #242 — PIN üzərindən, permission token tələb olunmur; iş yeri yoxlaması
// ilə eyni qayda). Auto-rejects the application if DisabilityGroup == 1.
// Nəticə cutoff_results cədvəlinə yazılır.
func (h *MyGovHandler) VerifyPension(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeMyGovError(w, http.StatusBadRequest, "invalid application id")
		return
	}

	// Run the disability check (GetPensionInfoByPin by PIN — no permission flow needed)
	resp, err := h.svc.VerifyPension(r.Context(), id)
	if err != nil {
		slog.Error("pension verify failed", "application_id", id, "error", err)
		writeMyGovError(w, http.StatusBadRequest, "MyGov məlumatları əldə edilə bilmədi: "+err.Error())
		return
	}

	// PR #148: audit — hansı ekspert MyGov yoxlaması etdi
	if h.appSvc != nil {
		if user := middleware.PrincipalFromContext(r.Context()); user != nil {
			if err := h.appSvc.SetMyGovAudit(r.Context(), id, user.ID, user.Username); err != nil {
				slog.Error("failed to set mygov audit", "application_id", id, "error", err)
			}
		}
	}

	writeMyGovJSON(w, http.StatusOK, resp)
}

// --- Helpers ---

func writeMyGovJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeMyGovError(w http.ResponseWriter, code int, message string) {
	slog.Warn("MyGov error", "status_code", code, "message", message)
	writeMyGovJSON(w, code, map[string]string{"error": message})
}
