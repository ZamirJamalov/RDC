package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	// DB connection
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server
	ServerAddr string

	// Migrations — when true, the runner will DROP and recreate tables on startup.
	// Should ONLY be true in dev/test environments. In production this must be false
	// or you will lose all data on every restart.
	MigrationsDropRecreate bool

	// Log level: "debug", "info", "warn", "error"
	LogLevel string
}

// Load reads configuration from environment variables. Required fields (DB_HOST,
// DB_PASSWORD) will cause a fatal error if missing — there are NO hardcoded
// defaults for credentials, ever.
func Load() *Config {
	cfg := &Config{
		DBHost:                 requireEnv("DB_HOST"),
		DBPort:                 getEnv("DB_PORT", "1433"),
		DBUser:                 requireEnv("DB_USER"),
		DBPassword:             requireEnv("DB_PASSWORD"),
		DBName:                 getEnv("DB_NAME", "RDC"),
		ServerAddr:             getEnv("SERVER_ADDR", ":8000"),
		MigrationsDropRecreate: getEnvBool("MIGRATIONS_DROP_RECREATE", true),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
	}

	if cfg.MigrationsDropRecreate {
		slog.Warn("MIGRATIONS_DROP_RECREATE is true — tables will be dropped and recreated on startup. " +
			"Set MIGRATIONS_DROP_RECREATE=false in production!")
	}

	return cfg
}

// DSN returns the SQL Server connection string in go-mssqldb format.
// Uses explicit port format (avoids SQLEXPRESS browser lookup) and disables
// encryption (acceptable for local dev; in production use encrypt=true with a
// proper certificate).
func (c *Config) DSN() string {
	return fmt.Sprintf("server=%s;port=%s;user id=%s;password=%s;database=%s;encrypt=disable",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName)
}

// requireEnv reads an environment variable and fatals if it is empty. Use this
// for any setting that has no safe default (credentials, hostnames, etc.).
func requireEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		slog.Error("required environment variable is not set", "key", key)
		os.Exit(1)
	}
	return value
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}
