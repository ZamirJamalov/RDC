package repository

import (
	"context"
	"fmt"

	"rdc-source/internal/model"
)

// UpdateApplicationDetails fills in the remaining fields after the expert
// completes the application (customer name, amount, term, card, contacts, etc).
// This is used by the CompleteApplication flow and the new CustomerConfirmApplication
// flow (PR #58).
//
// PR #94: also persists discount_code if the customer entered one in apply.html.
// discount_amount is set later (on approval) by UpdateApplicationDiscount.
func (r *ApplicationRepo) UpdateApplicationDetails(ctx context.Context, id int, app *model.LoanApplication) error {
	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications
                SET customer_full_name = ?,
                    amount = ?,
                    term_months = ?,
                    loan_purpose = ?,
                    akb_score = ?,
                    contact1_phone = ?,
                    contact2_phone = ?,
                    contact3_phone = ?,
                    contact1_relation = ?,
                    contact2_relation = ?,
                    contact3_relation = ?,
                    actual_address = ?,
                    card_number = ?,
                    customer_confirmed_at = ?,
                    card_ownership_confirmed = ?,
                    discount_code = ?,
                    kyc_id = ?,
                    partner_id = ?,
                    card_id = ?,
                    lw_application_id = ?,
                    status = ?,
                    credit_level = ?,
                    approved_rate = ?,
                    total_amount = ?,
                    updated_at = GETDATE()
                WHERE id = ?`,
		app.CustomerFullName,
		app.Amount,
		app.TermMonths,
		app.LoanPurpose,
		app.AkbScore,
		app.Contact1Phone,
		app.Contact2Phone,
		app.Contact3Phone,
		app.Contact1Relation,
		app.Contact2Relation,
		app.Contact3Relation,
		app.ActualAddress,
		app.CardNumber,
		nullableString(app.CustomerConfirmedAt),
		app.CardOwnershipConfirmed,
		nullableString(app.DiscountCode),
		nullableString(app.KycID),
		nullableString(app.PartnerID),
		nullableString(app.CardID),
		nullableString(app.LwApplicationID),
		app.Status,
		app.CreditLevel,
		app.ApprovedRate,
		app.TotalAmount,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update application details: %w", err)
	}
	return nil
}

// UpdateAkbScore updates only the akb_score field.
// PR #228: OTP verify-də AZMK-dən gələn AKB score-u saxlayır.
func (r *ApplicationRepo) UpdateAkbScore(ctx context.Context, id int, akbScore int) error {
	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications SET akb_score = ?, updated_at = GETDATE() WHERE id = ?`,
		akbScore, id)
	if err != nil {
		return fmt.Errorf("failed to update akb_score: %w", err)
	}
	return nil
}

// UpdateCustomerFullName updates only the customer_full_name field.
// PR #243: early cutoff mərhələsində AZMK GetPersonalInfo-dən gələn adı saxlayır —
// customer-confirm və video mərhələlərində eyni servisə ikinci sorğu göndərilməsin.
func (r *ApplicationRepo) UpdateCustomerFullName(ctx context.Context, id int, fullName string) error {
	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications SET customer_full_name = ?, updated_at = GETDATE() WHERE id = ?`,
		fullName, id)
	if err != nil {
		return fmt.Errorf("failed to update customer_full_name: %w", err)
	}
	return nil
}

// UpdateActualAddress updates only the actual_address field.
// PR #245: ekspert tərəfindən redaktə edilən faktiki ünvan DB-də saxlanılır.
func (r *ApplicationRepo) UpdateActualAddress(ctx context.Context, id int, address string) error {
	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications SET actual_address = ?, updated_at = GETDATE() WHERE id = ?`,
		address, id)
	if err != nil {
		return fmt.Errorf("failed to update actual_address: %w", err)
	}
	return nil
}

// UpdateActualAddressAudit sets the actual_address_updated_by fields.
// PR #249: faktiki ünvanın kim tərəfindən, nə vaxt update edildiyini saxlayır.
// UpdateContactsAudit (PR #148) pattern.
func (r *ApplicationRepo) UpdateActualAddressAudit(ctx context.Context, id int, userID int, username string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET actual_address_updated_by_user_id = ?,
			actual_address_updated_by_username = ?,
			actual_address_updated_at = GETDATE(),
			updated_at = GETDATE()
		WHERE id = ?`,
		userID, username, id)
	if err != nil {
		return fmt.Errorf("failed to update actual_address audit: %w", err)
	}
	return nil
}

// UpdateRegistrationAddress updates only the registration_address field.
// PR #245: AZMK GetPersonalInfo-dən gələn qeydiyyat ünvanı saxlanılır.
func (r *ApplicationRepo) UpdateRegistrationAddress(ctx context.Context, id int, address string) error {
	_, err := r.db.ExecContext(ctx, `
                UPDATE loan_applications SET registration_address = ?, updated_at = GETDATE() WHERE id = ?`,
		address, id)
	if err != nil {
		return fmt.Errorf("failed to update registration_address: %w", err)
	}
	return nil
}

// nullableString returns nil when s is empty (so the DB column stays NULL),
// otherwise returns s. Used for customer_confirmed_at which should be NULL
// until the customer confirms.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
