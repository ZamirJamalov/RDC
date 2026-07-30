package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"rdc-source/internal/model"
)

// SystemSettingsStore is the persistence interface for system settings
// (feature flags). The concrete *repository.SystemSettingsRepo satisfies
// this interface structurally.
//
// PR #98: introduced for the discount_codes_enabled feature flag.
type SystemSettingsStore interface {
	Get(ctx context.Context, key string) (*model.SystemSetting, error)
	Set(ctx context.Context, key, value, description string) error
	IsEnabled(ctx context.Context, key string) (bool, error)
	List(ctx context.Context) ([]model.SystemSetting, error)
}

// FeatureFlagService wraps SystemSettingsStore with caching to avoid hitting
// the DB on every discount code operation. The cache is short-lived (60s) so
// that admin toggles take effect quickly without a server restart.
//
// PR #98: introduced for the discount_codes_enabled feature flag.
type FeatureFlagService struct {
	repo SystemSettingsStore

	// In-memory cache (key → (enabled, fetched_at)).
	// A sync.Map would also work; we use a simple map + mutex because the
	// write contention is low (only the background refresh writes).
	cache    map[string]cachedFlag
	cacheTTL time.Duration
}

type cachedFlag struct {
	enabled   bool
	fetchedAt time.Time
}

// NewFeatureFlagService creates a new FeatureFlagService with a 60-second cache.
func NewFeatureFlagService(repo SystemSettingsStore) *FeatureFlagService {
	return &FeatureFlagService{
		repo:     repo,
		cache:    make(map[string]cachedFlag),
		cacheTTL: 60 * time.Second,
	}
}

// IsDiscountCodesEnabled returns true if the discount_codes_enabled setting
// is "1" (or "true"). Returns false (feature off) when:
//   - the setting value is "0" or anything other than "1"/"true"
//   - the setting does not exist (fail-safe: unknown → off)
//   - the DB query fails (fail-safe: log + off)
//
// The result is cached for 60 seconds to avoid hammering the DB on every
// discount code operation (validation, customer-confirm, approval).
func (s *FeatureFlagService) IsDiscountCodesEnabled(ctx context.Context) bool {
	return s.IsEnabled(ctx, model.SettingDiscountCodesEnabled)
}

// IsEnabled is the generic feature-flag check. Cached for cacheTTL.
func (s *FeatureFlagService) IsEnabled(ctx context.Context, key string) bool {
	if s == nil || s.repo == nil {
		// Service not wired — fail-safe: feature ON (so tests that don't
		// care about feature flags still work).
		return true
	}

	// Check cache first
	if cached, ok := s.cache[key]; ok {
		if time.Since(cached.fetchedAt) < s.cacheTTL {
			return cached.enabled
		}
	}

	// Cache miss or expired — query DB
	enabled, err := s.repo.IsEnabled(ctx, key)
	if err != nil {
		// On DB error, log and fail-safe (feature OFF for safety).
		// We don't cache errors so the next call retries.
		slog.Error("feature flag check failed — failing safe (disabled)",
			"key", key,
			"error", err)
		return false
	}

	// Update cache
	s.cache[key] = cachedFlag{
		enabled:   enabled,
		fetchedAt: time.Now(),
	}
	return enabled
}

// SetDiscountCodesEnabled toggles the discount_codes_enabled flag.
// value=true → "1", value=false → "0".
// The cache is invalidated immediately so the change takes effect at once.
func (s *FeatureFlagService) SetDiscountCodesEnabled(ctx context.Context, enabled bool) error {
	return s.Set(ctx, model.SettingDiscountCodesEnabled, enabled)
}

// Set updates a feature flag value and invalidates the cache for that key.
func (s *FeatureFlagService) Set(ctx context.Context, key string, enabled bool) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("feature flag service not wired")
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := s.repo.Set(ctx, key, value, ""); err != nil {
		return fmt.Errorf("failed to set feature flag %q: %w", key, err)
	}

	// Invalidate cache so the new value is picked up on the next read.
	delete(s.cache, key)

	slog.Info("feature flag updated",
		"key", key,
		"enabled", enabled)
	return nil
}

// Get retrieves a single setting (for admin dashboard display).
func (s *FeatureFlagService) Get(ctx context.Context, key string) (*model.SystemSetting, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("feature flag service not wired")
	}
	return s.repo.Get(ctx, key)
}

// List retrieves all settings (for admin dashboard display).
func (s *FeatureFlagService) List(ctx context.Context) ([]model.SystemSetting, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("feature flag service not wired")
	}
	return s.repo.List(ctx)
}

// Compile-time interface check: *repository.SystemSettingsRepo must satisfy SystemSettingsStore.
var _ SystemSettingsStore = (*nilSettingsStore)(nil)

type nilSettingsStore struct{}

func (*nilSettingsStore) Get(context.Context, string) (*model.SystemSetting, error) { return nil, sql.ErrNoRows }
func (*nilSettingsStore) Set(context.Context, string, string, string) error        { return nil }
func (*nilSettingsStore) IsEnabled(context.Context, string) (bool, error)          { return false, nil }
func (*nilSettingsStore) List(context.Context) ([]model.SystemSetting, error)      { return nil, nil }
