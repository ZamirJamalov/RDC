package service

import (
	"context"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// ApplicationStore is the persistence interface used by CreditEngine and
// ApplicationService. The concrete implementation is *repository.ApplicationRepo,
// which satisfies this interface structurally — Go's duck typing means we don't
// need an explicit "implements" declaration.
//
// Defining the interface here (in the consumer package) follows Go idiom:
// "Accept interfaces, return structs." This keeps the service layer testable
// without depending on a real SQL Server connection — tests inject a mock
// implementation that returns canned data.
//
// All methods accept a context.Context for cancellation/timeout propagation,
// and return errors wrapped with a descriptive prefix.
type ApplicationStore interface {
	// --- Transaction control ---

	// WithTx wraps fn in a single DB transaction. If fn returns nil the tx is
	// committed; otherwise it is rolled back. The TxRunner passed to fn is
	// used by the *Tx methods below so they all share the same transaction.
	WithTx(ctx context.Context, fn func(repository.TxRunner) error) error

	// --- Non-tx methods (for backward compat & simple cases) ---

	// CreateApplication inserts a new loan application and sets the ID on the
	// struct. The application's Status field is the source of truth for the
	// initial status (callers should set it to model.StatusPending).
	CreateApplication(ctx context.Context, app *model.LoanApplication) error

	// GetApplicationByID fetches a loan application by its primary key.
	// Returns an error (wrapping sql.ErrNoRows) if not found.
	GetApplicationByID(ctx context.Context, id int) (*model.LoanApplication, error)

	// GetApplicationByPublicID fetches a loan application by its UUID public_id.
	// PR #191: xarici API və UI public_id UUID istifadə edir.
	GetApplicationByPublicID(ctx context.Context, publicID string) (*model.LoanApplication, error)

	// UpdateApplicationStatus updates only the status field of an application.
	// Used by the credit engine to transition pending → checking.
	UpdateApplicationStatus(ctx context.Context, id int, status string) error

	// UpdateApplicationDecision updates the decision-related fields after
	// credit engine processing or manual operator action.
	UpdateApplicationDecision(ctx context.Context, id int,
		status, creditLevel, rejectionReason string,
		approvedAmount, approvedRate, totalAmount float64) error

	// SaveCheckResult inserts a check result for an application.
	SaveCheckResult(ctx context.Context, appID int, check *model.ApplicationCheckResult) error

	// GetCheckResults retrieves all check results for an application ordered by ID.
	GetCheckResults(ctx context.Context, appID int) ([]model.ApplicationCheckResult, error)

	// GetRecentAkbScore returns the most recent AKB score for a customer from DB.
	// PR #229: GetOffer bunu çağırır — əgər frontend-dən akbScore=0 gəlirsə.
	GetRecentAkbScore(ctx context.Context, customerPIN string) int

	// GetRecentPendingApplication returns the most recent pending_customer application
	// for the given PIN + phone within the last N minutes.
	// PR #217: Idempotent InitApplication — təkrar müraciətdə eyni app-i reuse et.
	GetRecentPendingApplication(ctx context.Context, customerPIN, customerPhone string, withinMinutes int) (*model.LoanApplication, error)

	// HasPendingApplication checks if a customer already has an application
	// that is not yet finalized (pending / checking / pending_approval).
	// Returns (0, "", nil) if no such application exists.
	// PR #256: daysRemaining də qaytarır (blocked rejection halında).
	HasPendingApplication(ctx context.Context, customerPIN string) (int, string, int, error)

	// ListByStatus retrieves all applications with the given status, ordered
	// by oldest first. Used by the expert queue to list pending_approval apps.
	ListByStatus(ctx context.Context, status string) ([]model.LoanApplication, error)

	// GetCreditLevelRate looks up the applicable interest rate for a given
	// credit level, amount, term, and unlock phase.
	GetCreditLevelRate(ctx context.Context, level string, amount float64, termMonths int, unlockPhase int) (float64, error)

	// PR #109: GetCreditLevelInterestRate looks up the annual interest rate
	// (separate from commission rate) — used for discount calculation.
	GetCreditLevelInterestRate(ctx context.Context, level string, amount float64, termMonths int, unlockPhase int) (float64, error)

	// CountApprovedAtLevel counts how many loan applications a customer has
	// had approved at a specific credit level.
	CountApprovedAtLevel(ctx context.Context, customerPIN string, level string) (int, error)

	// GetLevelRanges returns all active rate configurations for a given credit
	// level and unlock phase. Used for building descriptive error messages.
	GetLevelRanges(ctx context.Context, level string, unlockPhase int) ([]repository.LevelRange, error)

	// SaveCreditLevelHistory records a credit level assignment for a customer.
	// Called whenever an application is approved (auto or manual).
	SaveCreditLevelHistory(ctx context.Context, customerPIN, toLevel string, appID int) error

	// GetCustomerCurrentLevel returns the customer's current credit level
	// based on their most recent LW-confirmed approved application.
	GetCustomerCurrentLevel(ctx context.Context, customerPIN string) (string, error)

	// UpdateApplicationDetails fills in the remaining fields after the expert
	// completes the application (used by CompleteApplication flow).
	UpdateApplicationDetails(ctx context.Context, id int, app *model.LoanApplication) error

	// UpdateAkbScore updates only the akb_score field. PR #228.
	UpdateAkbScore(ctx context.Context, id int, akbScore int) error

	// UpdateCustomerFullName updates only the customer_full_name field.
	// PR #243: early cutoff mərhələsində AZMK-dən gələn ad saxlanılır.
	UpdateCustomerFullName(ctx context.Context, id int, fullName string) error

	// UpdateActualAddress updates only the actual_address field.
	// PR #245: ekspert faktiki ünvanı redaktə edir.
	UpdateActualAddress(ctx context.Context, id int, address string) error
	// UpdateActualAddressAudit sets the actual_address_updated_by fields.
	// PR #249: faktiki ünvan dəyişikliyinin audit izi (PR #148 pattern).
	UpdateActualAddressAudit(ctx context.Context, id int, userID int, username string) error

	// UpdateRegistrationAddress updates only the registration_address field.
	// PR #245: AZMK GetPersonalInfo-dən gələn qeydiyyat ünvanı saxlanılır.
	UpdateRegistrationAddress(ctx context.Context, id int, address string) error

	// PR #95: discount code persistence.
	// UpdateApplicationDiscount sets discount_code + discount_amount on an
	// application. Used by customer-confirm (to store the entered code) and
	// approval (to store the computed discount amount).
	UpdateApplicationDiscount(ctx context.Context, id int, discountCode string, discountAmount *float64) error

	// PR #124: kontakt nömrələri və yoxlanma statusu (pending_approval-da da işləyir)
	UpdateContacts(ctx context.Context, id int, app *model.LoanApplication) error

	// PR #134: müraciət timer-ı saxla
	UpdateTimer(ctx context.Context, id int, seconds int) error

	// PR #142: hansı dashboard istifadəçisi tərəfindən təsdiq/redd edilib
	UpdateProcessedBy(ctx context.Context, id int, userID int, username string) error

	// PR #148: audit fields — hansı ekspert hansı əməliyyatı etdi
	UpdateContactsAudit(ctx context.Context, id int, userID int, username string) error
	UpdateTimerAudit(ctx context.Context, id int, userID int, username string) error
	UpdateMyGovAudit(ctx context.Context, id int, userID int, username string) error

	// --- Tx-aware variants (used by ProcessApplication for atomicity) ---

	UpdateApplicationStatusTx(ctx context.Context, runner repository.TxRunner, id int, status string) error
	UpdateApplicationDecisionTx(ctx context.Context, runner repository.TxRunner, id int,
		status, creditLevel, rejectionReason string,
		approvedAmount, approvedRate, totalAmount float64) error
	SaveCheckResultTx(ctx context.Context, runner repository.TxRunner, appID int, check *model.ApplicationCheckResult) error
	SaveCreditLevelHistoryTx(ctx context.Context, runner repository.TxRunner, customerPIN, toLevel string, appID int) error
}
