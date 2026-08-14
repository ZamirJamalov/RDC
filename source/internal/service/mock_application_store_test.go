package service

import (
        "context"
        "database/sql"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
)
// mockApplicationStore is a test-only implementation of ApplicationStore.
// Each field controls the return value of the corresponding method, allowing
// tests to inject specific scenarios (success, error, not-found, etc.)
// without touching a real database.//
// All "recording" fields (e.g. CreatedApps, StatusUpdates) capture the calls
// made during the test so assertions can verify the service interacted with
// the store as expected.
//
// This mock is intentionally minimal — it does not validate inputs (beyond
// what the service layer already does) and does not maintain any state
// between calls. For more sophisticated scenarios (e.g. simulate "row not
// found only on the second call"), tests can mutate the fields between calls.
type mockApplicationStore struct {
        // --- Configurable return values ---

        // GetApplicationByID
        appByID    map[int]*model.LoanApplication
        appByIDErr error // returned for every call if set (overrides map lookup)

        // CreateApplication
        createErr error

        // UpdateApplicationStatus
        updateStatusErr error

        // UpdateApplicationDecision
        updateDecisionErr error

        // SaveCheckResult
        saveCheckErr error

        // GetCheckResults
        checkResults    []model.ApplicationCheckResult
        checkResultsErr error

        // HasPendingApplication
        pendingAppID  int
        pendingStatus string
        pendingErr    error

        // GetCreditLevelRate
        commission    float64
        rateErr error

        // PR #109: GetCreditLevelInterestRate
        annualInterestRate float64

        // CountApprovedAtLevel
        approvedCount    int
        approvedCountErr error

        // GetLevelRanges
        levelRanges    []repository.LevelRange
        levelRangesErr error

        // SaveCreditLevelHistory
        saveHistoryErr error

        // GetCustomerCurrentLevel — configurable return for testing
        currentLevel string

        // WithTx (T-1.3) — when non-nil, WithTx returns this error without
        // calling fn. Use to simulate transaction failures.
        withTxErr error

        // --- Recording of calls made during the test ---

        createdApps     []model.LoanApplication
        statusUpdates   []mockStatusUpdate
        decisionUpdates []mockDecisionUpdate
        checkSaves      []model.ApplicationCheckResult
        historySaves    []mockHistorySave
}

type mockStatusUpdate struct {
        ID     int
        Status string
}

type mockDecisionUpdate struct {
        ID              int
        Status          string
        CreditLevel     string
        RejectionReason string
        ApprovedAmount  float64
        ApprovedRate    float64
        TotalAmount     float64
}

type mockHistorySave struct {
        CustomerPIN string
        ToLevel     string
        AppID       int
}

// --- ApplicationStore method implementations ---

func (m *mockApplicationStore) CreateApplication(_ context.Context, app *model.LoanApplication) error {
        m.createdApps = append(m.createdApps, *app)
        if m.createErr != nil {
                return m.createErr
        }
        // Simulate auto-increment: assign the next ID
        app.ID = len(m.createdApps)
        return nil
}

func (m *mockApplicationStore) GetApplicationByID(_ context.Context, id int) (*model.LoanApplication, error) {
        if m.appByIDErr != nil {
                return nil, m.appByIDErr
        }
        if app, ok := m.appByID[id]; ok {
                // Return a copy so the test can't mutate the stored fixture
                copied := *app
                return &copied, nil
        }
        return nil, errNotFound
}

// GetApplicationByPublicID — PR #191: mock implementation (lookup by public_id).
func (m *mockApplicationStore) GetApplicationByPublicID(_ context.Context, publicID string) (*model.LoanApplication, error) {
        if m.appByIDErr != nil {
                return nil, m.appByIDErr
        }
        for _, app := range m.appByID {
                if app.PublicID == publicID {
                        copied := *app
                        return &copied, nil
                }
        }
        return nil, errNotFound
}

func (m *mockApplicationStore) UpdateApplicationStatus(_ context.Context, id int, status string) error {
        m.statusUpdates = append(m.statusUpdates, mockStatusUpdate{ID: id, Status: status})
        return m.updateStatusErr
}

