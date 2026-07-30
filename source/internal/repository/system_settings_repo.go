package repository

import (
        "context"
        "database/sql"
        "fmt"

        "rdc-source/internal/model"
)

// SystemSettingsRepo handles database operations for system_settings.
//
// PR #98: used for feature flags (e.g. discount_codes_enabled) that can be
// toggled at runtime without redeploying the application.
type SystemSettingsRepo struct {
        db *sql.DB
}

// NewSystemSettingsRepo creates a new SystemSettingsRepo.
func NewSystemSettingsRepo(db *sql.DB) *SystemSettingsRepo {
        return &SystemSettingsRepo{db: db}
}

// Get retrieves a setting by key. Returns the setting, or (nil, sql.ErrNoRows)
// if the key does not exist.
func (r *SystemSettingsRepo) Get(ctx context.Context, key string) (*model.SystemSetting, error) {
        var s model.SystemSetting
        var description sql.NullString
        err := r.db.QueryRowContext(ctx, `
                SELECT key, value, description, updated_at
                FROM system_settings
                WHERE key = ?`, key).Scan(
                &s.Key,
                &s.Value,
                &description,
                &s.UpdatedAt,
        )
        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, fmt.Errorf("setting %q not found: %w", key, err)
                }
                return nil, fmt.Errorf("failed to query setting: %w", err)
        }
        s.Description = description.String
        return &s, nil
}

// Set updates an existing setting's value. If the setting does not exist,
// it is created. The updated_at timestamp is refreshed.
func (r *SystemSettingsRepo) Set(ctx context.Context, key, value, description string) error {
        _, err := r.db.ExecContext(ctx, `
                MERGE system_settings AS target
                USING (SELECT ? AS key, ? AS value, ? AS description) AS source
                ON target.key = source.key
                WHEN MATCHED THEN
                        UPDATE SET value = source.value, description = COALESCE(source.description, target.description), updated_at = GETDATE()
                WHEN NOT MATCHED THEN
                        INSERT (key, value, description) VALUES (source.key, source.value, source.description);`,
                key, value, description)
        if err != nil {
                return fmt.Errorf("failed to upsert setting: %w", err)
        }
        return nil
}

// IsEnabled returns true if the given feature flag key exists and its value
// is "1" (or "true"). Returns false for any other value, or if the setting
// does not exist (fail-safe: unknown settings default to disabled).
//
// Use this for boolean feature flags like SettingDiscountCodesEnabled.
func (r *SystemSettingsRepo) IsEnabled(ctx context.Context, key string) (bool, error) {
        s, err := r.Get(ctx, key)
        if err != nil {
                if err == sql.ErrNoRows {
                        return false, nil // setting doesn't exist → feature off (fail-safe)
                }
                return false, err
        }
        return s.Value == "1" || s.Value == "true", nil
}

// List returns all system settings ordered by key. Useful for an admin
// dashboard showing all feature flags.
func (r *SystemSettingsRepo) List(ctx context.Context) ([]model.SystemSetting, error) {
        rows, err := r.db.QueryContext(ctx, `
                SELECT key, value, description, updated_at
                FROM system_settings
                ORDER BY key`)
        if err != nil {
                return nil, fmt.Errorf("failed to list settings: %w", err)
        }
        defer rows.Close()

        var settings []model.SystemSetting
        for rows.Next() {
                var s model.SystemSetting
                var description sql.NullString
                if err := rows.Scan(&s.Key, &s.Value, &description, &s.UpdatedAt); err != nil {
                        return nil, fmt.Errorf("failed to scan setting: %w", err)
                }
                s.Description = description.String
                settings = append(settings, s)
        }
        if err = rows.Err(); err != nil {
                return nil, fmt.Errorf("error iterating settings: %w", err)
        }
        return settings, nil
}
