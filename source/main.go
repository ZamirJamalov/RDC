package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"rdc-source/config"
	"rdc-source/internal/handler"
	"rdc-source/internal/middleware"
	"rdc-source/internal/migration"
	"rdc-source/internal/repository"
	"rdc-source/internal/service"
	"rdc-source/pkg/azmk"
	"rdc-source/pkg/lw"
	"rdc-source/pkg/stub"
	"rdc-source/pkg/videorecord"

	_ "github.com/microsoft/go-mssqldb"
)

func main() {
	// Load configuration (will fatal on missing required env vars)
	cfg := config.Load()

	// Initialize structured logger (log/slog) — JSON format.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	slog.Info("starting RDC server",
		"db_host", cfg.DBHost,
		"db_name", cfg.DBName,
		"server_addr", cfg.ServerAddr,
		"migrations_drop_recreate", cfg.MigrationsDropRecreate,
		"lw_use_mock", cfg.UseMockLW,
		"lw_base_url", cfg.LWBaseURL,
	)

	slog.Info("PR #282: application configuration",
		"step", "0.config",
		"log_level", cfg.LogLevel,
		"server_addr", cfg.ServerAddr,
		"db_host", cfg.DBHost,
		"db_name", cfg.DBName,
		"lw_use_mock", cfg.UseMockLW,
		"lw_use_stub", cfg.UseStubLW,
		"lw_base_url", cfg.LWBaseURL,
		"lw_timeout_s", cfg.LWTimeoutS,
		"azmk_use_mock", cfg.AzmkUseMock,
		"azmk_base_url", cfg.AzmkBaseURL,
		"azmk_timeout_s", cfg.AzmkTimeoutS,
		"azmk_kyc_verify_enabled", cfg.AzmkKycVerifyEnabled,
		"azmk_customer_data_use_mock", cfg.AzmkCustomerDataUseMock,
		"azmk_customer_data_url", cfg.AzmkCustomerDataURL,
		"azmk_branch_code", cfg.AzmkBranchCode,
		"azmk_product_id", cfg.AzmkProductID,
		"cutoff_checks_enabled", cfg.CutoffChecksEnabled,
		"cutoff_stop_on_first_fail", cfg.CutoffStopOnFirstFail,
		"employment_tenure_min_months", cfg.EmploymentTenureMinMonths,
		"video_record_enabled", cfg.VideoRecordEnabled,
		"video_record_use_mock", cfg.VideoRecordUseMock,
		"video_record_base_url", cfg.VideoRecordBaseURL,
		"rate_limit_per_minute", cfg.RateLimitPerMinute,
		"otp_rate_limit_per_min", cfg.OTPRateLimitPerMin,
		"discount_rate_per_min", cfg.DiscountRatePerMin,
		"allowed_origin", cfg.AllowedOrigin,
		"auth_session_ttl_hours", cfg.AuthSessionTTLHours,
	)

	// Connect to SQL Server
	db, err := sql.Open("mssql", cfg.DSN())
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	// PR #259: DB connection pool — concurrency üçün kritik.
	// Default (unlimited) production-da connection exhaustion-a səbəb olur.
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	slog.Info("connected to SQL Server", "max_open_conns", 50, "max_idle_conns", 10, "conn_max_lifetime", "5m")

	// Run database migrations.
	// PR #293: migrations binary-nin içində embed olunub (migrations_embed.go) —
	// diskdə migrations/ qovluğu tələb olunmur, executable tək başına kifayətdir.
	migrationsFS, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		slog.Error("failed to create sub filesystem for migrations", "error", err)
		os.Exit(1)
	}
	if err := migration.Run(db, migrationsFS, migration.Options{
		DropRecreate: cfg.MigrationsDropRecreate,
	}); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	// --- Repository layer ---
	appRepo := repository.NewApplicationRepo(db)
	customerRepo := repository.NewCustomerRepo(db)
	lwEventRepo := repository.NewLWLoanEventRepo(db)
	discountCodeRepo := repository.NewDiscountCodeRepo(db)     // PR #94/95: discount codes
	systemSettingsRepo := repository.NewSystemSettingsRepo(db) // PR #98: feature flags
	userRepo := repository.NewUserRepo(db)                     // PR #142: auth users
	cutoffResultRepo := repository.NewCutoffResultRepo(db)     // PR #168: cutoff results
	videoRecordRepo := repository.NewVideoRecordRepo(db)       // PR #188: video records
	serviceCacheRepo := repository.NewServiceCacheRepo(db)     // PR #205: service cache

	// --- LW Provider (T-2.13) ---
	// In mock mode: reads from local DB (mock_lms_loans table) + canned responses.
	// In real mode: makes HTTP calls to LWBaseURL with LWApiKey.
	lwProvider := newLWProvider(cfg, db)

	// --- OTP Provider + Service (T-3.1 to T-3.7) ---
	otpProvider := newOTPProvider(cfg, db)
	otpRepo := repository.NewOTPRepo(db)
	otpService := service.NewOTPService(otpProvider, otpRepo)

	// --- Service layer ---
	creditEngine := service.NewCreditEngine(lwProvider, appRepo)
	appService := service.NewApplicationService(appRepo, creditEngine, customerRepo, otpService)

	// PR #95: inject DiscountCodeService so customer-confirm can validate codes
	// and approval can apply discounts + send SMS with new codes.
	discountCodeService := service.NewDiscountCodeService(discountCodeRepo)
	appService.SetDiscountService(discountCodeService)

	// PR #98: feature flag service — lets admin toggle the discount code
	// feature on/off with one PUT command. Wired into DiscountCodeService
	// so validation/generation respect the flag.
	featureFlagService := service.NewFeatureFlagService(systemSettingsRepo)
	discountCodeService.SetFeatureFlagService(featureFlagService)

	// PR #117: AZMK Online Lending Service provider
	// Mock mode: AZMK_USE_MOCK=true (default) → MockProvider (test üçün)
	// Real mode: AZMK_USE_MOCK=false → HTTPProvider (real AZMK servisinə)
	var azmkProvider azmk.Provider
	if cfg.AzmkUseMock {
		slog.Info("using mock AZMK provider (dev/test mode)")
		azmkProvider = azmk.NewMockProvider()
	} else {
		slog.Info("using HTTP AZMK provider", "base_url", cfg.AzmkBaseURL, "timeout_s", cfg.AzmkTimeoutS, "auth", cfg.AzmkUsername != "")
		azmkProvider = azmk.NewHTTPProvider(cfg.AzmkBaseURL, cfg.AzmkUsername, cfg.AzmkPassword, cfg.AzmkTimeoutS)
	}
	appService.SetAzmkProvider(azmkProvider, cfg.AzmkBranchCode, cfg.AzmkCardExpiring, cfg.AzmkProductID, cfg.AzmkDisbursementFee)

	// PR #312: AZMK imza gözləmə worker-ı — pending_signature müraciətlərini
	// poll edir, imzalananda avtomatik disburse edir, vaxt bitəndə reject edir.
	appService.StartAzmkSignWorker(cfg.AzmkSignPollIntervalS, cfg.AzmkSignTimeoutS)

	// PR #163: Audit log — AZMK provider-lara DB əlaqəsi ver
	if httpProvider, ok := azmkProvider.(*azmk.HTTPProvider); ok {
		httpProvider.SetAuditDB(db, nil)
	}

	// PR #152: AZMK CustomerDataService (yaş yoxlaması üçün)
	// Mock mode: AZMK_CUSTOMER_DATA_USE_MOCK=true (default) → MockCustomerDataProvider
	//   FIN koduna görə fərqli şəxslər imitasiya olunur (finScenarios map)
	// Real mode: AZMK_CUSTOMER_DATA_USE_MOCK=false → HTTPCustomerDataProvider
	var customerDataProvider azmk.CustomerDataProvider
	if cfg.AzmkCustomerDataUseMock {
		slog.Info("using mock AZMK CustomerDataService (dev/test mode — FIN-based scenarios)")
		customerDataProvider = azmk.NewMockCustomerDataProvider()
	} else {
		slog.Info("using HTTP AZMK CustomerDataService", "url", cfg.AzmkCustomerDataURL)
		customerDataProvider = azmk.NewHTTPCustomerDataProvider(cfg.AzmkCustomerDataURL, cfg.AzmkUsername, cfg.AzmkPassword, cfg.AzmkTimeoutS)
	}
	appService.SetCustomerDataProvider(customerDataProvider)
	creditEngine.SetCustomerDataProvider(customerDataProvider)         // PR #265
	appService.SetCutoffRepo(cutoffResultRepo)                         // PR #168
	appService.SetKycVerifyEnabled(cfg.AzmkKycVerifyEnabled)           // PR #170
	appService.SetCutoffStopOnFirstFail(cfg.CutoffStopOnFirstFail)     // PR #171
	appService.SetCutoffChecksEnabled(cfg.CutoffChecksEnabled)         // PR #278
	appService.SetReferralDiscountPercent(cfg.ReferralDiscountPercent) // PR #284

	// PR #163: Audit log — CustomerData provider-a DB əlaqəsi ver
	if httpCDP, ok := customerDataProvider.(*azmk.HTTPCustomerDataProvider); ok {
		httpCDP.SetAuditDB(db, nil)
	}

	// PR #188: Video record service (Kvadrat Lab)
	var videoRecordProvider videorecord.Provider
	if cfg.VideoRecordUseMock {
		slog.Info("using mock video record service (dev/test mode)")
		videoRecordProvider = videorecord.NewMockProvider()
	} else {
		slog.Info("using HTTP video record service", "url", cfg.VideoRecordBaseURL)
		videoRecordProvider = videorecord.NewHTTPProvider(cfg.VideoRecordBaseURL, cfg.VideoRecordUsername, cfg.VideoRecordPassword, cfg.VideoRecordTimeoutS)
	}
	appService.SetVideoRecordProvider(videoRecordProvider)
	appService.SetVideoRecordRepo(videoRecordRepo)
	appService.SetServiceCacheRepo(serviceCacheRepo) // PR #205: service cache
	appService.SetVideoRecordEnabled(cfg.VideoRecordEnabled, cfg.VideoRecordWebhookURL, cfg.VideoRecordRedirectURL)
	slog.Info("video record service", "enabled", cfg.VideoRecordEnabled, "mock", cfg.VideoRecordUseMock)
	// Audit log for HTTP provider
	if httpVRP, ok := videoRecordProvider.(*videorecord.HTTPProvider); ok {
		httpVRP.SetAuditDB(db, nil)
	}

	// --- SIMA Provider + Service (T-4.1 to T-4.5) ---
	// PR #120: SIMA KYC artıq customer-confirm-da çağrılmır (AZMK KYC əvəzlədi).
	// SIMA service hələ də lwCallbackHandler üçün saxlanılır.
	simaProvider := newSimaProvider(cfg)
	simaRepo := repository.NewSimaRepo(db)
	simaService := service.NewSimaService(simaProvider, simaRepo)

	// --- MyGov Provider + Service (T-4.8 to T-4.10) ---
	mygovProvider := newMyGovProvider(cfg)
	mygovRepo := repository.NewMyGovRepo(db)
	mygovService := service.NewMyGovService(mygovProvider, mygovRepo, appRepo, otpProvider, cfg.MyGovClientID, cfg.MyGovRedirectURI, cfg.MyGovWebURL)
	mygovService.SetCustomerDataProvider(customerDataProvider)               // PR #239: AZMK GetEmployeeInfoByPin
	mygovService.SetCutoffRepo(cutoffResultRepo)                             // PR #242: EMPLOYMENT_TENURE / DISABILITY_GROUP1 cutoff_results-a yazılır
	mygovService.SetEmploymentTenureMinMonths(cfg.EmploymentTenureMinMonths) // PR #279: parametrik staj həddi

	// --- Handler layer ---
	lwMockHandler := handler.NewLWMockHandler(lwProvider)
	appHandler := handler.NewApplicationHandler(appService)
	lwRouterHandler := handler.NewLWRouterHandler(lwProvider)
	lwCallbackHandler := handler.NewLWCallbackHandler(simaService)
	otpHandler := handler.NewOTPHandler(otpService)
	mygovHandler := handler.NewMyGovHandler(mygovService, appService) // PR #148: appSvc for audit
	expertHandler := handler.NewExpertHandler(appService)
	lwLoanStatusHandler := handler.NewLWLoanStatusHandler(lwEventRepo)
	discountCodeHandler := handler.NewDiscountCodeHandler(discountCodeService) // PR #96
	featureFlagHandler := handler.NewFeatureFlagHandler(featureFlagService)    // PR #98

	// PR #142: Auth service + handlers
	authCfg := service.DefaultAuthConfig()
	if cfg.AuthSessionTTLHours > 0 {
		authCfg.SessionTTL = time.Duration(cfg.AuthSessionTTLHours) * time.Hour
	}
	authService := service.NewAuthService(userRepo, authCfg)
	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(authService)

	// PR #142: Ensure default admin user exists on first startup
	if err := authService.EnsureAdminUser(context.Background(), cfg.AdminInitialPassword); err != nil {
		slog.Error("failed to ensure admin user", "error", err)
		os.Exit(1)
	}

	// PR #149: Create rate limiters
	apiLimiter := middleware.NewRateLimiter(cfg.RateLimitPerMinute)
	otpLimiter := middleware.NewRateLimiter(cfg.OTPRateLimitPerMin)
	discountLimiter := middleware.NewRateLimiter(cfg.DiscountRatePerMin)
	slog.Info("rate limiters configured",
		"api_per_min", cfg.RateLimitPerMinute,
		"otp_per_min", cfg.OTPRateLimitPerMin,
		"discount_per_min", cfg.DiscountRatePerMin,
		"allowed_origin", cfg.AllowedOrigin,
	)

	// --- Route registration + middleware chain ---
	router := handler.NewRouter(appHandler, lwMockHandler, lwRouterHandler, lwCallbackHandler, otpHandler, mygovHandler, expertHandler, lwLoanStatusHandler, discountCodeHandler, featureFlagHandler, authHandler, userHandler, authService, cfg.AllowedOrigin, apiLimiter, otpLimiter, discountLimiter)

	// UI: serve embedded static files from web/ directory.
	// fs.Sub strips the "web/" prefix so /detail.html maps to web/detail.html.
	webFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		slog.Error("failed to create sub filesystem for web", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(webFS))
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			router.ServeHTTP(w, r)
			return
		}
		// PR #143: Clean URL rewrite — /login → /login.html, /dashboard → /index.html, etc.
		// PR #146: Fix — serve file directly instead of rewriting path + FileServer.
		//   http.FileServer automatically redirects /index.html → / (Go stdlib behavior),
		//   which caused /dashboard to redirect to / → /landing.html.
		//   Now we read the file from embed.FS and write it directly, bypassing FileServer.
		cleanURLMap := map[string]string{
			"/":          "/landing.html",
			"/login":     "/login.html",
			"/dashboard": "/index.html",
			"/admin":     "/admin.html",
			"/detail":    "/detail.html",
			"/apply":     "/apply.html",
			"/landing":   "/landing.html",
		}
		if target, ok := cleanURLMap[r.URL.Path]; ok {
			filePath := strings.TrimPrefix(target, "/")
			data, err := fs.ReadFile(webFS, filePath)
			if err != nil {
				slog.Error("clean URL file not found", "path", r.URL.Path, "target", target, "error", err)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// PR #292: middleware zənciri router-in içinə deyil, ROOT handler-ə tətbiq olunur.
	// Əvvəl zəncir yalnız /api/ router-in içində idi — HTML səhifə keçidləri
	// (/dashboard, /apply, /detail...) heç bir middleware-dən keçmirdi və nə
	// terminalda, nə də Loki-də görünmürdü.
	// Zəncir sırası: CORS → RequestID → Logger → Recovery → handler.
	// Logger Recovery-dən KƏNAR-dadır ki, panic baş verdikdə Recovery-nin qaytardığı
	// 500 statusu da "request_completed" kimi loglansın (əvvəl panic-lər
	// request_completed olmadan itirdi).
	rootHandler := middleware.Logger(slog.Default())(
		middleware.Recovery(slog.Default())(httpHandler))
	rootHandler = middleware.RequestID(rootHandler)
	rootHandler = middleware.CORS(cfg.AllowedOrigin)(rootHandler)

	// --- Start the HTTP server with graceful shutdown ---
	// PR #259: HTTP server timeout-ları — concurrency və security üçün.
	// ReadTimeout/WriteTimeout 200s — KYC verify polling 180s overhead üçün.
	// ReadHeaderTimeout 10s — slowloris attack qorunması.
	srv := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       200 * time.Second,
		WriteTimeout:      200 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal (SIGINT / SIGTERM)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
}

// newLWProvider creates the LW provider based on configuration (T-2.13).
// When UseMockLW is true (default for dev), returns a MockProvider backed by
// the local DB. When false, returns an HTTPProvider that calls the real LW
// system at LWBaseURL with LWApiKey.
func newLWProvider(cfg *config.Config, db *sql.DB) lw.Provider {
	if cfg.UseMockLW {
		slog.Info("using mock LW provider (dev/test mode)")
		return lw.NewMockProvider(db)
	}

	// PR #61: stub server mode — start in-process stub and point HTTPProvider at it.
	// This mode lets you exercise the full HTTP provider code path (timeouts,
	// error handling, ?scenario= param) without the real LW router being available.
	if cfg.UseStubLW {
		slog.Info("starting in-process LW stub server (development only)", "port", cfg.StubLWPort)
		go stub.StartInBackground(cfg.StubLWPort)
		stubURL := fmt.Sprintf("http://localhost:%d", cfg.StubLWPort)
		slog.Info("using HTTP LW provider pointed at in-process stub", "base_url", stubURL, "timeout_s", cfg.LWTimeoutS)
		// Give the stub a moment to bind to the port.
		time.Sleep(100 * time.Millisecond)
		return lw.NewHTTPProvider(
			stubURL,
			"stub-mode-no-auth-needed",
			time.Duration(cfg.LWTimeoutS)*time.Second,
		)
	}

	slog.Info("using HTTP LW provider", "base_url", cfg.LWBaseURL, "timeout_s", cfg.LWTimeoutS)
	return lw.NewHTTPProvider(
		cfg.LWBaseURL,
		cfg.LWApiKey,
		time.Duration(cfg.LWTimeoutS)*time.Second,
	)
}
