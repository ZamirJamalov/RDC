package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// ApplicationHandler handles HTTP requests for loan application endpoints.
type ApplicationHandler struct {
	service *service.ApplicationService
}

// NewApplicationHandler creates a new ApplicationHandler.
func NewApplicationHandler(svc *service.ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{service: svc}
}

// Create handles POST /api/applications — creates a new loan application.
func (h *ApplicationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req model.CreateApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	app, err := h.service.CreateApplication(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, app)
}

// GetByID handles GET /api/applications/{id} — retrieves a single application.
func (h *ApplicationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	app, err := h.service.GetApplication(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, app)
}

// GetStatus handles GET /api/applications/{id}/status — returns full status with checks and decision.
func (h *ApplicationHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status, err := h.service.GetStatus(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// UpdateStatus handles PUT /api/applications/{id}/status — manually sets application status (mock endpoint).
// This allows testing unlock_phase progression by approving a loan and re-applying.
func (h *ApplicationHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req service.UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	app, err := h.service.UpdateStatus(r.Context(), id, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, app)
}

// GetChecks handles GET /api/applications/{id}/checks — returns all check results for an application.
func (h *ApplicationHandler) GetChecks(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	checks, err := h.service.GetChecks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, checks)
}

// parsePathID converts a URL path parameter string to a positive integer.
func parsePathID(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("id path parameter is required")
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid id: must be an integer")
	}
	if id <= 0 {
		return 0, fmt.Errorf("invalid id: must be a positive integer")
	}
	return id, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}
