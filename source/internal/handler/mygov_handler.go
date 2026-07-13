package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"rdc-source/internal/service"
)

// MyGovHandler handles HTTP requests for MyGov endpoints (T-4.11).
type MyGovHandler struct {
	svc *service.MyGovService
}

// NewMyGovHandler creates a new MyGovHandler.
func NewMyGovHandler(svc *service.MyGovService) *MyGovHandler {
	return &MyGovHandler{svc: svc}
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
