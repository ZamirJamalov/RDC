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

        // UpdateApplicationStatus updates only the status field of an application.
        // Used by the credit engine to transition pending → checking.
        UpdateApplicationStatus(ctx context.Context, id int, status string) error

        // UpdateApplicationDecision updates the decision-related fields after
        // credit engine processing or manual operator action.
        UpdateApplicationDecision(ctx context.Context, id int,
                status, creditLevel, rejectionReason string,
                approvedAmount, approvedRate float64) error

        // SaveCheckResult inserts a check result for an application.
        SaveCheckResult(ctx context.Context, appID int, check *model.ApplicationCheckResult) error

        // GetCheckResults retrieves all check results for an application ordered by ID.
        GetCheckResults(ctx context.Context, appID int) ([]model.ApplicationCheckResult, error)

        // HasPendingApplication checks if a customer already has an application
        // that is not yet finalized (pending / checking / pending_approval).
        // Returns (0, "", nil) if no such application exists.
        HasPendingApplication(ctx context.Context, customerPIN string) (int, string, error)

        // ListByStatus retrieves all applications with the given status, ordered
        // by oldest first. Used by the expert queue to list pending_approval apps.
        ListByStatus(ctx context.Context, status string) ([]model.LoanApplication, error)

        // GetCreditLevelRate looks up the applicable interest rate for a given
        // credit level, amount, term, and unlock phase.
        GetCreditLevelRate(ctx context.Context, level string, amount float64, termMonths int, unlockPhase int) (float64, error)

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

        // --- Tx-aware variants (used by ProcessApplication for atomicity) ---

        UpdateApplicationStatusTx(ctx context.Context, runner repository.TxRunner, id int, status string) error
        UpdateApplicationDecisionTx(ctx context.Context, runner repository.TxRunner, id int,
                status, creditLevel, rejectionReason string,
                approvedAmount, approvedRate float64) error
        SaveCheckResultTx(ctx context.Context, runner repository.TxRunner, appID int, check *model.ApplicationCheckResult) error
        SaveCreditLevelHistoryTx(ctx context.Context, runner repository.TxRunner, customerPIN, toLevel string, appID int) error
}

