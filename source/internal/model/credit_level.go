package model

// CreditLevel defines a credit tier with amount ranges, term, and corresponding interest rate.
type CreditLevel struct {
	ID          int     `json:"id"`
	LevelName   string  `json:"level_name"` // new, trusted, valuable, elite
	MinAmount   float64 `json:"min_amount"`
	MaxAmount   float64 `json:"max_amount"`
	TermMonths  int     `json:"term_months"`
	Rate        float64 `json:"rate"`         // percentage, e.g. 30.00 for 30%
	UnlockPhase int     `json:"unlock_phase"` // 1 = first loan, 2 = after 1+ approved loan at this level
	IsActive    bool    `json:"is_active"`
}

// CreditLevelRule defines the criteria for transitioning between credit levels.
type CreditLevelRule struct {
	ID                 int    `json:"id"`
	FromLevel          string `json:"from_level"`
	ToLevel            string `json:"to_level"`
	MinCompletedLoans  int    `json:"min_completed_loans"`
	AllOnTime          bool   `json:"all_on_time"`
	HasEarlyCompletion bool   `json:"has_early_completion"`
	IsActive           bool   `json:"is_active"`
}
