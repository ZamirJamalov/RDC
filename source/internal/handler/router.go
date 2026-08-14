package handler

import (
        "log/slog"
        "net/http"

        "rdc-source/internal/middleware"
        "rdc-source/internal/service"
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
//   - /api/auth/*              — authentication (login, logout, me, change-password) [PR #142]
//   - /api/admin/users/*       — admin user management (admin-only) [PR #142]
//   - /api/mock/lw/*           — mock LW data setup (dev/test only)
//   - /api/applications/*      — loan application CRUD + status + checks
//   - /api/router/*            — LW router endpoints (personal-info, akb, asan, sima)
//   - /api/lw/*                — LW operations (blacklist, approve)
//   - /api/rdc/callback/*      — async callbacks from LW (sima-result)
//   - /api/otp/*               — OTP send/verify (T-3.8)
//   - /api/discount-codes/*    — discount code validation (PR #96)
//
// PR #142: Protected routes (expert, admin, mygov, contacts, timer) are wrapped
// with middleware.RequireAuth. Admin-only routes additionally use RequireAdmin.
func NewRouter(
        appHandler *ApplicationHandler,
        lwMockHandler *LWMockHandler,
        lwRouterHandler *LWRouterHandler,
        lwCallbackHandler *LWCallbackHandler,
        otpHandler *OTPHandler,
        mygovHandler *MyGovHandler,
        expertHandler *ExpertHandler,
        lwLoanStatusHandler *LWLoanStatusHandler,
        discountCodeHandler *DiscountCodeHandler, // PR #96
        featureFlagHandler *FeatureFlagHandler,   // PR #98
        authHandler *AuthHandler,                 // PR #142
        userHandler *UserHandler,                 // PR #142
        authSvc *service.AuthService,             // PR #142
        allowedOrigin string,                     // PR #149: CORS
        apiLimiter *middleware.RateLimiter,       // PR #149: generic API rate limit
        otpLimiter *middleware.RateLimiter,       // PR #149: OTP rate limit
        discountLimiter *middleware.RateLimiter,  // PR #149: discount code rate limit
) http.Handler {
        mux := http.NewServeMux()

        // --- PR #142: Auth endpoints ---
        // Login + logout are public (no auth required)
        mux.HandleFunc("POST /api/auth/login", authHandler.Login)
        mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)
        // PR #147: /me and /change-password REQUIRE auth — must be wrapped with RequireAuth
        //   Otherwise the principal is never set in context and Me() always returns 401.
        mux.Handle("GET /api/auth/me", middleware.RequireAuth(authSvc)(http.HandlerFunc(authHandler.Me)))
        mux.Handle("POST /api/auth/change-password", middleware.RequireAuth(authSvc)(http.HandlerFunc(authHandler.ChangePassword)))

        // --- PR #142: Admin user management (admin-only) ---
        adminAuth := middleware.RequireAdmin()
        mux.Handle("GET /api/admin/users", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.ListUsers))))
        mux.Handle("POST /api/admin/users", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.CreateUser))))
        mux.Handle("PUT /api/admin/users/{id}/role", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.UpdateRole))))
        mux.Handle("PUT /api/admin/users/{id}/active", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.SetActive))))
        mux.Handle("PUT /api/admin/users/{id}/lock", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.LockUser))))
        mux.Handle("PUT /api/admin/users/{id}/unlock", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.UnlockUser))))
        mux.Handle("PUT /api/admin/users/{id}/password", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.ResetPassword))))
        mux.Handle("DELETE /api/admin/users/{id}", middleware.RequireAuth(authSvc)(adminAuth(http.HandlerFunc(userHandler.DeleteUser))))

        // --- LW Mock endpoints (dev/test only) ---
        mux.HandleFunc("POST /api/mock/lw/setup", lwMockHandler.SetupLoans)
        mux.HandleFunc("GET /api/mock/lw/query", lwMockHandler.QueryLoans)

        // --- Loan application endpoints (PUBLIC — used by apply.html customer form) ---
        // PR #149: Rate-limited public endpoints
        mux.Handle("POST /api/applications/init", middleware.RateLimit(apiLimiter)(http.HandlerFunc(appHandler.InitApplication)))
        mux.Handle("POST /api/applications/init/verify", middleware.RateLimit(apiLimiter)(http.HandlerFunc(appHandler.VerifyInitApplication)))
        mux.HandleFunc("POST /api/applications", appHandler.Create)
        mux.HandleFunc("POST /api/applications/{id}/customer-confirm", appHandler.CustomerConfirm) // PR #58
        // PR #149: Changed GET → POST (FIN in body, not URL query param)
        mux.Handle("POST /api/applications/offer", middleware.RateLimit(apiLimiter)(http.HandlerFunc(appHandler.GetOffer)))
        mux.HandleFunc("GET /api/applications/{id}", appHandler.GetByID)
        mux.HandleFunc("GET /api/applications/{id}/status", appHandler.GetStatus)
        // PR #188: Video record endpoints (public — müştəri özü çağırır)
        mux.Handle("POST /api/applications/{id}/video-record/start", middleware.RateLimit(apiLimiter)(http.HandlerFunc(appHandler.StartVideoRecord)))
        mux.HandleFunc("GET /api/applications/{id}/video-record/status", appHandler.CheckVideoRecordStatus)
        mux.HandleFunc("GET /api/applications/{id}/video-record", appHandler.GetVideoRecord)
        mux.HandleFunc("GET /api/applications/video-required", appHandler.GetApplicationVideoRequired)

        // --- Loan application endpoints (PROTECTED — used by detail.html expert dashboard) ---
        protectedAuth := middleware.RequireAuth(authSvc)
        mux.Handle("PUT /api/applications/{id}/complete", protectedAuth(http.HandlerFunc(appHandler.CompleteApplication)))
        mux.Handle("PUT /api/applications/{id}/contacts", protectedAuth(http.HandlerFunc(appHandler.UpdateContacts))) // PR #124
        mux.Handle("PUT /api/applications/{id}/timer", protectedAuth(http.HandlerFunc(appHandler.UpdateTimer)))       // PR #134
        mux.Handle("PUT /api/applications/{id}/status", protectedAuth(http.HandlerFunc(appHandler.UpdateStatus)))
        mux.Handle("GET /api/applications/{id}/checks", protectedAuth(http.HandlerFunc(appHandler.GetChecks)))
        mux.Handle("GET /api/applications/{id}/loan-status", protectedAuth(http.HandlerFunc(lwLoanStatusHandler.GetStatus)))

        // --- LW Router endpoints (T-2.1 to T-2.7) — internal, protected ---
        mux.Handle("GET /api/router/personal-info", protectedAuth(http.HandlerFunc(lwRouterHandler.PersonalInfo)))
        mux.Handle("GET /api/router/akb-score", protectedAuth(http.HandlerFunc(lwRouterHandler.AkbScore)))
        mux.Handle("GET /api/router/akb-history", protectedAuth(http.HandlerFunc(lwRouterHandler.AkbHistory)))
        mux.Handle("GET /api/router/asan-finance", protectedAuth(http.HandlerFunc(lwRouterHandler.AsanFinance)))
        mux.Handle("POST /api/router/sima/init", protectedAuth(http.HandlerFunc(lwRouterHandler.SimaInit)))
        mux.Handle("GET /api/router/azmk-blacklist", protectedAuth(http.HandlerFunc(lwRouterHandler.AzmkBlacklist))) // PR #53

        // --- LW Operations (T-2.4, T-2.6) — internal, protected ---
        mux.Handle("GET /api/lw/blacklist", protectedAuth(http.HandlerFunc(lwRouterHandler.Blacklist)))
        mux.Handle("POST /api/lw/loans/approve", protectedAuth(http.HandlerFunc(lwRouterHandler.ApproveLoan)))

        // --- LW Callbacks (T-2.8) — external services call these, NOT browser ---
        // TODO PR #144: add HMAC signature verification
        mux.HandleFunc("POST /api/rdc/callback/sima-result", lwCallbackHandler.SimaResult)
        mux.HandleFunc("POST /api/rdc/callback/lw-loan-status", lwLoanStatusHandler.Callback)

        // --- OTP endpoints (T-3.8) — public, used by apply.html ---
        // PR #149: Rate-limited (stricter — otpLimiter)
        mux.Handle("POST /api/otp/send", middleware.RateLimit(otpLimiter)(http.HandlerFunc(otpHandler.Send)))
        mux.Handle("POST /api/otp/verify", middleware.RateLimit(otpLimiter)(http.HandlerFunc(otpHandler.Verify)))

        // --- MyGov endpoints (T-4.11) — protected, used by detail.html ---
        mux.Handle("POST /api/mygov/permission-link", protectedAuth(http.HandlerFunc(mygovHandler.PermissionLink)))
        mux.Handle("POST /api/mygov/fetch-data", protectedAuth(http.HandlerFunc(mygovHandler.FetchData)))

        // PR #65: Employment + Pension verification endpoints — protected
        mux.Handle("POST /api/applications/{id}/mygov-employment-request", protectedAuth(http.HandlerFunc(mygovHandler.RequestEmployment)))
        mux.Handle("POST /api/applications/{id}/mygov-employment-verify", protectedAuth(http.HandlerFunc(mygovHandler.VerifyEmployment)))
        mux.Handle("POST /api/applications/{id}/mygov-pension-request", protectedAuth(http.HandlerFunc(mygovHandler.RequestPension)))
        mux.Handle("POST /api/applications/{id}/mygov-pension-verify", protectedAuth(http.HandlerFunc(mygovHandler.VerifyPension)))

        // --- Expert (operator) endpoints (T-5.7) — protected ---
        mux.Handle("GET /api/expert/queue", protectedAuth(http.HandlerFunc(expertHandler.Queue)))
        mux.Handle("GET /api/expert/{id}", protectedAuth(http.HandlerFunc(expertHandler.GetApplication)))
        mux.Handle("PUT /api/expert/{id}/approve", protectedAuth(http.HandlerFunc(expertHandler.Approve)))
        mux.Handle("PUT /api/expert/{id}/reject", protectedAuth(http.HandlerFunc(expertHandler.Reject)))

        // --- PR #96: Discount code validation (public endpoint, used by apply.html) ---
        // PR #149: Rate-limited to prevent brute-force abuse
        mux.Handle("GET /api/discount-codes/validate", middleware.RateLimit(discountLimiter)(http.HandlerFunc(discountCodeHandler.Validate)))

        // --- PR #98: Feature flag management (admin endpoints) — now protected ---
        mux.Handle("GET /api/admin/feature-flags", protectedAuth(adminAuth(http.HandlerFunc(featureFlagHandler.List))))
        mux.Handle("GET /api/admin/feature-flags/{key}", protectedAuth(adminAuth(http.HandlerFunc(featureFlagHandler.Get))))
        mux.Handle("PUT /api/admin/feature-flags/{key}", protectedAuth(adminAuth(http.HandlerFunc(featureFlagHandler.Toggle))))

        // Wrap with middleware: CORS → RequestID → Recovery → Logger → mux
        // PR #149: CORS added as outermost middleware
        var handler http.Handler = mux
        handler = middleware.Logger(slog.Default())(handler)
        handler = middleware.Recovery(slog.Default())(handler)
        handler = middleware.RequestID(handler)
        handler = middleware.CORS(allowedOrigin)(handler)

        return handler
}
