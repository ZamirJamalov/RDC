package service

import (
	"context"
	"fmt"
	"log/slog"

	"rdc-source/pkg/azmk"
	"rdc-source/pkg/lw"
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
//
// PR #203: LW xətası olanda fail-soft — boş customer loans ilə davam et.
func (s *ApplicationService) GetOffer(ctx context.Context, customerPIN, customerSerial string, akbScore int) (*OfferResponse, error) {
	if customerPIN == "" {
		return nil, fmt.Errorf("customer_pin is required")
	}

	// PR #265: AZMK InquireByIdCard (LW silindi)
	customerLoans := &lw.CustomerLoansResponse{Loans: []lw.CustomerLoan{}}
	if s.creditEngine.customerDataProvider != nil && customerSerial != "" { // PR #379: serial boşdursa AZMK 400 qaytarır — ötür
		// PR #380: 3 günlük cache — HIT olsa fiziki çağırış edilmir
		// (app-agnostik çağırışdır — marker row NULL app_id ilə yazılır)
		var history *azmk.CreditHistory
		if cached, ok := s.GetCachedServiceResponse(ctx, azmk.AppIDFromContext(ctx), "AZMK_INQUIRE_BY_ID_CARD", customerPIN); ok {
			history = creditHistoryFromCache(cached)
		}
		if history == nil {
			var err error
			history, err = s.creditEngine.customerDataProvider.InquireByIdCard(ctx, customerPIN, customerSerial)
			if err != nil {
				slog.Warn("GetOffer: AZMK InquireByIdCard failed — fail-soft", "customer_pin", customerPIN, "error", err)
			}
		}
		if history != nil {
			customerLoans = convertAzmkHistoryToLwLoans(history)
		}
	}

	// 2. Resolve AKB score (LW first, fallback to request, fallback to DB)
	// PR #229: əgər akbScore = 0-dırsa, DB-dən oxu (OTP verify-də AZMK yazıb — PR #228)
	if akbScore <= 0 {
		akbScore = s.repo.GetRecentAkbScore(ctx, customerPIN)
		if akbScore > 0 {
			slog.Info("GetOffer: using AKB score from DB", "customer_pin", customerPIN, "akb_score", akbScore)
		}
	}
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
			Commission:         r.Commission,
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
// The frontend uses these to compute (PR #246):
//
//	commission_percent  = (rate / (100 - rate)) × 100   (15% → 17.65%)
//	commission_amount   = principal × commission_percent / 100
//	credit_amount       = principal + commission_amount
//	transfer_amount     = principal  (only the principal is transferred to card)
//	interest_amount     = principal × annual_interest_rate × (term_months / 12)
//	total_repayment     = credit_amount + interest_amount
//	monthly_payment     = total_repayment / term_months
type OfferRange struct {
	MinAmount          float64 `json:"min_amount"`
	MaxAmount          float64 `json:"max_amount"`
	TermMonths         int     `json:"term_months"`
	Commission         float64 `json:"commission"` // PR #86: commission rate
	Phase              int     `json:"phase"`
	AnnualInterestRate float64 `json:"annual_interest_rate"` // 55/52/48/45
}
