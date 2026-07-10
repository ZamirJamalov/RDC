package handler

import (
	"log/slog"
	"net/http"

	"rdc-source/internal/middleware"
)

// NewRouter builds the HTTP mux with all application routes registered and the
// standard middleware chain applied. Keeping route registration in one place
// makes it easy to see the full API surface and to add new endpoints without
// touching main.go.
//
// Middleware chain (outer-to-inner):
//  1. RequestID — assigns X-Request-ID to every request
//  2. Recovery  — catches panics, returns 500 instead of crashing
//  3. Logger    — emits one structured log line per request
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

	// Wrap with middleware: RequestID → Recovery → Logger → mux
	var handler http.Handler = mux
	handler = middleware.Logger(slog.Default())(handler)
	handler = middleware.Recovery(slog.Default())(handler)
	handler = middleware.RequestID(handler)

	return handler
}
