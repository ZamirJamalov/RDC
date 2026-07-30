package service

import (
        "context"
        "crypto/rand"
        "fmt"
        "log/slog"
        "math"
        "math/big"
        "strings"
        "time"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
)

// DiscountCodeService implements the referral/discount code business logic.
//
// PR #95: service layer on top of DiscountCodeRepo (PR #94).
//
// Responsibilities:
//  1. GenerateForApplication — called on approval: creates a new ALPUL-XXXXXX
//     code owned by the approved customer. Retries on collision.
//  2. ValidateForCustomer — called on customer-confirm: verifies the code
//     belongs to a different customer (self-use prevention), is active, and
//     not expired.
//  3. CalculateDiscount — called on approval: computes the discount amount
//     to subtract from the commission, based on discount_type (percent|fixed).
type DiscountCodeService struct {
        repo DiscountCodeStore
}

// NewDiscountCodeService creates a new DiscountCodeService.
func NewDiscountCodeService(repo DiscountCodeStore) *DiscountCodeService {
        return &DiscountCodeService{repo: repo}
}

// codeCharset is the alphabet used for the random suffix of a discount code.
// 0, O, I, 1 are excluded to avoid visual ambiguity when read over SMS.
const codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// codeSuffixLen is the length of the random suffix (after the ALPUL- prefix).
const codeSuffixLen = 6

// maxGenerateRetries is the number of collision-retry attempts during code
// generation. With a 32-char alphabet and 6-char suffix, the keyspace is
// 32^6 ≈ 1.07 billion — collisions are astronomically unlikely, but we still
// retry defensively.
const maxGenerateRetries = 5

// DefaultDiscountType and DefaultDiscountValue are used when generating a new
// code if the caller does not override. The values are externalised per-code
// (stored in discount_codes.discount_type / discount_value) so admin can
// change them without code changes.
const (
        DefaultDiscountType  = model.DiscountTypePercent
        DefaultDiscountValue = 2.00 // 2% off commission
)

// GenerateForApplication creates a new discount code owned by the customer
// whose application was just approved.
//
// The code is generated with a cryptographically-secure random suffix and
// retried on collision (defensive — keyspace is ~1B). The code is stored
// with status='active', ready to be redeemed by a different customer.
//
// Returns the newly-created DiscountCode (with ID + CreatedAt populated).
func (s *DiscountCodeService) GenerateForApplication(ctx context.Context, appID, customerID int) (*model.DiscountCode, error) {
        if appID <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }
        if customerID <= 0 {
                return nil, fmt.Errorf("invalid customer id")
        }

        var created *model.DiscountCode
        var lastErr error

        for attempt := 1; attempt <= maxGenerateRetries; attempt++ {
                code, err := generateRandomCode()
                if err != nil {
                        lastErr = fmt.Errorf("failed to generate code: %w", err)
                        continue
                }

                exists, err := s.repo.ExistsByCode(ctx, code)
                if err != nil {
                        lastErr = fmt.Errorf("failed to check code existence: %w", err)
                        continue
                }
                if exists {
                        slog.Warn("discount code collision — regenerating",
                                "attempt", attempt,
                                "code", code)
                        continue
                }

                dc := &model.DiscountCode{
                        Code:                    code,
                        IssuedToCustomerID:      customerID,
                        IssuedFromApplicationID: &appID,
                        DiscountType:            DefaultDiscountType,
                        DiscountValue:           DefaultDiscountValue,
                        Status:                  model.DiscountStatusActive,
                }

                if err := s.repo.Create(ctx, dc); err != nil {
                        // Race: another goroutine inserted the same code between our
                        // ExistsByCode check and Create. Retry.
                        lastErr = fmt.Errorf("failed to create discount code: %w", err)
                        continue
                }

                created = dc
                break
        }

        if created == nil {
                return nil, fmt.Errorf("failed to generate unique discount code after %d attempts: %w", maxGenerateRetries, lastErr)
        }

        slog.Info("discount code generated",
                "application_id", appID,
                "customer_id", customerID,
                "code", created.Code,
                "discount_type", created.DiscountType,
                "discount_value", created.DiscountValue)

        return created, nil
}

