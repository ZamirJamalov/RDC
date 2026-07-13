package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/pkg/otp"
)

// SMSProviderRepo handles database operations for SMS provider configurations.
type SMSProviderRepo struct {
        db *sql.DB
}

// NewSMSProviderRepo creates a new SMSProviderRepo.
func NewSMSProviderRepo(db *sql.DB) *SMSProviderRepo {
        return &SMSProviderRepo{db: db}
}

// GetActiveProvider retrieves the currently active SMS provider configuration.
// Returns (nil, nil) if no provider is active.
func (r *SMSProviderRepo) GetActiveProvider(ctx context.Context) (*otp.SMSProviderConfig, error) {
        var cfg otp.SMSProviderConfig
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, provider_code, base_url, app_key, username, password, sender_id, timeout_seconds
                FROM sms_providers
                WHERE is_active = 1`).Scan(
                &cfg.ID,
                &cfg.ProviderCode,
                &cfg.BaseURL,
                &cfg.AppKey,
                &cfg.Username,
                &cfg.Password,
                &cfg.SenderID,
                &cfg.TimeoutSeconds,
        )
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, nil
                }
                return nil, fmt.Errorf("failed to query active SMS provider: %w", err)
        }
        return &cfg, nil
}