func (m *mockApplicationStore) UpdateApplicationDecision(_ context.Context, id int,
        status, creditLevel, rejectionReason string, approvedAmount, approvedRate, totalAmount float64) error {
        m.decisionUpdates = append(m.decisionUpdates, mockDecisionUpdate{
                ID:              id,
                Status:          status,
                CreditLevel:     creditLevel,
                RejectionReason: rejectionReason,
                ApprovedAmount:  approvedAmount,
                ApprovedRate:    approvedRate,
                TotalAmount:     totalAmount,
        })
        // Simulate the DB UPDATE by mutating the stored application, so subsequent
        // GetApplicationByID calls return the updated state (matching real DB behavior).
        if app, ok := m.appByID[id]; ok {
                app.Status = status
                app.CreditLevel = creditLevel
                app.RejectionReason = rejectionReason
                app.ApprovedAmount = approvedAmount
                app.ApprovedRate = approvedRate
                app.TotalAmount = totalAmount
        }
        return m.updateDecisionErr
}

func (m *mockApplicationStore) SaveCheckResult(_ context.Context, _ int, check *model.ApplicationCheckResult) error {
        m.checkSaves = append(m.checkSaves, *check)
        return m.saveCheckErr
}

func (m *mockApplicationStore) GetCheckResults(_ context.Context, _ int) ([]model.ApplicationCheckResult, error) {
        if m.checkResultsErr != nil {
                return nil, m.checkResultsErr
        }
        // Return a copy
        out := make([]model.ApplicationCheckResult, len(m.checkResults))
        copy(out, m.checkResults)
        return out, nil
}

func (m *mockApplicationStore) HasPendingApplication(_ context.Context, _ string) (int, string, error) {
        return m.pendingAppID, m.pendingStatus, m.pendingErr
}

// ListByStatus returns all stored applications (by ID) that match the status.
// Used by the expert queue endpoint tests.
func (m *mockApplicationStore) ListByStatus(_ context.Context, status string) ([]model.LoanApplication, error) {
        var result []model.LoanApplication
        for _, app := range m.appByID {
                if app.Status == status {
                        copied := *app
                        result = append(result, copied)
                }
        }
        return result, nil
}

func (m *mockApplicationStore) GetCreditLevelRate(_ context.Context, _ string, _ float64, _ int, _ int) (float64, error) {
        return m.commission, m.rateErr
}

// PR #109: GetCreditLevelInterestRate mock — returns a default 55% annual interest
// (matches the 'new' level seed data). Tests that need a different value can
// override the field.
func (m *mockApplicationStore) GetCreditLevelInterestRate(_ context.Context, _ string, _ float64, _ int, _ int) (float64, error) {
        if m.annualInterestRate == 0 {
                return 55.0, nil // default 'new' level
        }
        return m.annualInterestRate, nil
}

func (m *mockApplicationStore) CountApprovedAtLevel(_ context.Context, _ string, _ string) (int, error) {
        return m.approvedCount, m.approvedCountErr
}

func (m *mockApplicationStore) GetLevelRanges(_ context.Context, _ string, _ int) ([]repository.LevelRange, error) {
        if m.levelRangesErr != nil {
                return nil, m.levelRangesErr
        }
        // Return a copy
        out := make([]repository.LevelRange, len(m.levelRanges))
        copy(out, m.levelRanges)
        return out, nil
}

func (m *mockApplicationStore) SaveCreditLevelHistory(_ context.Context, customerPIN, toLevel string, appID int) error {
        m.historySaves = append(m.historySaves, mockHistorySave{
                CustomerPIN: customerPIN,
                ToLevel:     toLevel,
                AppID:       appID,
        })
        return m.saveHistoryErr
}

// GetCustomerCurrentLevel returns a configurable current level for testing.
func (m *mockApplicationStore) GetCustomerCurrentLevel(_ context.Context, _ string) (string, error) {
        return m.currentLevel, nil
}

