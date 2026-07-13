package main

import (
        "database/sql"
        "log/slog"
        "time"

        "rdc-source/config"
        "rdc-source/internal/repository"
        "rdc-source/pkg/mygov"
        "rdc-source/pkg/otp"
        "rdc-source/pkg/sima"
)

// shutdownTimeout is the maximum time we wait for in-flight requests to finish
// during graceful shutdown. After this, the server is force-closed.
const shutdownTimeout = 30 * time.Second

// parseLogLevel converts a string env value ("debug", "info", ...) to slog.Level.
// Defaults to LevelInfo for unknown values — never panics.
func parseLogLevel(s string) slog.Level {
        switch s {
        case "debug":
                return slog.LevelDebug
        case "info":
                return slog.LevelInfo
        case "warn", "warning":
                return slog.LevelWarn
        case "error":
                return slog.LevelError
        default:
                return slog.LevelInfo
        }
}

// newOTPProvider creates the OTP provider based on configuration.
// When OTPUseMock is true (default for dev), returns a MockProvider that logs
// the code. When false, returns a DynamicSMSProvider that reads the active
// SMS gateway config from the database (runtime switchable).
func newOTPProvider(cfg *config.Config, db *sql.DB) otp.Provider {
        if cfg.OTPUseMock {
                slog.Info("using mock OTP provider (dev/test mode — codes logged, not sent)")
                return otp.NewMockProvider()
        }
        slog.Info("using dynamic SMS provider (DB-driven, 1-minute cache)")
        smsRepo := repository.NewSMSProviderRepo(db)
        return otp.NewDynamicSMSProvider(smsRepo, 1*time.Minute)
}

// newSimaProvider creates the SIMA KYC provider based on configuration (T-4.1 to T-4.2).
func newSimaProvider(cfg *config.Config) sima.Provider {
        if cfg.SimaUseMock {
                slog.Info("using mock SIMA provider (dev/test mode)")
                return sima.NewMockProvider()
        }
        slog.Info("using HTTP SIMA provider", "base_url", cfg.SimaBaseURL)
        return sima.NewHTTPProvider(
                cfg.SimaBaseURL,
                cfg.SimaApiKey,
                time.Duration(cfg.SimaTimeoutS)*time.Second,
        )
}

// newMyGovProvider creates the MyGov provider based on configuration (T-4.8).
func newMyGovProvider(cfg *config.Config) mygov.Provider {
        if cfg.MyGovUseMock {
                slog.Info("using mock MyGov provider (dev/test mode)")
                return mygov.NewMockProvider()
        }
        slog.Info("using HTTP MyGov provider", "base_url", cfg.MyGovBaseURL)
        return mygov.NewHTTPProvider(
                cfg.MyGovBaseURL,
                cfg.MyGovApiKey,
                time.Duration(cfg.MyGovTimeoutS)*time.Second,
        )
}

