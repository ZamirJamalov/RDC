package service

import (
	"context"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// DiscountCodeStore is the interface used by ApplicationService to talk to the
// discount code repository. The concrete *repository.DiscountCodeRepo satisfies
// this interface structurally (Go duck typing).
//
// PR #94: foundation layer for referral/discount code functionality.
// The actual service logic (generate, validate, apply discount) will be added
// in PR #95. This interface is exposed now so the repo can be wired into
// ApplicationService and tested.
type DiscountCodeStore interface {
	// Create inserts a new discount code record and sets the ID on the struct.
	// Called when a loan application is approved (PR #95 will wire this in).
	Create(ctx context.Context, c *model.DiscountCode) error

	// GetByCode retrieves a discount code by its code string (case-sensitive).
	// Returns sql.ErrNoRows (wrapped) if not found.
	GetByCode(ctx context.Context, code string) (*model.DiscountCode, error)

	// MarkUsed marks a discount code as 'used' by a specific application.
	// Sets status='used', used_by_application_id, used_at=GETDATE().
	// Called atomically inside the approval transaction (PR #95).
	MarkUsed(ctx context.Context, codeID, applicationID int) error

	// ExistsByCode returns true if a discount code with the given code string
	// already exists. Used during code generation to avoid collisions.
	ExistsByCode(ctx context.Context, code string) (bool, error)

	// GetByOwnerCustomerID returns all discount codes owned by a customer.
	// Future use: customer dashboard "my codes" view.
	GetByOwnerCustomerID(ctx context.Context, customerID int) ([]*model.DiscountCode, error)

	// --- Tx-aware variants (used by approval transaction in PR #95) ---

	// CreateTx is the tx-aware variant of Create.
	CreateTx(ctx context.Context, runner repository.TxRunner, c *model.DiscountCode) error

	// MarkUsedTx is the tx-aware variant of MarkUsed.
	MarkUsedTx(ctx context.Context, runner repository.TxRunner, codeID, applicationID int) error

	// GetByCodeTx is the tx-aware variant of GetByCode (for re-validation inside tx).
	GetByCodeTx(ctx context.Context, runner repository.TxRunner, code string) (*model.DiscountCode, error)
}
