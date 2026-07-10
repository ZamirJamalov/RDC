package main

import (
        "log/slog"
        "time"

        "rdc-source/config"
        "rdc-source/pkg/otp"
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

// newOTPProvider creates the OTP provider based on configuration (T-3.1 to T-3.3).
// When OTPUseMock is true (default for dev), returns a MockProvider that logs
// the code. When false, returns an HTTPProvider that calls a real SMS gateway.
func newOTPProvider(cfg *config.Config) otp.Provider {
        if cfg.OTPUseMock {
                slog.Info("using mock OTP provider (dev/test mode — codes logged, not sent)")
                return otp.NewMockProvider()
        }
        slog.Info("using HTTP OTP provider", "base_url", cfg.OTPBaseURL, "sender", cfg.OTPSender)
        return otp.NewHTTPProvider(
                cfg.OTPBaseURL,
                cfg.OTPApiKey,
                cfg.OTPSender,
                time.Duration(cfg.OTPTimeoutS)*time.Second,
        )
}

