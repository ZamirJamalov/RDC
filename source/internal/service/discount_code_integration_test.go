package service

import (
        "context"
        "testing"

        "rdc-source/internal/model"
)

// TestApprovalFlow_DiscountCodeLifecycle tests the complete discount code
// lifecycle through the approval flow:
//
//  1. Customer A's application is approved → a new discount code is generated
//     for A
//  2. Customer B enters A's code on customer-confirm → code is validated
//     (active, not self-use) and stored on B's application
//  3. B's application is approved → discount is applied to commission,
//     code is marked as 'used', B gets their own new code
//  4. Customer C tries to use the same code → validation fails (already used)
//
// This test uses mock stores (no real DB) to verify the service-layer logic
// without needing SQL Server.
func TestApprovalFlow_DiscountCodeLifecycle(t *testing.T) {
        // === Setup ===
        discountStore := newMockDiscountCodeStore()
        discountSvc := NewDiscountCodeService(discountStore)

        // Wire up the ApplicationService with all dependencies.
        // We can't easily construct a full ApplicationService (it needs a
        // CreditEngine), so we test the discount logic directly via the
        // service methods that don't require the engine.

        // === Step 1: A's application is approved → generate code for A ===
        t.Log("Step 1: Generating discount code for customer A (appID=1, customerID=100)")
        codeA, err := discountSvc.GenerateForApplication(context.Background(), 1, 100)
        if err != nil {
                t.Fatalf("Step 1: GenerateForApplication failed: %v", err)
        }
        if codeA == nil {
                t.Fatal("Step 1: expected non-nil code")
        }
        if codeA.Status != model.DiscountStatusActive {
                t.Errorf("Step 1: status should be 'active', got %q", codeA.Status)
        }
        if codeA.IssuedToCustomerID != 100 {
                t.Errorf("Step 1: issued_to_customer_id should be 100, got %d", codeA.IssuedToCustomerID)
        }
        t.Logf("Step 1: ✓ Code generated: %s (owner: customer 100)", codeA.Code)

        // === Step 2: B enters A's code → validation passes (different customer) ===
        t.Log("Step 2: Customer B (200) validates A's code")
        dc, err := discountSvc.ValidateForCustomer(context.Background(), codeA.Code, 200)
        if err != nil {
                t.Fatalf("Step 2: ValidateForCustomer failed: %v", err)
        }
        if dc.Code != codeA.Code {
                t.Errorf("Step 2: returned code %q != expected %q", dc.Code, codeA.Code)
        }
        t.Logf("Step 2: ✓ Code validated for customer B")

        // === Step 3: B's application is approved → discount applied + code marked used ===
        t.Log("Step 3: B's application approved — applying discount + marking code used")

        // PR #109: discount artıq interestAmount-dan çıxılır (komissiyadan yox)
        // 500 AZN, annual_interest_rate=55, term=3 ay
        // interestAmount = 500 × 0.55 × (3/12) = 68.75 AZN
        interestAmount := calculateInterestAmount(500, 55, 3)
        discount := discountSvc.CalculateDiscount(dc, interestAmount)
        if discount <= 0 {
                t.Errorf("Step 3: expected positive discount, got %v", discount)
        }
        t.Logf("Step 3: Interest amount: %.2f, Discount: %.2f", interestAmount, discount)

        // PR #109: total_amount dəyişmir (discount yalnız interestAmount-a təsir edir)
        totalAmount := calculateTotalAmountWithDiscount(500, 14, discount)
        noDiscountTotal := calculateTotalAmount(500, 14)
        if totalAmount != noDiscountTotal {
                t.Errorf("Step 3: PR #109 — total should not change. expected %v, got %v",
                        noDiscountTotal, totalAmount)
        }
        t.Logf("Step 3: Total (unchanged): %.2f, Discount from interest: %.2f",
                totalAmount, discount)

        // Mark the code as used
        if err := discountStore.MarkUsed(context.Background(), dc.ID, 2); err != nil {
                t.Fatalf("Step 3: MarkUsed failed: %v", err)
        }
        // Verify the code is now 'used'
        usedCode, err := discountStore.GetByCode(context.Background(), codeA.Code)
        if err != nil {
                t.Fatalf("Step 3: GetByCode after MarkUsed failed: %v", err)
        }
        if usedCode.Status != model.DiscountStatusUsed {
                t.Errorf("Step 3: status should be 'used', got %q", usedCode.Status)
        }
        t.Logf("Step 3: ✓ Code marked as 'used' (used_by_application_id=%d)", 2)

        // B also gets their own new code
        codeB, err := discountSvc.GenerateForApplication(context.Background(), 2, 200)
        if err != nil {
                t.Fatalf("Step 3: GenerateForApplication for B failed: %v", err)
        }
        t.Logf("Step 3: ✓ New code generated for B: %s", codeB.Code)

        // === Step 4: C tries to use A's code (now used) → validation fails ===
        t.Log("Step 4: Customer C (300) tries to use A's already-used code")
        _, err = discountSvc.ValidateForCustomer(context.Background(), codeA.Code, 300)
        if err == nil {
                t.Fatal("Step 4: expected error for already-used code, got nil")
        }
        t.Logf("Step 4: ✓ Validation correctly rejected: %v", err)

        // === Step 5: A tries to use their own code → self-use prevention ===
        // First, generate a fresh code for A (the previous one is used)
        t.Log("Step 5: Customer A (100) tries to use their own fresh code")
        freshCodeA, err := discountSvc.GenerateForApplication(context.Background(), 3, 100)
        if err != nil {
                t.Fatalf("Step 5: GenerateForApplication failed: %v", err)
        }
        _, err = discountSvc.ValidateForCustomer(context.Background(), freshCodeA.Code, 100)
        if err == nil {
                t.Fatal("Step 5: expected self-use error, got nil")
        }
        t.Logf("Step 5: ✓ Self-use prevention worked: %v", err)
}

