package mygov

import (
        "context"
        "encoding/json"
        "strings"
        "testing"
        "time"
)

// jsonMarshal is a thin wrapper around encoding/json.Marshal to keep the test
// file self-contained.
func jsonMarshal(v any) ([]byte, error) {
        return json.Marshal(v)
}

// jsonUnmarshal is a thin wrapper around encoding/json.Unmarshal.
func jsonUnmarshal(data []byte, v any) error {
        return json.Unmarshal(data, v)
}

// contains checks if s contains substr (strings.Contains wrapper).
func contains(s, substr string) bool {
        return strings.Contains(s, substr)
}

// TestAuthorizedData_WorkHistory_JSON verifies that the extended AuthorizedData
// struct (with WorkHistory, DisabilityGroup, IsPensioner, PensionType) serializes
// and deserializes correctly — so the mygov_repo can store it as JSON and the
// credit engine can read it back.
func TestAuthorizedData_WorkHistory_JSON(t *testing.T) {
        now := time.Now().Truncate(time.Second)
        prevEnd := now.AddDate(0, -3, -10)
        prevStart := prevEnd.AddDate(0, -4, 0)

        original := &AuthorizedData{
                Fin:             "PIN1",
                FullName:        "Test Customer",
                OfficialIncome:  1500.0,
                EmployerName:    "ABC LLC",
                Address:         "Bakı",
                FetchedAt:       now,
                DisabilityGroup: 0,
                IsPensioner:     false,
                PensionType:     "",
                WorkHistory: []WorkPlace{
                        {
                                EmployerName: "ABC LLC",
                                StartDate:    now.AddDate(0, -8, 0),
                                EndDate:      nil, // currently employed
                                Position:     "Engineer",
                        },
                        {
                                EmployerName: "Previous Corp",
                                StartDate:    prevStart,
                                EndDate:      &prevEnd,
                                Position:     "Specialist",
                        },
                },
        }

        // Marshal to JSON
        data, err := jsonMarshal(original)
        if err != nil {
                t.Fatalf("marshal failed: %v", err)
        }

        // Unmarshal back
        var decoded AuthorizedData
        if err := jsonUnmarshal(data, &decoded); err != nil {
                t.Fatalf("unmarshal failed: %v", err)
        }

        // Verify fields
        if decoded.Fin != original.Fin {
                t.Errorf("Fin = %q, want %q", decoded.Fin, original.Fin)
        }
        if decoded.DisabilityGroup != original.DisabilityGroup {
                t.Errorf("DisabilityGroup = %d, want %d", decoded.DisabilityGroup, original.DisabilityGroup)
        }
        if decoded.IsPensioner != original.IsPensioner {
                t.Errorf("IsPensioner = %v, want %v", decoded.IsPensioner, original.IsPensioner)
        }
        if len(decoded.WorkHistory) != 2 {
                t.Fatalf("WorkHistory len = %d, want 2", len(decoded.WorkHistory))
        }
        if decoded.WorkHistory[0].EmployerName != "ABC LLC" {
                t.Errorf("WorkHistory[0].EmployerName = %q, want 'ABC LLC'", decoded.WorkHistory[0].EmployerName)
        }
        if decoded.WorkHistory[0].EndDate != nil {
                t.Errorf("WorkHistory[0].EndDate = %v, want nil (currently employed)", decoded.WorkHistory[0].EndDate)
        }
        if decoded.WorkHistory[1].EndDate == nil {
                t.Errorf("WorkHistory[1].EndDate = nil, want non-nil (previous job)")
        }
}

// TestAuthorizedData_DisabilityGroup1_Pensioner verifies the JSON round-trip
// for a 1st-group disability pensioner (auto-reject case).
func TestAuthorizedData_DisabilityGroup1_Pensioner(t *testing.T) {
        original := &AuthorizedData{
                Fin:             "PIN1",
                FullName:        "Disabled Pensioner",
                OfficialIncome:  300.0,
                IsPensioner:     true,
                PensionType:     "disability",
                DisabilityGroup: 1,
                WorkHistory:     []WorkPlace{},
                FetchedAt:       time.Now().Truncate(time.Second),
        }

        data, err := jsonMarshal(original)
        if err != nil {
                t.Fatalf("marshal failed: %v", err)
        }

        var decoded AuthorizedData
        if err := jsonUnmarshal(data, &decoded); err != nil {
                t.Fatalf("unmarshal failed: %v", err)
        }

        if decoded.DisabilityGroup != 1 {
                t.Errorf("DisabilityGroup = %d, want 1", decoded.DisabilityGroup)
        }
        if !decoded.IsPensioner {
                t.Errorf("IsPensioner = false, want true")
        }
        if decoded.PensionType != "disability" {
                t.Errorf("PensionType = %q, want 'disability'", decoded.PensionType)
        }
}

