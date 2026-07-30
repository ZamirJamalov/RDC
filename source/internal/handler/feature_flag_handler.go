package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"rdc-source/internal/service"
)

// FeatureFlagHandler handles HTTP requests for feature flag management.
//
// PR #98: admin endpoints to toggle and inspect feature flags.
//
// Endpoints:
//   - GET  /api/admin/feature-flags
//     Returns all feature flags (key, value, description).
//
//   - GET  /api/admin/feature-flags/{key}
//     Returns a single feature flag.
//
//   - PUT  /api/admin/feature-flags/{key}
//     Updates a feature flag. Body: {"enabled": true|false}
//     The {key} must be a known flag (currently only "discount_codes_enabled").
type FeatureFlagHandler struct {
	flags *service.FeatureFlagService
}

// NewFeatureFlagHandler creates a new FeatureFlagHandler.
func NewFeatureFlagHandler(flags *service.FeatureFlagService) *FeatureFlagHandler {
	return &FeatureFlagHandler{flags: flags}
}

// featureFlagResponse is the JSON shape for a single flag.
type featureFlagResponse struct {
	Key         string `json:"key"`
	Enabled     bool   `json:"enabled"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// toggleFlagRequest is the body for PUT /api/admin/feature-flags/{key}.
type toggleFlagRequest struct {
	Enabled bool `json:"enabled"`
}

// knownFlags lists the flags that can be toggled via this API.
// Unknown keys are rejected with 400 Bad Request.
var knownFlags = map[string]bool{
	"discount_codes_enabled": true,
}

// List handles GET /api/admin/feature-flags.
// Returns all feature flags.
func (h *FeatureFlagHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.flags == nil {
		writeFlagError(w, http.StatusServiceUnavailable, "feature flag service not wired")
		return
	}
	settings, err := h.flags.List(r.Context())
	if err != nil {
		slog.Error("feature-flags list failed", "error", err)
		writeFlagError(w, http.StatusInternalServerError, "failed to list flags")
		return
	}

	resp := make([]featureFlagResponse, 0, len(settings))
	for _, s := range settings {
		resp = append(resp, featureFlagResponse{
			Key:         s.Key,
			Enabled:     s.Value == "1" || s.Value == "true",
			Value:       s.Value,
			Description: s.Description,
		})
	}

	writeFlagJSON(w, http.StatusOK, map[string]interface{}{
		"flags": resp,
	})
}

// Get handles GET /api/admin/feature-flags/{key}.
// Returns a single feature flag.
func (h *FeatureFlagHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.flags == nil {
		writeFlagError(w, http.StatusServiceUnavailable, "feature flag service not wired")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		// Fallback for older Go versions: parse from URL path
		key = strings.TrimPrefix(r.URL.Path, "/api/admin/feature-flags/")
	}
	if !knownFlags[key] {
		writeFlagError(w, http.StatusBadRequest, "unknown feature flag: "+key)
		return
	}

	setting, err := h.flags.Get(r.Context(), key)
	if err != nil {
		slog.Error("feature-flags get failed", "key", key, "error", err)
		writeFlagError(w, http.StatusNotFound, "flag not found")
		return
	}

	writeFlagJSON(w, http.StatusOK, featureFlagResponse{
		Key:         setting.Key,
		Enabled:     setting.Value == "1" || setting.Value == "true",
		Value:       setting.Value,
		Description: setting.Description,
	})
}

// Toggle handles PUT /api/admin/feature-flags/{key}.
// Body: {"enabled": true|false}
//
// Example: turn OFF the discount code feature with one command:
//
//	curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled \
//	  -H "Content-Type: application/json" \
//	  -d '{"enabled": false}'
//
// And turn it back ON:
//
//	curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled \
//	  -H "Content-Type: application/json" \
//	  -d '{"enabled": true}'
func (h *FeatureFlagHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	if h.flags == nil {
		writeFlagError(w, http.StatusServiceUnavailable, "feature flag service not wired")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		key = strings.TrimPrefix(r.URL.Path, "/api/admin/feature-flags/")
	}
	if !knownFlags[key] {
		writeFlagError(w, http.StatusBadRequest, "unknown feature flag: "+key)
		return
	}

	var req toggleFlagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFlagError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Route to the appropriate setter based on the flag key.
	switch key {
	case "discount_codes_enabled":
		if err := h.flags.SetDiscountCodesEnabled(r.Context(), req.Enabled); err != nil {
			slog.Error("feature-flags toggle failed",
				"key", key,
				"enabled", req.Enabled,
				"error", err)
			writeFlagError(w, http.StatusInternalServerError, "failed to update flag")
			return
		}
	default:
		writeFlagError(w, http.StatusBadRequest, "unknown feature flag: "+key)
		return
	}

	slog.Info("feature flag toggled",
		"key", key,
		"enabled", req.Enabled)

	writeFlagJSON(w, http.StatusOK, map[string]interface{}{
		"key":     key,
		"enabled": req.Enabled,
		"message": "feature flag updated",
	})
}

// --- Helpers ---

func writeFlagJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeFlagError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
