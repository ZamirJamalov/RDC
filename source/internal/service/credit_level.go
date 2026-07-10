package service

import (
	"fmt"
	"math"
	"rdc-source/internal/repository"
	"rdc-source/pkg/lw"
	"strings"
)

// loanAnalytics holds pre-computed loan history metrics used by the credit engine.
type loanAnalytics struct {
	completedCount int
	allOnTime      bool
	hasEarly       bool
	hasActive      bool
}

// computeAnalytics extracts loan history metrics from a list of LW loan records.
func computeAnalytics(loans []lw.CustomerLoan) *loanAnalytics {
	a := &loanAnalytics{allOnTime: true}
	for _, loan := range loans {
		if loan.Status == "active" {
			a.hasActive = true
		}
		if loan.Status == "completed" || loan.Status == "closed" {
			a.completedCount++
			if !loan.WasOnTime {
				a.allOnTime = false
			}
			if loan.EarlyCompletion {
				a.hasEarly = true
			}
		}
	}
	return a
}

// determineCreditLevel maps loan history analytics + AKB score to a credit level.
//
// Rules:
//   - AKB score 700+: overrides to "valuable" regardless of LW history
//   - New:     No completed loans (and AKB < 700)
//   - Trusted: 1+ completed loans, all on time (and AKB < 700)
//   - Valuable: 2+ completed loans, all on time (and AKB < 700)
//   - Elite:   2+ completed loans, all on time, at least 1 early completion (and AKB < 700)
func determineCreditLevel(a *loanAnalytics, akbScore int) string {
	// AKB 700+ override: directly assign valuable level
	if akbScore >= 700 {
		return "valuable"
	}

	if a.completedCount == 0 {
		return "new"
	}
	if a.completedCount >= 2 && a.allOnTime && a.hasEarly {
		return "elite"
	}
	if a.completedCount >= 2 && a.allOnTime {
		return "valuable"
	}
	if a.completedCount >= 1 && a.allOnTime {
		return "trusted"
	}
	// Has completed loans but with late payments — stays at new
	return "new"
}

// buildRangeSummary creates a human-readable summary of available ranges for error messages.
func buildRangeSummary(ranges []repository.LevelRange, unlockPhase int) string {
	if len(ranges) == 0 {
		return "Bu level ucun hec bir araliq konfiqurasiya edilmeyib."
	}

	// Collect unique terms and track min/max amounts
	terms := make(map[int]bool)
	minAmt := math.MaxFloat64
	maxAmt := 0.0
	for _, r := range ranges {
		terms[r.TermMonths] = true
		if r.MinAmount < minAmt {
			minAmt = r.MinAmount
		}
		if r.MaxAmount > maxAmt {
			maxAmt = r.MaxAmount
		}
	}

	// Build term list
	termList := make([]string, 0, len(terms))
	for t := range terms {
		termList = append(termList, fmt.Sprintf("%d ay", t))
	}

	phaseNote := ""
	if unlockPhase == 1 {
		phaseNote = " (phase 1 — daha genis araliq ucun once 1 kredit baglayin)"
	}

	return fmt.Sprintf("Desteklenen araliq: %.0f-%.0f AZN. Desteklenen muddeetler: %s.%s",
		minAmt, maxAmt, strings.Join(termList, ", "), phaseNote)
}