// GenerateForApplicationTx is the tx-aware variant of GenerateForApplication.
// Used inside the approval transaction so code generation is atomic with the
// status update and mark-used.
func (s *DiscountCodeService) GenerateForApplicationTx(ctx context.Context, runner repository.TxRunner, appID, customerID int) (*model.DiscountCode, error) {
        if appID <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }
        if customerID <= 0 {
                return nil, fmt.Errorf("invalid customer id")
        }

        var created *model.DiscountCode
        var lastErr error

        for attempt := 1; attempt <= maxGenerateRetries; attempt++ {
                code, err := generateRandomCode()
                if err != nil {
                        lastErr = fmt.Errorf("failed to generate code: %w", err)
                        continue
                }

                // Note: ExistsByCode uses r.db, not the tx — but a unique violation
                // on CreateTx will still fail the tx and retry. This is acceptable
                // because the keyspace is huge and collisions are extremely rare.
                exists, err := s.repo.ExistsByCode(ctx, code)
                if err != nil {
                        lastErr = fmt.Errorf("failed to check code existence: %w", err)
                        continue
                }
                if exists {
                        slog.Warn("discount code collision (tx) — regenerating",
                                "attempt", attempt,
                                "code", code)
                        continue
                }

                dc := &model.DiscountCode{
                        Code:                    code,
                        IssuedToCustomerID:      customerID,
                        IssuedFromApplicationID: &appID,
                        DiscountType:            DefaultDiscountType,
                        DiscountValue:           DefaultDiscountValue,
                        Status:                  model.DiscountStatusActive,
                }

                if err := s.repo.CreateTx(ctx, runner, dc); err != nil {
                        lastErr = fmt.Errorf("failed to create discount code: %w", err)
                        continue
                }

                created = dc
                break
        }

        if created == nil {
                return nil, fmt.Errorf("failed to generate unique discount code after %d attempts: %w", maxGenerateRetries, lastErr)
        }

        slog.Info("discount code generated (tx)",
                "application_id", appID,
                "customer_id", customerID,
                "code", created.Code)

        return created, nil
}

// ValidateForCustomer checks whether a discount code entered by a customer is
// valid for their use. Returns the code if valid, or an error explaining why not.
//
// Validation rules:
//  1. Code must exist in the DB.
//  2. Code must be in 'active' status (not 'used' or 'expired').
//  3. Code must NOT belong to the current customer (self-use prevention).
//  4. (Optional) If valid_until is set, it must be in the future.
//
// This is called during customer-confirm. The code is not marked as 'used' at
// this point — that happens atomically on approval (so a rejected application
// does not consume the code).
func (s *DiscountCodeService) ValidateForCustomer(ctx context.Context, code string, currentCustomerID int) (*model.DiscountCode, error) {
        normalized := normalizeCode(code)
        if normalized == "" {
                return nil, fmt.Errorf("endirim kodu boşdur")
        }

        dc, err := s.repo.GetByCode(ctx, normalized)
        if err != nil {
                slog.Info("discount code not found",
                        "code", normalized,
                        "customer_id", currentCustomerID)
                return nil, fmt.Errorf("yanlış endirim kodu")
        }

        if dc.Status != model.DiscountStatusActive {
                slog.Info("discount code not active",
                        "code", normalized,
                        "status", dc.Status,
                        "customer_id", currentCustomerID)
                return nil, fmt.Errorf("bu endirim kodu artıq istifadə olunub")
        }

        if dc.IssuedToCustomerID == currentCustomerID {
                slog.Info("discount code self-use attempt blocked",
                        "code", normalized,
                        "customer_id", currentCustomerID)
                return nil, fmt.Errorf("öz endirim kodunuzdan istifadə edə bilməzsiniz")
        }

        if dc.ValidUntil != nil && dc.ValidUntil.Before(time.Now()) {
                slog.Info("discount code expired",
                        "code", normalized,
                        "valid_until", dc.ValidUntil,
                        "customer_id", currentCustomerID)
                return nil, fmt.Errorf("bu endirim kodunun müddəti bitib")
        }

        return dc, nil
}

