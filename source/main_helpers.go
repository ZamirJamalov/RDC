package main

import (
	"log/slog"
	"time"
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
