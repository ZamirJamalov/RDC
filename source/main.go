package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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

	// Initialize structured logger (log/slog) — JSON format is friendlier for
	// log aggregation tools (ELK, Loki, CloudWatch, etc.).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	slog.Info("starting RDC server",
		"db_host", cfg.DBHost,
		"db_name", cfg.DBName,
		"server_addr", cfg.ServerAddr,
		"migrations_drop_recreate", cfg.MigrationsDropRecreate,
	)

	// Connect to SQL Server
	db, err := sql.Open("mssql", cfg.DSN())
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Verify the connection
	if err = db.Ping(); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to SQL Server")

	// Run database migrations on startup. In production
	// (MIGRATIONS_DROP_RECREATE=false) this is idempotent — uses IF NOT EXISTS
	// guards and never drops data.
	if err := migration.Run(db, "migrations", migration.Options{
		DropRecreate: cfg.MigrationsDropRecreate,
	}); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	// --- Repository layer ---
	appRepo := repository.NewApplicationRepo(db)

	// --- LW Provider (unified: loan data + blacklist + router + approve) ---
	lwProvider := lw.NewMockProvider(db)

	// --- Service layer ---
	creditEngine := service.NewCreditEngine(lwProvider, appRepo)
	appService := service.NewApplicationService(appRepo, creditEngine)

	// --- Handler layer ---
	lwMockHandler := handler.NewLWMockHandler(lwProvider)
	appHandler := handler.NewApplicationHandler(appService)

	// --- Route registration + middleware chain ---
	router := handler.NewRouter(appHandler, lwMockHandler)

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
