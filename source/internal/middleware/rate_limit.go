package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimitEntry tracks request counts per key (IP or IP+path).
type rateLimitEntry struct {
	count     int
	resetTime time.Time
}

// RateLimiter is an in-memory IP-based rate limiter.
// PR #149: protects public API endpoints from brute-force and abuse.
type RateLimiter struct {
	mu         sync.Mutex
	entries    map[string]*rateLimitEntry
	maxPerMin  int
}

// NewRateLimiter creates a new RateLimiter with the given max requests per minute.
func NewRateLimiter(maxPerMin int) *RateLimiter {
	rl := &RateLimiter{
		entries:   make(map[string]*rateLimitEntry),
		maxPerMin: maxPerMin,
	}
	// Background cleanup every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

// cleanup removes expired entries to prevent memory growth.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for key, entry := range rl.entries {
		if now.After(entry.resetTime) {
			delete(rl.entries, key)
		}
	}
}

// Allow checks if the given key is within the rate limit.
// Returns true if allowed, false if rate limit exceeded.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	entry, exists := rl.entries[key]
	if !exists || now.After(entry.resetTime) {
		rl.entries[key] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}
	entry.count++
	return entry.count <= rl.maxPerMin
}

// RateLimit returns middleware that rate-limits requests per IP.
// PR #149: usage — RateLimit(limiter)(handler)
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !limiter.Allow(ip) {
				w.Header().Set("Retry-After", "60")
				writeAuthError(w, http.StatusTooManyRequests, "çox çox sorğu — 1 dəqiqə gözləyin")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitWithKey returns middleware that rate-limits requests per IP + custom key suffix.
// PR #149: useful for OTP (rate limit per phone number, not just IP).
func RateLimitWithKey(limiter *RateLimiter, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			suffix := keyFunc(r)
			key := ip
			if suffix != "" {
				key = ip + ":" + suffix
			}
			if !limiter.Allow(key) {
				w.Header().Set("Retry-After", "60")
				writeAuthError(w, http.StatusTooManyRequests, "çox çox sorğu — 1 dəqiqə gözləyin")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractIP gets the client IP from the request.
// If behind a proxy, uses X-Forwarded-For (first IP), otherwise RemoteAddr.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Strip port from RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx >= 0 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}
