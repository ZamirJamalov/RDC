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
        "rdc-source/internal/migration"
        "rdc-source/internal/repository"
        "rdc-source/internal/service"
        "rdc-source/pkg/lw"
        "rdc-source/pkg/stub"

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
        slog.Info("connected to SQL Server")

        // Run database migrations
        if err := migration.Run(db, "migrations", migration.Options{
                DropRecreate: cfg.MigrationsDropRecreate,
        }); err != nil {
                slog.Error("migration failed", "error", err)
                os.Exit(1)
        }

        // --- Repository layer ---
        appRepo := repository.NewApplicationRepo(db)
        customerRepo := repository.NewCustomerRepo(db)
        lwEventRepo := repository.NewLWLoanEventRepo(db)

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


        // --- SIMA Provider + Service (T-4.1 to T-4.5) ---
        simaProvider := newSimaProvider(cfg)
        simaRepo := repository.NewSimaRepo(db)
        simaService := service.NewSimaService(simaProvider, simaRepo)

        // PR #69: inject SIMA service into ApplicationService so that
        // CustomerConfirmApplication can trigger KYC SMS automatically.
        appService.SetSimaService(simaService)

        // --- MyGov Provider + Service (T-4.8 to T-4.10) ---
        mygovProvider := newMyGovProvider(cfg)
        mygovRepo := repository.NewMyGovRepo(db)
        mygovService := service.NewMyGovService(mygovProvider, mygovRepo, appRepo, otpProvider, cfg.MyGovClientID, cfg.MyGovRedirectURI, cfg.MyGovWebURL)

        // --- Handler layer ---
        lwMockHandler := handler.NewLWMockHandler(lwProvider)
        appHandler := handler.NewApplicationHandler(appService)
        lwRouterHandler := handler.NewLWRouterHandler(lwProvider)
        lwCallbackHandler := handler.NewLWCallbackHandler(simaService)
        otpHandler := handler.NewOTPHandler(otpService)
        mygovHandler := handler.NewMyGovHandler(mygovService)
        expertHandler := handler.NewExpertHandler(appService)
        lwLoanStatusHandler := handler.NewLWLoanStatusHandler(lwEventRepo)

        // --- Route registration + middleware chain ---
        router := handler.NewRouter(appHandler, lwMockHandler, lwRouterHandler, lwCallbackHandler, otpHandler, mygovHandler, expertHandler, lwLoanStatusHandler)

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
                fileServer.ServeHTTP(w, r)
        })

        // --- Start the HTTP server with graceful shutdown ---
        srv := &http.Server{Addr: cfg.ServerAddr, Handler: httpHandler}

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
