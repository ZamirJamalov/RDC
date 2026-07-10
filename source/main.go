package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"rdc-source/config"
	"rdc-source/internal/handler"
	"rdc-source/internal/repository"
	"rdc-source/internal/service"
	"rdc-source/pkg/lw"
	"strings"

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
	runMigrations(db)

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

	// Start the server on port 8000
	addr := ":8000"
	fmt.Printf("RDC server starting on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// runMigrations reads and executes 001_init.sql on every startup.
// The SQL file starts with DROP TABLE IF EXISTS so it always creates a clean schema.
func runMigrations(db *sql.DB) {
	sqlBytes, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		log.Fatalf("Failed to read migrations/001_init.sql: %v", err)
	}

	statements := splitSQLStatements(string(sqlBytes))

	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		_, err = db.Exec(stmt)
		if err != nil {
			log.Fatalf("Migration failed at statement %d: %v\n--- Statement ---\n%s", i+1, err, stmt)
		}
	}

	log.Println("Migration 001_init applied successfully (8 tables + seed data)")
}

// splitSQLStatements splits SQL content into individual statements
// by splitting on semicolons at the end of a line.
func splitSQLStatements(content string) []string {
	var statements []string
	var current strings.Builder

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		current.WriteString(line)
		current.WriteString("\n")

		// If line ends with semicolon, this statement is complete
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, current.String())
			current.Reset()
		}
	}

	// In case the last statement doesn't end with semicolon
	remaining := strings.TrimSpace(current.String())
	if remaining != "" {
		statements = append(statements, remaining)
	}

	return statements
}