// TestMockProvider_ReturnsWorkHistory verifies the mock provider now returns
// a populated WorkHistory (PR #64 change).
func TestMockProvider_ReturnsWorkHistory(t *testing.T) {
        provider := NewMockProvider()
        data, err := provider.FetchAuthorizedData(context.Background(), "test-token")
        if err != nil {
                t.Fatalf("FetchAuthorizedData failed: %v", err)
        }
        if len(data.WorkHistory) == 0 {
                t.Fatalf("WorkHistory empty, want at least 1 entry (PR #64)")
        }
        if data.WorkHistory[0].EndDate != nil {
                t.Errorf("WorkHistory[0].EndDate = %v, want nil (current job)", data.WorkHistory[0].EndDate)
        }
        if data.DisabilityGroup != 0 {
                t.Errorf("DisabilityGroup = %d, want 0 (no disability)", data.DisabilityGroup)
        }
        if data.IsPensioner {
                t.Errorf("IsPensioner = true, want false")
        }
}

// --- PR #237: GetEmployeeInfoByPin mock tests ---

// TestMockProvider_GetEmployeeInfoByPin_Default verifies the default scenario:
// 1 active main-job record signed 8 months ago (passes the 6-month rule).
func TestMockProvider_GetEmployeeInfoByPin_Default(t *testing.T) {
        provider := NewMockProvider()
        info, err := provider.GetEmployeeInfoByPin(context.Background(), "1SBK08P")
        if err != nil {
                t.Fatalf("GetEmployeeInfoByPin failed: %v", err)
        }
        if info.Data == nil || info.Data.Response == nil {
                t.Fatalf("Data/Response nil")
        }
        if len(info.Data.Response.Active) == 0 {
                t.Fatalf("Active empty, want 1 record (default scenario)")
        }
        rec := info.Data.Response.Active[0]
        if !rec.IsMainJob() {
                t.Errorf("Active[0] is not the main job (WorkPlaceType.Label != '1')")
        }
        if rec.Contract == nil || rec.Contract.SignDate == "" {
                t.Fatalf("Contract.SignDate empty, want dd.mm.yyyy date")
        }
        signDate, err := time.Parse("02.01.2006", rec.Contract.SignDate)
        if err != nil {
                t.Fatalf("SignDate %q not in dd.mm.yyyy format: %v", rec.Contract.SignDate, err)
        }
        months := time.Since(signDate).Hours() / 24 / 30
        if months < 7 || months > 9 {
                t.Errorf("SignDate tenure = %.1f months, want ~8 months", months)
        }
}

// TestMockProvider_GetEmployeeInfoByPin_NOJOB verifies the NOJOB scenario:
// Active is empty (no workplace info → EMPLOYMENT_TENURE reject).
func TestMockProvider_GetEmployeeInfoByPin_NOJOB(t *testing.T) {
        provider := NewMockProvider()
        info, err := provider.GetEmployeeInfoByPin(context.Background(), "NOJOB001")
        if err != nil {
                t.Fatalf("GetEmployeeInfoByPin failed: %v", err)
        }
        if info.Data == nil || info.Data.Response == nil {
                t.Fatalf("Data/Response nil")
        }
        if len(info.Data.Response.Active) != 0 {
                t.Errorf("Active = %d records, want 0 (NOJOB scenario)", len(info.Data.Response.Active))
        }
}

// TestMockProvider_GetEmployeeInfoByPin_SHORT verifies the SHORT scenario:
// active record signed 3 months ago (fails the 6-month rule).
func TestMockProvider_GetEmployeeInfoByPin_SHORT(t *testing.T) {
        provider := NewMockProvider()
        info, err := provider.GetEmployeeInfoByPin(context.Background(), "SHORT01")
        if err != nil {
                t.Fatalf("GetEmployeeInfoByPin failed: %v", err)
        }
        if len(info.Data.Response.Active) != 1 {
                t.Fatalf("Active = %d records, want 1", len(info.Data.Response.Active))
        }
        signDate, err := time.Parse("02.01.2006", info.Data.Response.Active[0].Contract.SignDate)
        if err != nil {
                t.Fatalf("SignDate parse failed: %v", err)
        }
        months := time.Since(signDate).Hours() / 24 / 30
        if months < 2 || months > 4 {
                t.Errorf("SignDate tenure = %.1f months, want ~3 months", months)
        }
}

// TestWorkPlace_EndDate_Nil_Omitempty verifies that EndDate is omitted from
// JSON when nil (current job), so the wire format is clean.
func TestWorkPlace_EndDate_Nil_Omitempty(t *testing.T) {
        wp := WorkPlace{
                EmployerName: "Test Co",
                StartDate:    time.Now(),
                EndDate:      nil,
        }
        data, err := jsonMarshal(wp)
        if err != nil {
                t.Fatalf("marshal failed: %v", err)
        }
        str := string(data)
        if contains(str, "end_date") {
                t.Errorf("JSON contains 'end_date' when EndDate is nil; want omitempty. JSON: %s", str)
        }
}
