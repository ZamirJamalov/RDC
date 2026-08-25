package migration

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
)

// Options controls how Run executes migrations.
type Options struct {
	// DropRecreate, when true, drops all tables before applying migrations.
	// Should ONLY be true in dev/test environments. In production this must be
	// false or you will lose all data on every restart.
	DropRecreate bool
}

// Run reads every .sql file from migrationsFS, splits each file into individual
// batches (separated by the GO keyword), and executes them in order on the given
// DB connection. A failure in any batch is fatal — the function returns an error
// pointing at the offending batch and content.
//
// PR #293: migrationsFS embed.FS-dən gəlir — SQL faylları binary-nin içindədir,
// runtime-da diskdən migrations/ qovluğu tələb olunmur. fs.ReadDir fayl adlarına
// görə sıralı qaytarır, ona görə 001_, 002_ ... prefiksləri sıranı təmin edir.
//
// SQL Server batches: statements separated by a line containing only "GO" are
// submitted as separate exec calls — required for DDL like CREATE TABLE that
// must be the only statement in a batch.
func Run(db *sql.DB, migrationsFS fs.FS, opts Options) error {
	if opts.DropRecreate {
		slog.Warn("running migrations in DropRecreate mode — all data will be lost",
			"fs", "embedded")
		if err := dropAllTables(db); err != nil {
			return fmt.Errorf("drop-all phase failed: %w", err)
		}
	}

	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := runFile(db, migrationsFS, entry.Name()); err != nil {
			return fmt.Errorf("migration %s failed: %w", entry.Name(), err)
		}
		slog.Info("migration applied", "file", entry.Name())
	}
	return nil
}

// dropAllTables drops all application tables in reverse dependency order.
// Used only in dev/test mode (opts.DropRecreate = true). In production the
// migrations are idempotent — they use IF NOT EXISTS guards and never drop.
func dropAllTables(db *sql.DB) error {
	dropStatements := []string{
		"DROP TABLE IF EXISTS application_checks",
		"DROP TABLE IF EXISTS credit_level_history",
		// PR #94: discount_codes depends on loan_applications + customers,
		// so it must be dropped before them.
		"DROP TABLE IF EXISTS discount_codes",
		"DROP TABLE IF EXISTS loan_applications",
		"DROP TABLE IF EXISTS rejection_reasons",
		"DROP TABLE IF EXISTS check_type_config",
		"DROP TABLE IF EXISTS credit_levels",
		"DROP TABLE IF EXISTS credit_level_rules",
		"DROP TABLE IF EXISTS mock_lms_loans",
		// PR #94: customers is also dropped in dev mode (was missing before).
		"DROP TABLE IF EXISTS customers",
		// PR #89: business_cutoffs (no FK deps, safe to drop last).
		"DROP TABLE IF EXISTS business_cutoffs",
		// PR #98: system_settings (no FK deps).
		"DROP TABLE IF EXISTS system_settings",
		// PR #116: azmk_online_lending sütunları loan_applications-a əlavə olunub,
		// ayrıca cədvəl yoxdur — dropAllTables-də nəsə lazım deyil.
		// PR #142: sessions must be dropped before users (FK dependency).
		"DROP TABLE IF EXISTS sessions",
		"DROP TABLE IF EXISTS users",
		// PR #163: service audit logs
		"DROP TABLE IF EXISTS service_audit_logs",
		// PR #168: cutoff results
		"DROP TABLE IF EXISTS cutoff_results",
		// PR #188: video records
		"DROP TABLE IF EXISTS video_records",
		// PR #205: service cache config
		"DROP TABLE IF EXISTS service_cache_config",
	}
	for _, stmt := range dropStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("statement failed: %v\n--- Statement ---\n%s", err, stmt)
		}
	}
	return nil
}

// runFile reads a single .sql file from migrationsFS and executes its batches
// in order. A "batch" is a group of statements separated by a line containing
// only "GO" (the SQL Server batch separator).
func runFile(db *sql.DB, migrationsFS fs.FS, name string) error {
	sqlBytes, err := fs.ReadFile(migrationsFS, name)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", name, err)
	}

	batches := splitSQLBatches(string(sqlBytes))

	for i, batch := range batches {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		if _, err := db.Exec(batch); err != nil {
			return fmt.Errorf("batch %d failed: %v\n--- Batch ---\n%s", i+1, err, batch)
		}
	}
	return nil
}

// splitSQLBatches splits SQL content into batches separated by lines containing
// only "GO" (case-insensitive). Within each batch, individual statements are
// separated by semicolons at the end of a line. Empty lines and "--" comment
// lines are skipped, but inline comments after code are preserved.
//
// Example:
//
//	CREATE TABLE foo (...);
//	GO
//	INSERT INTO foo (...) VALUES (...);
//	GO
//
// → yields 2 batches.
func splitSQLBatches(content string) []string {
	var batches []string
	var current strings.Builder

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Batch separator — flush current batch and start a new one
		if strings.EqualFold(trimmed, "GO") {
			if s := strings.TrimSpace(current.String()); s != "" {
				batches = append(batches, s)
			}
			current.Reset()
			continue
		}

		// Skip standalone comment lines and empty lines (preserve inline comments)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		current.WriteString(line)
		current.WriteString("\n")
	}

	// Flush the final batch (in case the file doesn't end with GO)
	if s := strings.TrimSpace(current.String()); s != "" {
		batches = append(batches, s)
	}

	return batches
}
