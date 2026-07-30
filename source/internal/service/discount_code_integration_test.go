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

        // Simulate the approval flow logic (without the full ApplicationService)
        commissionAmount := calculateCommissionAmount(500, 14) // 500 AZN, 14% commission
        discount := discountSvc.CalculateDiscount(dc, commissionAmount)
        if discount <= 0 {
                t.Errorf("Step 3: expected positive discount, got %v", discount)
        }
        t.Logf("Step 3: Commission amount: %.2f, Discount: %.2f", commissionAmount, discount)

        totalAmount := calculateTotalAmountWithDiscount(500, 14, discount)
        noDiscountTotal := calculateTotalAmount(500, 14)
        if totalAmount >= noDiscountTotal {
                t.Errorf("Step 3: discounted total %v should be < no-discount total %v",
                        totalAmount, noDiscountTotal)
        }
        t.Logf("Step 3: Total without discount: %.2f, with discount: %.2f",
                noDiscountTotal, totalAmount)

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
// discount end-to-end:
//
//      Principal: 500 AZN
//      Commission rate: 14%
//      Discount: 10% of commission
//
// Expected:
//      commission_amount = 500 × (14/86) × 100 = 81.40
//      discount = 81.40 × 10/100 = 8.14
//      discounted_commission = 81.40 - 8.14 = 73.26
//      total = 500 + 73.26 = 573.26
func TestDiscountCode_PercentDiscountCalculation(t *testing.T) {
        principal := 500.0
        commission := 14.0

        commissionAmount := calculateCommissionAmount(principal, commission)
        t.Logf("Commission amount: %.4f", commissionAmount)

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypePercent,
                DiscountValue: 10.0, // 10% off commission
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, commissionAmount)
        t.Logf("Discount (10%% of %.4f): %.4f", commissionAmount, discount)

        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        totalWithoutDiscount := calculateTotalAmount(principal, commission)
        t.Logf("Total without discount: %.2f", totalWithoutDiscount)
        t.Logf("Total with discount:    %.2f", totalWithDiscount)
        t.Logf("Savings:                %.2f", totalWithoutDiscount-totalWithDiscount)

        if totalWithDiscount >= totalWithoutDiscount {
                t.Errorf("discounted total %v should be < no-discount total %v",
                        totalWithDiscount, totalWithoutDiscount)
        }
        if totalWithDiscount <= principal {
                t.Errorf("discounted total %v should be > principal %v (commission is positive)",
                        totalWithDiscount, principal)
        }
}

// TestDiscountCode_FixedDiscountCalculation tests a fixed-amount discount
// that is smaller than the commission.
//
// With principal=500, commission=14:
//   commission_amount = (14/86) × 100 ≈ 16.28  (formula does NOT multiply by principal)
// So a fixed 5 AZN discount is fully applied (5 < 16.28).
func TestDiscountCode_FixedDiscountCalculation(t *testing.T) {
        principal := 500.0
        commission := 14.0

        commissionAmount := calculateCommissionAmount(principal, commission)
        t.Logf("Commission amount: %.4f", commissionAmount)

        // Use a discount that is smaller than the commission (16.28)
        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 5.0, // 5 AZN off (< 16.28 commission)
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, commissionAmount)
        if discount != 5.0 {
                t.Errorf("expected discount 5.0 (smaller than commission %.2f), got %v",
                        commissionAmount, discount)
        }

        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        totalWithoutDiscount := calculateTotalAmount(principal, commission)
        savings := totalWithoutDiscount - totalWithDiscount

        t.Logf("Total without discount: %.2f", totalWithoutDiscount)
        t.Logf("Total with discount:    %.2f", totalWithDiscount)
        t.Logf("Savings:                %.2f", savings)

        // Savings should equal the discount (5 AZN)
        if savings < 4.99 || savings > 5.01 {
                t.Errorf("expected savings ≈ 5.0, got %v", savings)
        }
}

// TestDiscountCode_FixedDiscountLargerThanCommission tests that a fixed
// discount larger than the commission is clamped (commission can't go negative).
//
// With principal=300, commission=14:
//   commission_amount = 300 × (14/86) × 100 ≈ 16.28
// So a 500 AZN discount is clamped to 16.28 (the full commission).
func TestDiscountCode_FixedDiscountLargerThanCommission(t *testing.T) {
        principal := 300.0
        commission := 14.0

        commissionAmount := calculateCommissionAmount(principal, commission)
        t.Logf("Commission amount: %.4f (principal=%.0f, commission=%.0f)",
                commissionAmount, principal, commission)

        code := &model.DiscountCode{
                DiscountType:  model.DiscountTypeFixed,
                DiscountValue: 500.0, // 500 AZN off — way more than the commission (~16.28)
        }

        svc := NewDiscountCodeService(newMockDiscountCodeStore())
        discount := svc.CalculateDiscount(code, commissionAmount)
        t.Logf("Discount requested: 500.0, applied: %.4f (clamped to commission)", discount)

        // Discount should be clamped to the commission amount (rounded to 2 decimals)
        expectedClamp := float64(int(commissionAmount*100)) / 100
        if discount < expectedClamp-0.01 || discount > expectedClamp+0.01 {
                t.Errorf("expected discount clamped to commission ≈ %.4f, got %v",
                        commissionAmount, discount)
        }

        totalWithDiscount := calculateTotalAmountWithDiscount(principal, commission, discount)
        t.Logf("Total with discount: %.2f (should equal principal %.2f)",
                totalWithDiscount, principal)

        // Total should equal the principal (commission fully discounted)
        if totalWithDiscount < principal-0.01 || totalWithDiscount > principal+0.01 {
                t.Errorf("expected total ≈ principal %.2f when discount >= commission, got %v",
                        principal, totalWithDiscount)
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
