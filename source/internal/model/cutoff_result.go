package model

import "time"

// CutoffResult represents a single cutoff check result for an application.
// PR #168: Hər müraciət üzrə plan/fakt nəticələri.
type CutoffResult struct {
	ID            int       `json:"id"`
	ApplicationID int       `json:"application_id"`
	CutoffCode    string    `json:"cutoff_code"`    // AKB_SCORE_LOW, AZMK_BLACKLIST, etc.
	CutoffName    string    `json:"cutoff_name"`    // Human-readable name
	ServiceName   string    `json:"service_name"`   // AZMK_GET_MKR_SCORE, etc.
	Checked       bool      `json:"checked"`        // Was this check performed?
	Passed        bool      `json:"passed"`         // 1=passed, 0=rejected
	ActualValue   string    `json:"actual_value"`   // "point=150", "ratio=7.5"
	Threshold     string    `json:"threshold"`      // "< 200", "> 6"
	Details       string    `json:"details"`
	CreatedAt     time.Time `json:"created_at"`
}