// UpdateApplicationDetails updates the mock store's application record.
func (m *mockApplicationStore) UpdateApplicationDetails(_ context.Context, id int, app *model.LoanApplication) error {
        if existing, ok := m.appByID[id]; ok {
                existing.CustomerFullName = app.CustomerFullName
                existing.Amount = app.Amount
                existing.TermMonths = app.TermMonths
                existing.LoanPurpose = app.LoanPurpose
                existing.AkbScore = app.AkbScore
                existing.Contact1Phone = app.Contact1Phone
                existing.Contact2Phone = app.Contact2Phone
                existing.Contact3Phone = app.Contact3Phone
                existing.ActualAddress = app.ActualAddress
                existing.CardNumber = app.CardNumber
                existing.Status = app.Status
                existing.CustomerConfirmedAt = app.CustomerConfirmedAt
                existing.CardOwnershipConfirmed = app.CardOwnershipConfirmed
                existing.RejectionReason = app.RejectionReason
                existing.Contact1Relation = app.Contact1Relation
                existing.Contact2Relation = app.Contact2Relation
                existing.Contact3Relation = app.Contact3Relation
                // PR #95: discount_code
                existing.DiscountCode = app.DiscountCode
                // PR #116: AZMK Online Lending fields
                existing.KycID = app.KycID
                existing.PartnerID = app.PartnerID
                existing.CardID = app.CardID
                existing.LwApplicationID = app.LwApplicationID
        }
        return nil
}

// UpdateApplicationDiscount — PR #95: mock implementation. Mutates the stored
// application to reflect discount_code / discount_amount changes.
func (m *mockApplicationStore) UpdateApplicationDiscount(_ context.Context, id int, discountCode string, discountAmount *float64) error {
        if existing, ok := m.appByID[id]; ok {
                existing.DiscountCode = discountCode
                existing.DiscountAmount = discountAmount
        }
        return nil
}

// UpdateContacts — PR #124: mock implementation.
func (m *mockApplicationStore) UpdateContacts(_ context.Context, id int, app *model.LoanApplication) error {
        if existing, ok := m.appByID[id]; ok {
                existing.Contact1Phone = app.Contact1Phone
                existing.Contact2Phone = app.Contact2Phone
                existing.Contact3Phone = app.Contact3Phone
                existing.Contact1Relation = app.Contact1Relation
                existing.Contact2Relation = app.Contact2Relation
                existing.Contact3Relation = app.Contact3Relation
                existing.Contact1Name = app.Contact1Name
                existing.Contact2Name = app.Contact2Name
                existing.Contact3Name = app.Contact3Name
                existing.Contact1Verified = app.Contact1Verified
                existing.Contact2Verified = app.Contact2Verified
                existing.Contact3Verified = app.Contact3Verified
        }
        return nil
}

// UpdateTimer — PR #134: mock implementation.
func (m *mockApplicationStore) UpdateTimer(_ context.Context, id int, seconds int) error {
        if existing, ok := m.appByID[id]; ok {
                existing.TimerSeconds = seconds
        }
        return nil
}

// PR #142: UpdateProcessedBy — mock implementation (no-op, just records)
func (m *mockApplicationStore) UpdateProcessedBy(_ context.Context, id int, userID int, username string) error {
        if existing, ok := m.appByID[id]; ok {
                uid := userID
                existing.ProcessedByUserID = &uid
                existing.ProcessedByUsername = username
        }
        return nil
}

// PR #148: audit mock implementations
func (m *mockApplicationStore) UpdateContactsAudit(_ context.Context, id int, userID int, username string) error {
        if existing, ok := m.appByID[id]; ok {
                uid := userID
                existing.ContactsUpdatedByUserID = &uid
                existing.ContactsUpdatedByUsername = username
        }
        return nil
}

func (m *mockApplicationStore) UpdateTimerAudit(_ context.Context, id int, userID int, username string) error {
        if existing, ok := m.appByID[id]; ok {
                uid := userID
                existing.TimerUpdatedByUserID = &uid
                existing.TimerUpdatedByUsername = username
        }
        return nil
}

