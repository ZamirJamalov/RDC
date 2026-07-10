package handler

import "net/http"

// NewRouter builds the HTTP mux with all application routes registered.
// Keeping route registration in one place makes it easy to see the full
// API surface and to add new endpoints without touching main.go.
//
// Route groups:
//   - /api/mock/lw/*        — mock LW data setup (dev/test only)
//   - /api/applications/*   — loan application CRUD + status + checks
func NewRouter(appHandler *ApplicationHandler, lwMockHandler *LWMockHandler) http.Handler {
	mux := http.NewServeMux()

	// LW Mock endpoints (formerly Mock LMS)
	mux.HandleFunc("POST /api/mock/lw/setup", lwMockHandler.SetupLoans)
	mux.HandleFunc("GET /api/mock/lw/query", lwMockHandler.QueryLoans)

	// Loan application endpoints
	mux.HandleFunc("POST /api/applications", appHandler.Create)
	mux.HandleFunc("GET /api/applications/{id}", appHandler.GetByID)
	mux.HandleFunc("PUT /api/applications/{id}/status", appHandler.UpdateStatus)
	mux.HandleFunc("GET /api/applications/{id}/status", appHandler.GetStatus)
	mux.HandleFunc("GET /api/applications/{id}/checks", appHandler.GetChecks)

	return mux
}
