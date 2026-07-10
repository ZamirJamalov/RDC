package migration

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
)

// Run reads every .sql file from migrationsDir, splits each file into individual
// statements, and executes them in order on the given DB connection. A failure in
// any statement is fatal — the function returns an error pointing at the offending
// statement number and content.
//
// The migrations directory is expected to sit next to the binary at startup. Files
// are processed in lexicographic order, so naming them with a numeric prefix
// (001_init.sql, 002_otp_codes.sql, ...) guarantees the correct order.
//
// NOTE: this implementation is intentionally simple — it splits on semicolons at
// the end of a line and skips empty lines / "--" comments. For full DDL with
// semicolons inside strings or procedures, switch to a proper migration tool
// (golang-migrate, goose) in a later phase.
func Run(db *sql.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir %q: %w", migrationsDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := migrationsDir + "/" + entry.Name()
		if err := runFile(db, path); err != nil {
			return fmt.Errorf("migration %s failed: %w", entry.Name(), err)
		}
		log.Printf("migration applied: %s", entry.Name())
	}
	return nil
}

// runFile reads a single .sql file and executes its statements in order.
func runFile(db *sql.DB, path string) error {
	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	statements := splitSQLStatements(string(sqlBytes))

	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("statement %d failed: %v\n--- Statement ---\n%s", i+1, err, stmt)
		}
	}
	return nil
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
