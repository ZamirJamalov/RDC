package service

import (
        "context"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
)

// mockApplicationStore is a test-only implementation of ApplicationStore.
// Each field controls the return value of the corresponding method, allowing
// tests to inject specific scenarios (success, error, not-found, etc.)
// without touching a real database.
//
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
        rate    float64
        rateErr error

        // CountApprovedAtLevel
        approvedCount    int
        approvedCountErr error

        // GetLevelRanges
        levelRanges    []repository.LevelRange
        levelRangesErr error

        // SaveCreditLevelHistory
        saveHistoryErr error

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

func (m *mockApplicationStore) UpdateApplicationStatus(_ context.Context, id int, status string) error {
        m.statusUpdates = append(m.statusUpdates, mockStatusUpdate{ID: id, Status: status})
        return m.updateStatusErr
}

func (m *mockApplicationStore) UpdateApplicationDecision(_ context.Context, id int,
        status, creditLevel, rejectionReason string, approvedAmount, approvedRate float64) error {
        m.decisionUpdates = append(m.decisionUpdates, mockDecisionUpdate{
                ID:              id,
                Status:          status,
                CreditLevel:     creditLevel,
                RejectionReason: rejectionReason,
                ApprovedAmount:  approvedAmount,
                ApprovedRate:    approvedRate,
        })
        // Simulate the DB UPDATE by mutating the stored application, so subsequent
        // GetApplicationByID calls return the updated state (matching real DB behavior).
        if app, ok := m.appByID[id]; ok {
                app.Status = status
                app.CreditLevel = creditLevel
                app.RejectionReason = rejectionReason
                app.ApprovedAmount = approvedAmount
                app.ApprovedRate = approvedRate
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

func (m *mockApplicationStore) GetCreditLevelRate(_ context.Context, _ string, _ float64, _ int, _ int) (float64, error) {
        return m.rate, m.rateErr
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

// errNotFound is the sentinel error returned by mockApplicationStore.GetApplicationByID
// when no fixture exists for the requested ID. It mimics
// repository.ApplicationRepo's "application with id X not found" error.
var errNotFound = errNotFoundSentinel{}

type errNotFoundSentinel struct{}

func (errNotFoundSentinel) Error() string { return "application not found (mock)" }

// newMockStore returns a mockApplicationStore with sensible defaults:
// no errors, empty results, rate=0, approvedCount=0. Tests should override
// the specific fields they need for their scenario.
func newMockStore() *mockApplicationStore {
        return &mockApplicationStore{
                appByID: make(map[int]*model.LoanApplication),
        }
}
