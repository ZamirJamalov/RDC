package main

import (
        "context"
        "database/sql"
        "log/slog"
        "net/http"
        "os"
        "os/signal"
        "syscall"
        "time"

        "rdc-source/config"
        "rdc-source/internal/handler"
        "rdc-source/internal/migration"
        "rdc-source/internal/repository"
        "rdc-source/internal/service"
        "rdc-source/pkg/lw"

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

        // --- LW Provider (T-2.13) ---
        // In mock mode: reads from local DB (mock_lms_loans table) + canned responses.
        // In real mode: makes HTTP calls to LWBaseURL with LWApiKey.
        lwProvider := newLWProvider(cfg, db)

        // --- Service layer ---
        creditEngine := service.NewCreditEngine(lwProvider, appRepo)
        appService := service.NewApplicationService(appRepo, creditEngine)

        // --- OTP Provider + Service (T-3.1 to T-3.7) ---
        otpProvider := newOTPProvider(cfg)
        otpRepo := repository.NewOTPRepo(db)
        otpService := service.NewOTPService(otpProvider, otpRepo)

        // --- Handler layer ---
        lwMockHandler := handler.NewLWMockHandler(lwProvider)
        appHandler := handler.NewApplicationHandler(appService)
        lwRouterHandler := handler.NewLWRouterHandler(lwProvider)
        lwCallbackHandler := handler.NewLWCallbackHandler()
        otpHandler := handler.NewOTPHandler(otpService)

        // --- Route registration + middleware chain ---
        router := handler.NewRouter(appHandler, lwMockHandler, lwRouterHandler, lwCallbackHandler, otpHandler)

        // --- Start the HTTP server with graceful shutdown ---
        srv := &http.Server{Addr: cfg.ServerAddr, Handler: router}

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
        slog.Info("using HTTP LW provider", "base_url", cfg.LWBaseURL, "timeout_s", cfg.LWTimeoutS)
        return lw.NewHTTPProvider(
                cfg.LWBaseURL,
                cfg.LWApiKey,
                time.Duration(cfg.LWTimeoutS)*time.Second,
        )
}