// TestDiscountCode_PercentDiscountCalculation tests the math for a percent
// discount applied to interestAmount (PR #109).
//
//      Principal: 500 AZN
//      Annual interest rate: 55%
//      Term: 3 months
//      Discount: 10% of interest
//
// Expected:
//      interestAmount = 500 × 0.55 × (3/12) = 68.75
//      discount = 68.75 × 10/100 = 6.88
//      discounted_interest = 68.75 - 6.88 = 61.87
//      total_amount (→ LW) = 500 + commission (dəyişmir — discount yalnız interestAmount-a təsir edir)
func TestDiscountCode_PercentDiscountCalculation(t *testing.T) {
        principal := 500.0
        annualInterestRate := 55.0
        termMonths := 3

        interestAmount := calculateInterestAmount(principal, annualInterestRate, termMonths)
        t.Logf("Interest amount: %.4f", interestAmount)

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypePercent,
                DiscountValue: 10.0, // 10% off interest
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, interestAmount)
        t.Logf("Discount (10%% of %.4f): %.4f", interestAmount, discount)

        // PR #109: total_amount dəyişmir
        commission := 14.0
        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        totalWithoutDiscount := calculateTotalAmount(principal, commission)
        t.Logf("Total without discount: %.2f", totalWithoutDiscount)
        t.Logf("Total with discount:    %.2f (unchanged — PR #109)", totalWithDiscount)
        t.Logf("Discount amount:        %.2f (from interest)", discount)
        t.Logf("Discounted interest:    %.4f", interestAmount-discount)

        // PR #109: total dəyişməməlidir
        if totalWithDiscount != totalWithoutDiscount {
                t.Errorf("PR #109: total should not change. expected %v, got %v",
                        totalWithoutDiscount, totalWithDiscount)
        }
        // Discount 0-dan böyük olmalıdır
        if discount <= 0 {
                t.Errorf("discount should be > 0, got %v", discount)
        }
}

