package otp

import (
        "context"
        "fmt"
        "log/slog"
        "sync"
        "time"
)

// SMSProviderReader is the interface for reading active provider config from DB.
// *repository.SMSProviderRepo satisfies this structurally.
type SMSProviderReader interface {
        GetActiveProvider(ctx context.Context) (*SMSProviderConfig, error)
}

// SMSProviderConfig represents an SMS gateway configuration (mirrors model.SMSProviderConfig).
// PR #196: parametrik sahələr əlavə edildi — hər hansı SMS provayder dəstəklənir.
type SMSProviderConfig struct {
        ID             int
        ProviderCode   string
        BaseURL        string
        AppKey         string
        Username       string
        Password       string
        SenderID       string
        TimeoutSeconds int
        // PR #196: parametrik sahələr
        HTTPMethod     string // GET və ya POST (default: GET)
        ParamUser      string // URL param adı (default: user)
        ParamPassword  string // URL param adı (default: password)
        ParamPhone     string // URL param adı (default: gsm)
        ParamSender    string // URL param adı (default: from)
        ParamText      string // URL param adı (default: text)
        SuccessField   string // response-də success sahə (default: errno)
        SuccessValue   string // success dəyəri (default: 100)
        ErrorField     string // response-də error mətni sahəsi (default: errtext)
}

// DynamicSMSProvider implements the OTP Provider interface by reading the
// active provider config from the database. It caches the provider for a
// configurable TTL to avoid hitting the DB on every SMS.
//
// Thread-safe: uses RWMutex with double-check locking pattern.
type DynamicSMSProvider struct {
        repo     SMSProviderReader
        cacheTTL time.Duration

        mu          sync.RWMutex
        cached      Provider
        cacheExpiry time.Time
}

// NewDynamicSMSProvider creates a new DynamicSMSProvider.
// cacheTTL controls how often the DB is queried for provider changes.
func NewDynamicSMSProvider(repo SMSProviderReader, cacheTTL time.Duration) *DynamicSMSProvider {
        return &DynamicSMSProvider{
                repo:     repo,
                cacheTTL: cacheTTL,
        }
}

// Send delivers the given message via the active SMS provider.
func (d *DynamicSMSProvider) Send(ctx context.Context, phone, message string) error {
        provider, err := d.getActiveProvider(ctx)
        if err != nil {
                return fmt.Errorf("failed to get active SMS provider: %w", err)
        }
        return provider.Send(ctx, phone, message)
}

// Name returns "dynamic".
func (d *DynamicSMSProvider) Name() string { return "dynamic" }

// getActiveProvider returns the cached provider if valid, otherwise
// reads from DB and creates a new HTTPProvider.
func (d *DynamicSMSProvider) getActiveProvider(ctx context.Context) (Provider, error) {
        // Fast path: read lock
        d.mu.RLock()
        if d.cached != nil && time.Now().Before(d.cacheExpiry) {
                p := d.cached
                d.mu.RUnlock()
                return p, nil
        }
        d.mu.RUnlock()

        // Slow path: write lock
        d.mu.Lock()
        defer d.mu.Unlock()

        // Double-check (another goroutine may have refreshed while we waited)
        if d.cached != nil && time.Now().Before(d.cacheExpiry) {
                return d.cached, nil
        }

        // Read from DB
        cfg, err := d.repo.GetActiveProvider(ctx)
        if err != nil {
                return nil, fmt.Errorf("failed to read SMS provider from DB: %w", err)
        }
        if cfg == nil {
                return nil, fmt.Errorf("no active SMS provider found in DB")
        }

        // Create HTTPProvider with DB config — PR #196: parametrik sahələr ötürülür
        provider := NewHTTPProvider(
                cfg.BaseURL,
                cfg.Password, // API key / password
                cfg.Username, // user
                cfg.SenderID, // sender
                time.Duration(cfg.TimeoutSeconds)*time.Second,
                // PR #196: parametrik sahələr
                cfg.HTTPMethod,
                cfg.ParamUser,
                cfg.ParamPassword,
                cfg.ParamPhone,
                cfg.ParamSender,
                cfg.ParamText,
                cfg.SuccessField,
                cfg.SuccessValue,
                cfg.ErrorField,
        )

        // Cache it
        d.cached = provider
        d.cacheExpiry = time.Now().Add(d.cacheTTL)

        slog.Info("SMS provider loaded from DB",
                "provider", cfg.ProviderCode,
                "base_url", cfg.BaseURL,
                "sender", cfg.SenderID,
                "cache_ttl", d.cacheTTL.String())

        return provider, nil
}

// ForceRefresh clears the cache so the next Send call reads from DB.
// Useful when an admin switches providers via DB update.
func (d *DynamicSMSProvider) ForceRefresh() {
        d.mu.Lock()
        defer d.mu.Unlock()
        d.cached = nil
        d.cacheExpiry = time.Time{}
}
