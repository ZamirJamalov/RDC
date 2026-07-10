package service

import (
        "context"
        "log/slog"
        "math"
        "time"

        "rdc-source/internal/model"
)

// engineProcessor is the minimal interface processApplicationWithRetry needs.
// *CreditEngine satisfies it, and tests can use a wrapper (e.g. flakyEngine)
// to simulate transient failures.
type engineProcessor interface {
        ProcessApplication(ctx context.Context, appID int) error
}

// RetryConfig controls the async retry behavior of the credit engine.
// The engine is triggered from CreateApplication in a background goroutine;
// if it fails (e.g. transient DB connection issue, LW timeout), we retry
// up to MaxAttempts times with exponential backoff.
//
// Defaults are conservative (3 attempts, 500ms initial backoff, ~2s max)
// because the engine runs in the background — we don't want to saturate
// the DB with retries if there's a real outage.
type RetryConfig struct {
        MaxAttempts   int           // total attempts including the first
        InitialDelay  time.Duration // delay before the 2nd attempt
        MaxDelay      time.Duration // cap on backoff growth
        BackoffFactor float64       // multiplier between attempts (2.0 = double each time)
}

// DefaultRetryConfig returns sensible defaults: 3 attempts, 500ms start,
// 4s max, doubling backoff.
func DefaultRetryConfig() RetryConfig {
        return RetryConfig{
                MaxAttempts:   3,
                InitialDelay:  500 * time.Millisecond,
                MaxDelay:      4 * time.Second,
                BackoffFactor: 2.0,
        }
}

// processApplicationWithRetry calls engine.ProcessApplication up to
// cfg.MaxAttempts times. Between attempts it sleeps with exponential
// backoff. The function returns nil as soon as one attempt succeeds, or
// the last error if all attempts fail.
//
// Retryable scenarios (transient failures):
//   - DB connection drops mid-pipeline
//   - LW returns 5xx (server error)
//   - context deadline exceeded (rare in background mode)
//
// Non-retryable scenarios (the function still returns the error, but
// retrying wouldn't help):
//   - Application not found (data integrity)
//   - Rate lookup fails (configuration issue, not transient)
//   - Blacklist check fails (fail-open, doesn't block)
//
// We don't currently distinguish between retryable and non-retryable errors
// — the engine is idempotent (status updates + check saves are safe to
// repeat), so retrying is always safe. A future improvement could add a
// Retryable(error) bool predicate to short-circuit non-retryable cases.
func processApplicationWithRetry(ctx context.Context, engine engineProcessor, appID int, cfg RetryConfig) error {
        var lastErr error

        for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
                err := engine.ProcessApplication(ctx, appID)
                if err == nil {
                        if attempt > 1 {
                                slog.Info("credit engine succeeded after retry",
                                        "application_id", appID,
                                        "attempt", attempt)
                        }
                        return nil
                }

                lastErr = err
                slog.Warn("credit engine attempt failed",
                        "application_id", appID,
                        "attempt", attempt,
                        "max_attempts", cfg.MaxAttempts,
                        "error", err)

                // Don't sleep after the last attempt
                if attempt < cfg.MaxAttempts {
                        delay := backoff(cfg.InitialDelay, cfg.BackoffFactor, attempt, cfg.MaxDelay)
                        slog.Debug("retrying credit engine",
                                "application_id", appID,
                                "next_attempt", attempt+1,
                                "delay_ms", delay.Milliseconds())
                        select {
                        case <-ctx.Done():
                                return ctx.Err()
                        case <-time.After(delay):
                        }
                }
        }

        return lastErr
}

// backoff computes the delay before the next attempt. The formula is:
//
//      delay = min(initialDelay * backoffFactor^(attempt-1), maxDelay)
//
// For attempt=1, delay=initialDelay. For attempt=2, delay=initialDelay*factor.
// For attempt=3, delay=initialDelay*factor^2. Etc.
func backoff(initialDelay time.Duration, factor float64, attempt int, maxDelay time.Duration) time.Duration {
        multiplier := math.Pow(factor, float64(attempt-1))
        delay := time.Duration(float64(initialDelay) * multiplier)
        if delay > maxDelay {
                return maxDelay
        }
        return delay
}

// triggerAsyncProcessing starts the credit engine in a background goroutine
// with retry. Called from CreateApplication so the HTTP response returns
// immediately while the pipeline runs in the background.
//
// The goroutine uses context.Background() (not the request context) because
// the request completes before the pipeline finishes — we don't want to
// cancel the pipeline when the client disconnects.
func (s *ApplicationService) triggerAsyncProcessing(app *model.LoanApplication) {
        go func() {
                ctx := context.Background()
                cfg := DefaultRetryConfig()
                if err := processApplicationWithRetry(ctx, s.creditEngine, app.ID, cfg); err != nil {
                        slog.Error("credit engine processing failed after all retries",
                                "application_id", app.ID,
                                "customer_pin", app.CustomerPIN,
                                "attempts", cfg.MaxAttempts,
                                "error", err)
                        // Last-resort: mark the application as rejected so it doesn't
                        // stay in "checking" forever. The reason explains what happened.
                        rejectReason := "Credit engine failed after retries: " + err.Error()
                        if rejectErr := s.repo.UpdateApplicationDecision(ctx, app.ID,
                                model.StatusRejected, "", rejectReason, 0, 0); rejectErr != nil {
                                slog.Error("failed to mark application as rejected after engine failure",
                                        "application_id", app.ID,
                                        "original_error", err,
                                        "reject_error", rejectErr)
                        }
                }
        }()
}
