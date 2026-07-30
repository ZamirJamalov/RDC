package model

import "time"

// SystemSetting represents a runtime-configurable key-value pair stored in the
// system_settings table. Used for feature flags and admin-toggled settings
// that can be changed without redeploying the application.
//
// PR #98: introduced for the discount_codes_enabled feature flag, which lets
// an admin turn the entire discount code feature on/off with one command.
type SystemSetting struct {
	Key         string     `json:"key"`
	Value       string     `json:"value"`
	Description string     `json:"description,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Known system setting keys.
const (
	// SettingDiscountCodesEnabled toggles the discount code feature.
	// "1" = enabled (default), "0" = disabled.
	// When disabled:
	//   - GET /api/discount-codes/validate returns valid=false with reason="feature_disabled"
	//   - CustomerConfirmApplication ignores the discount_code field
	//   - Approval flow skips discount application + SMS
	SettingDiscountCodesEnabled = "discount_codes_enabled"
)
