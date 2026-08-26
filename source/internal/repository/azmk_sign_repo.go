package repository

import (
	"context"
	"fmt"
)

// PR #312: AZMK imza gözləmə axını üçün repo metodları.
//
// Axın:
//   1. Ekspert approve → AZMK /application/create uğurlu → UpdateAzmkCreated
//      (status=pending_signature, lw_application_id, azmk_created_at=GETUTCDATE())
//   2. Background worker hər N saniyədə GET /application/{id}/status yoxlayır:
//        - loanId gəlirsə → UpdateAzmkLoanID (audit)
//        - signed=true   → ClaimForDisburse → Disburse → MarkDisbursedFromDisbursing
//        - vaxt bitibsə  → ExpireAzmkSignIfTimeout (rejected)
//
// Cüt disburse qorunması (claim protokolu):
//   disburse sorğusu YALNIZ pending_signature → disbursing atomik keçidini
//   qazanan worker tərəfindən göndərilir. disbursing-dən auto-retry YOXDUR —
//   xəta/disbursing-də-qalma halında müraciət disburse_failed olur və manual
//   yoxlama tələb olunur (pul keçib-keçmədiyi AZMK-da yoxlanılır).

// UpdateAzmkCreated marks the application as waiting for the customer's
// signature after a successful AZMK application create.
//
// azmk_created_at UTC olaraq GETUTCDATE() ilə yazılır — imza limiti (3 saat)
// DB tərəfində hesablananda timezone qarışığı olmasın deyə.
func (r *ApplicationRepo) UpdateAzmkCreated(ctx context.Context, id int, lwApplicationID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET status = 'pending_signature',
		    lw_application_id = ?,
		    azmk_created_at = GETUTCDATE(),
		    updated_at = GETDATE()
		WHERE id = ?`, lwApplicationID, id)
	if err != nil {
		return fmt.Errorf("failed to update azmk created: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update azmk created: application %d not found", id)
	}
	return nil
}

// UpdateAzmkLoanID saves the AZMK loanId (məs. "HO0030210") returned by
// GET /application/{id}/status. Audit məqsədi daşıyır.
func (r *ApplicationRepo) UpdateAzmkLoanID(ctx context.Context, id int, loanID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET azmk_loan_id = ?,
		    updated_at = GETDATE()
		WHERE id = ?`, loanID, id)
	if err != nil {
		return fmt.Errorf("failed to update azmk loan id: %w", err)
	}
	return nil
}

// ClaimForDisburse atomically transitions pending_signature → disbursing.
// Returns true if this caller won the claim — YALNIZ bu halda AZMK disburse
// sorğusu göndərilə bilər.
//
// PR #312: cüt disburse qorunması. AZMK tərəfindən təkrar disburse-i
// idempotent rədd etmir — ona görə sorğu bizim tərəfdən maksimum 1 dəfə
// çıxmalıdır. Guarded update: iki eyni vaxtlı cəhddən yalnız biri qazanır.
func (r *ApplicationRepo) ClaimForDisburse(ctx context.Context, id int) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET status = 'disbursing',
		    updated_at = GETDATE()
		WHERE id = ? AND status = 'pending_signature'`, id)
	if err != nil {
		return false, fmt.Errorf("failed to claim for disburse: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to read rows affected: %w", err)
	}
	return n > 0, nil
}

// MarkDisbursedFromDisbursing transitions disbursing → disbursed after a
// successful AZMK disburse call. Guarded: yalnız disbursing statusunda işləyir.
func (r *ApplicationRepo) MarkDisbursedFromDisbursing(ctx context.Context, id int) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET status = 'disbursed',
		    updated_at = GETDATE()
		WHERE id = ? AND status = 'disbursing'`, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark disbursed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to read rows affected: %w", err)
	}
	return n > 0, nil
}

// MarkDisburseFailed transitions disbursing → disburse_failed. Xəta mətni
// rejection_reason sahəsində saxlanılır (dashboard-da göstərilir).
//
// PR #312: avtomatik retry YOXDUR — disburse xətasının nəticəsi naməlum ola
// bilər (timeout = AZMK tərəfindən icra olunmuş ola bilər), ona görə təkrar
// sorğu göndərmək təhlükəlidir. Manual yoxlama lazımdır.
func (r *ApplicationRepo) MarkDisburseFailed(ctx context.Context, id int, reason string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET status = 'disburse_failed',
		    rejection_reason = ?,
		    updated_at = GETDATE()
		WHERE id = ? AND status = 'disbursing'`, reason, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark disburse failed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to read rows affected: %w", err)
	}
	return n > 0, nil
}

// ExpireAzmkSignIfTimeout atomically rejects the application if the customer
// did not sign the AZMK contract within timeoutSeconds. Returns true if the
// application was expired by this call.
//
// Şərt DB tərəfində yoxlanılır (azmk_created_at < NOW - timeout) — race yoxdur,
// timezone qarışığı yoxdur (hər iki tərəf UTC).
func (r *ApplicationRepo) ExpireAzmkSignIfTimeout(ctx context.Context, id int, timeoutSeconds int) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications
		SET status = 'rejected',
		    rejection_reason = ?,
		    updated_at = GETDATE()
		WHERE id = ? AND status = 'pending_signature'
		  AND azmk_created_at IS NOT NULL
		  AND DATEDIFF(SECOND, azmk_created_at, GETUTCDATE()) > ?`,
		"AZMK: müqavilə imza vaxtı bitdi", id, timeoutSeconds)
	if err != nil {
		return false, fmt.Errorf("failed to expire azmk sign: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to read rows affected: %w", err)
	}
	return n > 0, nil
}
