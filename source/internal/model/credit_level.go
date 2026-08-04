package model

// CreditLevel defines a credit tier with amount ranges, term, commission rate,
// and annual interest rate.
//
// PR #86 (migration 021): the 'rate' column was renamed to 'commission' in DB.
// PR #109: the model field is renamed to match (was misleadingly called Rate).
type CreditLevel struct {
        ID                int     `json:"id"`
        LevelName         string  `json:"level_name"`           // new, trusted, valuable, elite
        MinAmount         float64 `json:"min_amount"`
        MaxAmount         float64 `json:"max_amount"`
        TermMonths        int     `json:"term_months"`
        Commission        float64 `json:"commission"`           // PR #109: commission rate, e.g. 14.00 for 14% (was Rate)
        AnnualInterestRate float64 `json:"annual_interest_rate"` // PR #109: annual interest rate, e.g. 55.00 for 55%
        UnlockPhase       int     `json:"unlock_phase"`          // 1 = first loan, 2 = after 1+ approved loan at this level
        IsActive          bool    `json:"is_active"`
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