// ValidateForCustomerTx is the tx-aware variant of ValidateForCustomer.
// Used inside the approval transaction to re-validate the code (race condition
// protection: between customer-confirm and approval, another customer may
// have redeemed the same code).
func (s *DiscountCodeService) ValidateForCustomerTx(ctx context.Context, runner repository.TxRunner, code string, currentCustomerID int) (*model.DiscountCode, error) {
        normalized := normalizeCode(code)
        if normalized == "" {
                return nil, fmt.Errorf("endirim kodu boşdur")
        }

        dc, err := s.repo.GetByCodeTx(ctx, runner, normalized)
        if err != nil {
                return nil, fmt.Errorf("yanlış endirim kodu")
        }

        if dc.Status != model.DiscountStatusActive {
                return nil, fmt.Errorf("bu endirim kodu artıq istifadə olunub")
        }

        if dc.IssuedToCustomerID == currentCustomerID {
                return nil, fmt.Errorf("öz endirim kodunuzdan istifadə edə bilməzsiniz")
        }

        if dc.ValidUntil != nil && dc.ValidUntil.Before(time.Now()) {
                return nil, fmt.Errorf("bu endirim kodunun müddəti bitib")
        }

        return dc, nil
}

// CalculateDiscount computes the discount amount to subtract from the
// commission, based on the code's discount_type and discount_value.
//
//   - percent: discount = commission × (value / 100)
//   - fixed:   discount = min(value, commission)
//
// The discount is clamped to [0, commission] — it can never exceed the
// commission and can never be negative. This protects against bad data
// (e.g. discount_value > commission).
//
// Returns the discount amount (always >= 0 and <= commissionAmount).
func (s *DiscountCodeService) CalculateDiscount(code *model.DiscountCode, commissionAmount float64) float64 {
        if code == nil {
                return 0
        }
        if code.DiscountValue <= 0 || commissionAmount <= 0 {
                return 0
        }

        var discount float64
        switch code.DiscountType {
        case model.DiscountTypePercent:
                discount = commissionAmount * (code.DiscountValue / 100.0)
        case model.DiscountTypeFixed:
                discount = code.DiscountValue
        default:
                slog.Warn("unknown discount_type — no discount applied",
                        "code", code.Code,
                        "discount_type", code.DiscountType)
                return 0
        }

        // Clamp: discount must be in [0, commissionAmount]
        if discount < 0 {
                discount = 0
        }
        if discount > commissionAmount {
                discount = commissionAmount
        }

        return math.Round(discount*100) / 100
}

// normalizeCode uppercases and trims the code. Discount codes are case-
// insensitive on input (customer may type "alpul-ab12cd") but stored as
// uppercase ("ALPUL-AB12CD"). The DB lookup uses the normalized form.
func normalizeCode(code string) string {
        return strings.ToUpper(strings.TrimSpace(code))
}

// generateRandomCode produces a new code in the form "ALPUL-XXXXXX" where
// XXXXXX is a 6-character random suffix drawn from codeCharset.
//
// Uses crypto/rand for unpredictability (defends against code guessing /
// brute-force enumeration of codes).
func generateRandomCode() (string, error) {
        suffix := make([]byte, codeSuffixLen)
        maxN := big.NewInt(int64(len(codeCharset)))

        for i := 0; i < codeSuffixLen; i++ {
                n, err := rand.Int(rand.Reader, maxN)
                if err != nil {
                        return "", fmt.Errorf("crypto/rand failed: %w", err)
                }
                suffix[i] = codeCharset[n.Int64()]
        }

        return model.DiscountCodePrefix + string(suffix), nil
}
