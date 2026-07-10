package handler

import (
	"log/slog"
	"net/http"

	"rdc-source/internal/middleware"
)

// NewRouter builds the HTTP mux with all application routes registered and the
// standard middleware chain applied.
//
// Middleware chain (outer-to-inner):
//  1. RequestID — assigns X-Request-ID to every request
//  2. Recovery  — catches panics, returns 500 instead of crashing
//  3. Logger    — emits one structured log line per request
//
// Route groups:
//   - /api/mock/lw/*           — mock LW data setup (dev/test only)
//   - /api/applications/*      — loan application CRUD + status + checks
//   - /api/router/*            — LW router endpoints (personal-info, akb, asan, sima)
//   - /api/lw/*                — LW operations (blacklist, approve)
//   - /api/rdc/callback/*      — async callbacks from LW (sima-result)
func NewRouter(
	appHandler *ApplicationHandler,
	lwMockHandler *LWMockHandler,
	lwRouterHandler *LWRouterHandler,
	lwCallbackHandler *LWCallbackHandler,
) http.Handler {
	mux := http.NewServeMux()

	// LW Mock endpoints (dev/test only)
	mux.HandleFunc("POST /api/mock/lw/setup", lwMockHandler.SetupLoans)
	mux.HandleFunc("GET /api/mock/lw/query", lwMockHandler.QueryLoans)

	// Loan application endpoints
	mux.HandleFunc("POST /api/applications", appHandler.Create)
	mux.HandleFunc("GET /api/applications/{id}", appHandler.GetByID)
	mux.HandleFunc("PUT /api/applications/{id}/status", appHandler.UpdateStatus)
	mux.HandleFunc("GET /api/applications/{id}/status", appHandler.GetStatus)
	mux.HandleFunc("GET /api/applications/{id}/checks", appHandler.GetChecks)

	// LW Router endpoints (T-2.1 to T-2.7)
	mux.HandleFunc("GET /api/router/personal-info", lwRouterHandler.PersonalInfo)
	mux.HandleFunc("GET /api/router/akb-score", lwRouterHandler.AkbScore)
	mux.HandleFunc("GET /api/router/akb-history", lwRouterHandler.AkbHistory)
	mux.HandleFunc("GET /api/router/asan-finance", lwRouterHandler.AsanFinance)
	mux.HandleFunc("POST /api/router/sima/init", lwRouterHandler.SimaInit)

	// LW Operations (T-2.4, T-2.6)
	mux.HandleFunc("GET /api/lw/blacklist", lwRouterHandler.Blacklist)
	mux.HandleFunc("POST /api/lw/loans/approve", lwRouterHandler.ApproveLoan)

	// LW Callbacks (T-2.8)
	mux.HandleFunc("POST /api/rdc/callback/sima-result", lwCallbackHandler.SimaResult)

	// Wrap with middleware: RequestID → Recovery → Logger → mux
	var handler http.Handler = mux
	handler = middleware.Logger(slog.Default())(handler)
	handler = middleware.Recovery(slog.Default())(handler)
	handler = middleware.RequestID(handler)

	return handler
}