// TestDiscountCode_FixedDiscountCalculation tests a fixed-amount discount
// applied to interestAmount (PR #109).
//
// With principal=500, annual_interest_rate=55, term=3 ay:
//   interestAmount = 500 × 0.55 × (3/12) = 68.75 AZN
// A fixed 5 AZN discount is fully applied (5 < 68.75).
//
// total_amount (→ LW) = principal + commission (dəyişmir)
// discountAmount = 5 AZN (interestAmount-dan çıxılır, frontend-də göstərilir)
func TestDiscountCode_FixedDiscountCalculation(t *testing.T) {
        principal := 500.0
        annualInterestRate := 55.0
        termMonths := 3

        interestAmount := calculateInterestAmount(principal, annualInterestRate, termMonths)
        t.Logf("Interest amount: %.4f (principal=%.0f, annual_rate=%.0f%%, term=%d ay)",
                interestAmount, principal, annualInterestRate, termMonths)

        // 5 AZN discount < 68.75 interest → fully applied
        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 5.0,
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, interestAmount)
        if discount != 5.0 {
                t.Errorf("expected discount 5.0 (smaller than interest %.2f), got %v",
                        interestAmount, discount)
        }

        // PR #109: total_amount dəyişmir (discount yalnız interestAmount-a təsir edir)
        commission := 14.0
        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        totalWithoutDiscount := calculateTotalAmount(principal, commission)
        if totalWithDiscount != totalWithoutDiscount {
                t.Errorf("PR #109: total should not change with discount. expected %v, got %v",
                        totalWithoutDiscount, totalWithDiscount)
        }

        t.Logf("Total (unchanged):       %.2f", totalWithDiscount)
        t.Logf("Discount amount:         %.2f (from interest %.2f)", discount, interestAmount)
        t.Logf("Discounted interest:     %.2f", interestAmount-discount)
}

// TestDiscountCode_FixedDiscountLargerThanInterest tests that a fixed
// discount larger than the interestAmount is clamped (interest can't go negative).
//
// With principal=300, annual_interest_rate=55, term=3 ay:
//   interestAmount = 300 × 0.55 × (3/12) = 41.25 AZN
// A 500 AZN discount is clamped to 41.25 (the full interest).
func TestDiscountCode_FixedDiscountLargerThanInterest(t *testing.T) {
        principal := 300.0
        annualInterestRate := 55.0
        termMonths := 3

        interestAmount := calculateInterestAmount(principal, annualInterestRate, termMonths)
        t.Logf("Interest amount: %.4f (principal=%.0f, annual_rate=%.0f%%, term=%d ay)",
                interestAmount, principal, annualInterestRate, termMonths)

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 500.0, // 500 AZN off — way more than interest (41.25)
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, interestAmount)
        t.Logf("Discount requested: 500.0, applied: %.4f (clamped to interest)", discount)

        // Discount should be clamped to the interest amount
        if discount < interestAmount-0.01 || discount > interestAmount+0.01 {
                t.Errorf("expected discount clamped to interest ≈ %.4f, got %v",
                        interestAmount, discount)
        }

        // PR #109: total_amount yine dəyişmir
        commission := 14.0
        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        totalWithoutDiscount := calculateTotalAmount(principal, commission)
        if totalWithDiscount != totalWithoutDiscount {
                t.Errorf("PR #109: total should not change. expected %v, got %v",
                        totalWithoutDiscount, totalWithDiscount)
        }
}

// TestApprovalFlow_MultipleCodesForSameCustomer tests that a customer can
// have multiple discount codes (one per approved application). This is
// important for the referral chain — each approval generates a new code.
func TestApprovalFlow_MultipleCodesForSameCustomer(t *testing.T) {
        store := newMockDiscountCodeStore()
        svc := NewDiscountCodeService(store)

        // Customer 100 gets three codes from three separate approvals
        codes := make([]string, 3)
        for i := 1; i <= 3; i++ {
                code, err := svc.GenerateForApplication(context.Background(), i, 100)
                if err != nil {
                        t.Fatalf("iteration %d: GenerateForApplication failed: %v", i, err)
                }
                codes[i-1] = code.Code
        }

        // All three codes should be distinct
        for i := 0; i < 3; i++ {
                for j := i + 1; j < 3; j++ {
                        if codes[i] == codes[j] {
                                t.Errorf("codes[%d] == codes[%d] (%q) — should be distinct",
                                        i, j, codes[i])
                        }
                }
        }

        // All three should be active and owned by customer 100
        for _, codeStr := range codes {
                dc, err := svc.ValidateForCustomer(context.Background(), codeStr, 200)
                if err != nil {
                        t.Errorf("expected code %q to be valid for customer 200, got error: %v",
                                codeStr, err)
                }
                if dc.IssuedToCustomerID != 100 {
                        t.Errorf("code %q: issued_to_customer_id should be 100, got %d",
                                codeStr, dc.IssuedToCustomerID)
                }
        }
}
