package model

import "time"

// DiscountCode represents a referral / discount code in the system.
//
// Lifecycle:
//   1. When a loan application is approved, the system generates a new code
//      with status='active' and ties it to the approved customer (owner).
//   2. The owner shares the code with another customer.
//   3. The other customer enters the code in apply.html (optional field).
//   4. On customer-confirm, the code is validated (active, belongs to someone else).
//   5. On the second customer's approval, the discount is applied to commission
//      and the code is marked as status='used'.
//
// Rules:
//   - Self-use prevention: owner cannot redeem own code.
//   - Single-use: each code can only be redeemed once.
//   - External control: discount_type and discount_value are stored per code
//     (so admin can change discount levels without code changes).
type DiscountCode struct {
	ID                      int        `json:"id"`
	Code                    string     `json:"code"`                              // ALPUL-AB12CD
	IssuedToCustomerID      int        `json:"issued_to_customer_id"`             // owner (FK customers.id)
	IssuedFromApplicationID int        `json:"issued_from_application_id"`        // approval that generated it (FK loan_applications.id)
	DiscountType            string     `json:"discount_type"`                     // 'percent' | 'fixed'
	DiscountValue           float64    `json:"discount_value"`                    // percent: 2.00=2%; fixed: 5.00=5 AZN
	Status                  string     `json:"status"`                            // 'active' | 'used' | 'expired'
	UsedByApplicationID     *int       `json:"used_by_application_id,omitempty"`  // which app redeemed it
	UsedAt                  *time.Time `json:"used_at,omitempty"`                 // when redeemed
	ValidUntil              *time.Time `json:"valid_until,omitempty"`             // optional expiry
	CreatedAt               time.Time  `json:"created_at"`
}

// Discount code types.
const (
	DiscountTypePercent = "percent" // discount = commission × (value / 100)
	DiscountTypeFixed   = "fixed"   // discount = min(value, commission)
)

// Discount code statuses.
const (
	DiscountStatusActive  = "active"  // available for redemption
	DiscountStatusUsed    = "used"    // already redeemed
	DiscountStatusExpired = "expired" // past valid_until (or manually expired)
)

// DiscountCodePrefix is the prefix used for all generated codes.
// Format: ALPUL-XXXXXX (6 uppercase alphanumeric chars, O/I/0/1 excluded).
const DiscountCodePrefix = "ALPUL-"
