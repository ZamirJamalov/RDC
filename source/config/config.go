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

        // LW Provider configuration (T-2.12)
        // When UseMockLW is true, the LW provider reads from the local DB (mock_lms_loans
        // table) and returns canned responses for router endpoints. When false, the
        // HTTPProvider makes real HTTP calls to LWBaseURL with LWApiKey.
        //
        // PR #61: When UseStubLW is true, an in-process stub HTTP server is started
        // (pkg/stub) that mimics the real LW router responses. The HTTPProvider
        // points to it (LWBaseURL is overridden to http://localhost:{StubLWPort}).
        // Use this when the real LW router is not yet available but you want to
        // exercise the full HTTP provider code path (timeouts, error handling,
        // scenario-based responses via ?scenario= query param).
        //
        // Mode matrix:
        //   UseMockLW=true  UseStubLW=false → MockProvider (local DB, no HTTP)
        //   UseMockLW=false UseStubLW=true  → HTTPProvider + in-process stub server
        //   UseMockLW=false UseStubLW=false → HTTPProvider + real LW router
        LWBaseURL   string
        LWApiKey    string
        UseMockLW   bool
        LWTimeoutS  int // HTTP timeout for LW calls, in seconds
        UseStubLW   bool
        StubLWPort  int // port for the in-process stub server (default 8090)

        // OTP Provider configuration (T-3.1 to T-3.3)
        // When OTPUseMock is true, the OTP provider logs codes instead of sending SMS.
        // When false, the HTTPProvider calls a real SMS gateway at OTPBaseURL.
        OTPBaseURL  string
        OTPApiKey   string
        OTPSender   string // sender ID shown on the customer's phone
        OTPUseMock  bool
        OTPTimeoutS int // HTTP timeout for OTP calls, in seconds

        // SIMA Provider configuration (T-4.1 to T-4.2)
        SimaBaseURL  string
        SimaApiKey   string
        SimaUseMock  bool
        SimaTimeoutS int

        // MyGov Provider configuration (T-4.8)
        MyGovBaseURL  string
        MyGovApiKey   string
        MyGovUseMock  bool
        MyGovTimeoutS int

        // MyGov Deeplink configuration
        MyGovClientID    string // UUID provided by IDDA
        MyGovRedirectURI string // Partner redirect URI after consent approval
        MyGovWebURL      string // Web URL for SMS (netlify app that triggers mygov:// deeplink)

        // Phase 5: income + contacts validation (T-5.2)
        MinOfficialIncomeAZN float64 // minimum official income required for approval
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
                LWBaseURL:              getEnv("LW_BASE_URL", "http://localhost:8080"),
                LWApiKey:               getEnv("LW_API_KEY", ""),
                UseMockLW:              getEnvBool("LW_USE_MOCK", true),
                LWTimeoutS:             getEnvInt("LW_TIMEOUT_S", 30),
                UseStubLW:              getEnvBool("LW_USE_STUB", false),
                StubLWPort:             getEnvInt("LW_STUB_PORT", 8090),
                OTPBaseURL:             getEnv("OTP_BASE_URL", "http://localhost:8081"),
                OTPApiKey:              getEnv("OTP_API_KEY", ""),
                OTPSender:              getEnv("OTP_SENDER", "RDC"),
                OTPUseMock:             getEnvBool("OTP_USE_MOCK", true),
                OTPTimeoutS:            getEnvInt("OTP_TIMEOUT_S", 10),
                SimaBaseURL:            getEnv("SIMA_BASE_URL", "http://localhost:8082"),
                SimaApiKey:             getEnv("SIMA_API_KEY", ""),
                SimaUseMock:            getEnvBool("SIMA_USE_MOCK", true),
                SimaTimeoutS:           getEnvInt("SIMA_TIMEOUT_S", 15),
                MyGovBaseURL:           getEnv("MYGOV_BASE_URL", "http://localhost:8083"),
                MyGovApiKey:            getEnv("MYGOV_API_KEY", ""),
                MyGovUseMock:           getEnvBool("MYGOV_USE_MOCK", true),
                MyGovTimeoutS:          getEnvInt("MYGOV_TIMEOUT_S", 15),
                MyGovClientID:          getEnv("MYGOV_CLIENT_ID", ""),
                MyGovRedirectURI:       getEnv("MYGOV_REDIRECT_URI", "https://webhook.site/9f74dfae-92bc-458e-a3e3-b5134a9bf8bb"),
                MyGovWebURL:            getEnv("MYGOV_WEB_URL", "https://lively-pie-17ab5c.netlify.app/"),
                MinOfficialIncomeAZN:   getEnvFloat("MIN_OFFICIAL_INCOME_AZN", 300.0),
        }

        if cfg.MigrationsDropRecreate {
                slog.Warn("MIGRATIONS_DROP_RECREATE is true — tables will be dropped and recreated on startup. " +
                        "Set MIGRATIONS_DROP_RECREATE=false in production!")
        }

        if !cfg.UseMockLW && cfg.LWApiKey == "" && !cfg.UseStubLW {
                slog.Warn("LW_USE_MOCK is false and LW_USE_STUB is false but LW_API_KEY is empty — real LW calls will fail authentication")
        }

        if cfg.UseMockLW && cfg.UseStubLW {
                slog.Warn("Both LW_USE_MOCK and LW_USE_STUB are true — LW_USE_MOCK wins (stub server will not be started)")
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

func getEnvInt(key string, fallback int) int {
        if value, ok := os.LookupEnv(key); ok {
                if n, err := parseInt(value); err == nil {
                        return n
                }
                slog.Warn("invalid integer env var, using fallback", "key", key, "value", value, "fallback", fallback)
        }
        return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
        if value, ok := os.LookupEnv(key); ok {
                if f, err := parseFloat(value); err == nil {
                        return f
                }
                slog.Warn("invalid float env var, using fallback", "key", key, "value", value, "fallback", fallback)
        }
        return fallback
}

func parseInt(s string) (int, error) {
        var n int
        _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
        return n, err
}

func parseFloat(s string) (float64, error) {
        var f float64
        _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
        return f, err
}
