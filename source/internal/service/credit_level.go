package service

import (
        "fmt"
        "math"
        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/lw"
        "strings"
)

// loanAnalytics holds per-level loan history metrics.
type loanAnalytics struct {
        hasActive      bool
        loansByLevel   map[string][]completedLoanInfo
        completedCount int
        allOnTime      bool

        // PR #51 — additional rejection-rule inputs (fetched from LW / external sources).
        // These are populated by the engine before calling computeDecision.

        // akbScore is the resolved AKB credit score (LW override > request fallback).
        // Rule: score < 200 → reject.
        akbScore int

        // akbStopFactors is the list of 2-letter stop factor codes from AKB
        // (e.g. ["AB", "TY"]). Rule: any non-empty list → reject.
        akbStopFactors []string

        // customerAge is the customer's age in years, computed from DOB
        // returned by GetPersonalInfo. Rule: age > 69 → reject.
        customerAge int
}

type completedLoanInfo struct {
        DelayDays  int
        TermMonths int
}

// computeAnalytics extracts per-level loan history from LW records.
func computeAnalytics(loans []lw.CustomerLoan) *loanAnalytics {
        a := &loanAnalytics{loansByLevel: make(map[string][]completedLoanInfo), allOnTime: true}
        for _, loan := range loans {
                if loan.Status == "active" {
                        a.hasActive = true
                }
                if loan.Status == "completed" || loan.Status == "closed" {
                        a.completedCount++
                        if loan.DelayDays > 0 {
                                a.allOnTime = false
                        }
                        level := loan.LevelAtClose
                        if level == "" {
                                level = model.CreditLevelNew
                        }
                        a.loansByLevel[level] = append(a.loansByLevel[level], completedLoanInfo{
                                DelayDays:  loan.DelayDays,
                                TermMonths: loan.TermMonths,
                        })
                }
        }
        return a
}

// levelRule defines promotion criteria for a credit level.
type levelRule struct {
        nextLevel     string
        maxDelayDays  int
        minTermMonths int
        requiredLoans int
}

// levelRules maps each level to its promotion rule.
var levelRules = map[string]levelRule{
        model.CreditLevelNew:      {nextLevel: model.CreditLevelTrusted, maxDelayDays: 2, minTermMonths: 3, requiredLoans: 2},
        model.CreditLevelTrusted:  {nextLevel: model.CreditLevelValuable, maxDelayDays: 3, minTermMonths: 3, requiredLoans: 2},
        model.CreditLevelValuable: {nextLevel: model.CreditLevelElite, maxDelayDays: 4, minTermMonths: 2, requiredLoans: 2},
}

// previousLevel returns the level below the given one.
func previousLevel(level string) string {
        switch level {
        case model.CreditLevelTrusted:
                return model.CreditLevelNew
        case model.CreditLevelValuable:
                return model.CreditLevelTrusted
        case model.CreditLevelElite:
                return model.CreditLevelValuable
        default:
                return model.CreditLevelNew
        }
}

// determineCreditLevel evaluates sequential level progression and delay downgrade.
//
// Logic:
//  1. AKB 700+ → override to "valuable"
//  2. If no current level → "new"
//  3. Check if delay exceeded at current level → downgrade to previous
//  4. Check promotion conditions at current level → promote to next
//  5. Otherwise → stay at current level
func determineCreditLevel(a *loanAnalytics, akbScore int, currentLevel string) string {
        if akbScore >= 700 {
                return model.CreditLevelValuable
        }
        if currentLevel == "" {
                currentLevel = model.CreditLevelNew
        }
        rule, hasRule := levelRules[currentLevel]
        loans := a.loansByLevel[currentLevel]

        // Check delay exceeded → downgrade
        if hasRule && len(loans) > 0 {
                maxDelay := 0
                for _, l := range loans {
                        if l.DelayDays > maxDelay {
                                maxDelay = l.DelayDays
                        }
                }
                if maxDelay > rule.maxDelayDays {
                        return previousLevel(currentLevel)
                }
        }

        // Check promotion
        if hasRule && len(loans) >= rule.requiredLoans {
                allMeetTerm := true
                for _, l := range loans {
                        if l.TermMonths < rule.minTermMonths {
                                allMeetTerm = false
                                break
                        }
                }
                if allMeetTerm {
                        return rule.nextLevel
                }
        }

        return currentLevel
}

// buildRangeSummary creates a human-readable summary of available ranges.
func buildRangeSummary(ranges []repository.LevelRange, unlockPhase int) string {
        if len(ranges) == 0 {
                return "Bu level ucun hec bir araliq konfiqurasiya edilmeyib."
        }
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
