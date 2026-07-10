package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"rdc-source/pkg/lw"
)

// LWMockHandler handles HTTP requests for the LW mock endpoints
// (formerly MockLmsHandler — now uses unified LW Provider).
type LWMockHandler struct {
	lwProvider lw.Provider
}

// NewLWMockHandler creates a new LWMockHandler.
func NewLWMockHandler(provider lw.Provider) *LWMockHandler {
	return &LWMockHandler{lwProvider: provider}
}

// SetupLoans handles POST /api/mock/lw/setup — sets up mock loan data for a customer.
func (h *LWMockHandler) SetupLoans(w http.ResponseWriter, r *http.Request) {
	var req lw.LoanSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLWMockError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.CustomerPIN == "" {
		writeLWMockError(w, http.StatusBadRequest, "customer_pin is required")
		return
	}

	err := h.lwProvider.SetupCustomerLoans(r.Context(), &req)
	if err != nil {
		writeLWMockError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeLWMockJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "mock LW loan data set up successfully",
		"customer_pin": req.CustomerPIN,
		"loan_count":   len(req.Loans),
	})
}

// QueryLoans handles GET /api/mock/lw/query — returns loan data for a customer.
func (h *LWMockHandler) QueryLoans(w http.ResponseWriter, r *http.Request) {
	customerPIN := r.URL.Query().Get("customer_pin")
	if customerPIN == "" {
		writeLWMockError(w, http.StatusBadRequest, "customer_pin query parameter is required")
		return
	}

	result, err := h.lwProvider.GetCustomerLoans(r.Context(), customerPIN)
	if err != nil {
		writeLWMockError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeLWMockJSON(w, http.StatusOK, result)
}

func writeLWMockJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeLWMockError(w http.ResponseWriter, code int, message string) {
	fmt.Println("LW Mock Error:", message)
	writeLWMockJSON(w, code, map[string]string{"error": message})
}
