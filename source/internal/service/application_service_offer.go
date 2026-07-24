package service

import (
        "context"
        "fmt"
)

// GetOffer returns the available amount/term ranges for a customer's credit
// level (T-6.5). Used by the frontend to show the customer what they can
// borrow before creating an application.
//
// Pipeline:
//  1. Fetch customer loans from LW
//  2. Resolve AKB score (LW first, request fallback)
//  3. Determine credit level (new/trusted/valuable/elite)
//  4. Determine unlock phase (1 = first loan, 2 = 1+ approved)
//  5. Get all rate ranges for this level + phase
func (s *ApplicationService) GetOffer(ctx context.Context, customerPIN string, akbScore int) (*OfferResponse, error) {
        if customerPIN == "" {
                return nil, fmt.Errorf("customer_pin is required")
        }

        // 1. Fetch customer loans from LW to determine credit level
        customerLoans, err := s.creditEngine.lwProvider.GetCustomerLoans(ctx, customerPIN)
        if err != nil {
                return nil, fmt.Errorf("failed to fetch customer loans: %w", err)
        }

        // 2. Resolve AKB score (LW first, fallback to request)
        resolvedAkb := s.creditEngine.resolveAkbScore(ctx, customerPIN, akbScore)

        // 3. Determine credit level
        analytics := computeAnalytics(customerLoans.Loans)
        currentLevel, _ := s.repo.GetCustomerCurrentLevel(ctx, customerPIN)
        creditLevel := determineCreditLevel(analytics, resolvedAkb, currentLevel)

        // 4. Determine unlock phase
        approvedCount, err := s.repo.CountApprovedAtLevel(ctx, customerPIN, creditLevel)
        if err != nil {
                return nil, fmt.Errorf("failed to count approved loans: %w", err)
        }
        unlockPhase := resolveUnlockPhase(approvedCount)

        // 5. Get all rate ranges for this level + phase
        repoRanges, err := s.repo.GetLevelRanges(ctx, creditLevel, unlockPhase)
        if err != nil {
                return nil, fmt.Errorf("failed to get level ranges: %w", err)
        }

        // Convert repository types to response types
        ranges := make([]OfferRange, len(repoRanges))
        for i, r := range repoRanges {
                ranges[i] = OfferRange{
                        MinAmount:          r.MinAmount,
                        MaxAmount:          r.MaxAmount,
                        TermMonths:         r.TermMonths,
                        Rate:               r.Rate,
                        Phase:              r.Phase,
                        AnnualInterestRate: r.AnnualInterestRate,
                }
        }

        return &OfferResponse{
                CustomerPIN: customerPIN,
                CreditLevel: creditLevel,
                UnlockPhase: unlockPhase,
                AkbScore:    resolvedAkb,
                Ranges:      ranges,
        }, nil
}

// OfferResponse is returned by GetOffer (T-6.5).
type OfferResponse struct {
        CustomerPIN string       `json:"customer_pin"`
        CreditLevel string       `json:"credit_level"`
        UnlockPhase int          `json:"unlock_phase"`
        AkbScore    int          `json:"akb_score"`
        Ranges      []OfferRange `json:"ranges"`
}

// OfferRange is a single amount/term/rate combination available to the customer.
//
// PR #78: Rate is the COMMISSION rate (from credit_levels.rate).
// AnnualInterestRate is the real annual interest rate (55/52/48/45).
// The frontend uses these to compute:
//   commission_amount  = principal × (rate / (100 - rate)) × 100
//   credit_amount      = principal + commission_amount
//   transfer_amount    = principal  (only the principal is transferred to card)
//   interest_amount    = principal × annual_interest_rate × (term_months / 12)
//   total_repayment    = credit_amount + interest_amount
//   monthly_payment    = total_repayment / term_months
type OfferRange struct {
        MinAmount          float64 `json:"min_amount"`
        MaxAmount          float64 `json:"max_amount"`
        TermMonths         int     `json:"term_months"`
        Rate               float64 `json:"rate"`                  // commission rate
        Phase              int     `json:"phase"`
        AnnualInterestRate float64 `json:"annual_interest_rate"`  // PR #78: 55/52/48/45
}
