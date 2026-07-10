package main

import (
        "context"
        "database/sql"
        "fmt"
        "log"
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
        // Load configuration
        cfg := config.Load()

        // Connect to SQL Server
        db, err := sql.Open("mssql", cfg.DSN())
        if err != nil {
                log.Fatalf("Failed to open database: %v", err)
        }
        defer db.Close()

        // Verify the connection
        if err = db.Ping(); err != nil {
                log.Fatalf("Failed to ping database: %v", err)
        }
        log.Println("Connected to SQL Server successfully")

        // Run database migrations on startup
        if err := migration.Run(db, "migrations"); err != nil {
                log.Fatalf("Migration failed: %v", err)
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

        // --- Route registration ---
        router := handler.NewRouter(appHandler, lwMockHandler)

        // --- Start the server on port 8000 with graceful shutdown ---
        addr := ":8000"
        srv := &http.Server{Addr: addr, Handler: router}

        go func() {
                fmt.Printf("RDC server starting on %s\n", addr)
                if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                        log.Fatalf("Server failed: %v", err)
                }
        }()

        // Wait for interrupt signal (SIGINT / SIGTERM)
        stop := make(chan os.Signal, 1)
        signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
        <-stop

        log.Println("Shutting down server...")
        if err := srv.Shutdown(context.Background()); err != nil {
                log.Printf("Forced shutdown: %v", err)
        }
        log.Println("Server stopped")
}