func (m *mockApplicationStore) UpdateMyGovAudit(_ context.Context, id int, userID int, username string) error {
        if existing, ok := m.appByID[id]; ok {
                uid := userID
                existing.MyGovCheckedByUserID = &uid
                existing.MyGovCheckedByUsername = username
        }
        return nil
}

// --- Tx-aware variants (T-1.3) ---
//
// These accept a repository.TxRunner but ignore it — the mock doesn't actually
// run inside a real transaction. The runner is only relevant for the real DB
// implementation. The recording fields (decisionUpdates, checkSaves,
// historySaves) are shared with the non-tx variants so assertions work the
// same regardless of which variant the code under test calls.

func (m *mockApplicationStore) WithTx(_ context.Context, fn func(repository.TxRunner) error) error {
        // For the mock, we pass a nil runner — the *Tx methods below never use it.
        // If a test wants to simulate a tx failure, set withTxErr.
        if m.withTxErr != nil {
                return m.withTxErr
        }
        return fn(nilTxRunner{})
}

// nilTxRunner is a no-op TxRunner used by the mock's WithTx. The *Tx methods
// in the mock ignore the runner entirely, so this satisfies the interface
// without doing any real work.
type nilTxRunner struct{}

func (nilTxRunner) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
        return nil, nil
}

func (nilTxRunner) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
        return nil, nil
}

func (nilTxRunner) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
        return nil
}

func (m *mockApplicationStore) UpdateApplicationStatusTx(_ context.Context, _ repository.TxRunner, id int, status string) error {
        m.statusUpdates = append(m.statusUpdates, mockStatusUpdate{ID: id, Status: status})
        return m.updateStatusErr
}

func (m *mockApplicationStore) UpdateApplicationDecisionTx(_ context.Context, _ repository.TxRunner, id int,
        status, creditLevel, rejectionReason string, approvedAmount, approvedRate, totalAmount float64) error {
        m.decisionUpdates = append(m.decisionUpdates, mockDecisionUpdate{
                ID:              id,
                Status:          status,
                CreditLevel:     creditLevel,
                RejectionReason: rejectionReason,
                ApprovedAmount:  approvedAmount,
                ApprovedRate:    approvedRate,
                TotalAmount:     totalAmount,
        })
        // Simulate the DB UPDATE by mutating the stored application, so subsequent
        // GetApplicationByID calls return the updated state (matching real DB behavior).
        if app, ok := m.appByID[id]; ok {
                app.Status = status
                app.CreditLevel = creditLevel
                app.RejectionReason = rejectionReason
                app.ApprovedAmount = approvedAmount
                app.ApprovedRate = approvedRate
                app.TotalAmount = totalAmount
        }
        return m.updateDecisionErr
}

func (m *mockApplicationStore) SaveCheckResultTx(_ context.Context, _ repository.TxRunner, _ int, check *model.ApplicationCheckResult) error {
        m.checkSaves = append(m.checkSaves, *check)
        return m.saveCheckErr
}

func (m *mockApplicationStore) SaveCreditLevelHistoryTx(_ context.Context, _ repository.TxRunner, customerPIN, toLevel string, appID int) error {
        m.historySaves = append(m.historySaves, mockHistorySave{
                CustomerPIN: customerPIN,
                ToLevel:     toLevel,
                AppID:       appID,
        })
        return m.saveHistoryErr
}

// errNotFound is the sentinel error returned by mockApplicationStore.GetApplicationByID
// when no fixture exists for the requested ID. It mimics
// repository.ApplicationRepo's "application with id X not found" error.
var errNotFound = errNotFoundSentinel{}

type errNotFoundSentinel struct{}

func (errNotFoundSentinel) Error() string { return "application not found (mock)" }

// newMockStore returns a mockApplicationStore with sensible defaults:
// no errors, empty results, commission=0, approvedCount=0. Tests should override
// the specific fields they need for their scenario.
func newMockStore() *mockApplicationStore {
        return &mockApplicationStore{
                appByID: make(map[int]*model.LoanApplication),
        }
}
