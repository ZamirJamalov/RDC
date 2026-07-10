package config

import (
	"fmt"
	"os"
)

// Config holds the database connection configuration.
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
}

// Load reads configuration from environment variables or applies defaults.
func Load() *Config {
	cfg := &Config{
		DBHost:     getEnv("DB_HOST", "172.17.1.24"),
		DBPort:     getEnv("DB_PORT", "1433"),
		DBUser:     getEnv("DB_USER", "rdc_test"),
		DBPassword: getEnv("DB_PASSWORD", "BpmRdc2026"),
		DBName:     getEnv("DB_NAME", "RDC"),
	}
	return cfg
}

// DSN returns the SQL Server connection string in go-mssqldb format.
// It uses explicit port format and disables encryption for local development.
func (c *Config) DSN() string {
	// URL-encode the password to handle special characters like ! @ # etc.
	//passEncoded := url.QueryEscape(c.DBPassword)

	// 1. Host:Port formatından istifadə edirik (SQLEXPRESS axtarış xətasını tamamilə həll edir)
	// 2. Sona '&encrypt=disable' əlavə edirik (Lokal mühitdə SSL təhlükəsizlik bloklamasını keçir)
	// return fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s&encrypt=disable",
	// 	c.DBUser, passEncoded, c.DBHost, c.DBPort, c.DBName)

	return fmt.Sprintf("server=%s;port=%s;user id=%s;password=%s;database=%s;encrypt=disable",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName)

	// // database parametrini sildik
	// return fmt.Sprintf("server=%s;port=%s;user id=%s;password=%s;encrypt=disable",
	// 	c.DBHost, c.DBPort, c.DBUser, c.DBPassword)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
