package service

import (
	"testing"
	"time"

	"rdc-source/pkg/mygov"
)

// --- PR #65: checkEmploymentTenure tests ---

// TestCheckEmploymentTenure_CurrentJob6Months_Pass verifies that a customer
// with 8 months at their current job passes the tenure check.
func TestCheckEmploymentTenure_CurrentJob6Months_Pass(t *testing.T) {
	now := time.Now()
	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "ABC LLC",
			StartDate:    now.AddDate(0, -8, 0), // 8 months ago
			EndDate:      nil,                   // currently employed
		},
	}

	passed, reason := checkEmploymentTenure(workHistory)
	if !passed {
		t.Errorf("passed = false, want true; reason = %q", reason)
	}
	if !contains(reason, "uyğundur") {
		t.Errorf("reason = %q, want 'uyğundur'", reason)
	}
}

// TestCheckEmploymentTenure_CurrentJob3MonthsNoPrevious_Fail verifies that
// 3 months at current job with no previous job fails.
func TestCheckEmploymentTenure_CurrentJob3MonthsNoPrevious_Fail(t *testing.T) {
	now := time.Now()
	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "ABC LLC",
			StartDate:    now.AddDate(0, -3, 0), // 3 months ago
			EndDate:      nil,
		},
	}

	passed, reason := checkEmploymentTenure(workHistory)
	if passed {
		t.Errorf("passed = true, want false")
	}
	if !contains(reason, "imtina") {
		t.Errorf("reason = %q, want 'imtina'", reason)
	}
}

// TestCheckEmploymentTenure_Current3PlusPrevious4Gap10_Pass verifies the
// combined tenure rule: current 3 months + previous 4 months, gap 10 days
// (< 29) → combined 7 months → pass.
func TestCheckEmploymentTenure_Current3PlusPrevious4Gap10_Pass(t *testing.T) {
	now := time.Now()
	prevEnd := now.AddDate(0, -3, -10)   // 3 months 10 days ago
	prevStart := prevEnd.AddDate(0, -4, 0) // 4 months before that

	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "Current LLC",
			StartDate:    now.AddDate(0, -3, 0), // 3 months ago
			EndDate:      nil,
		},
		{
			EmployerName: "Previous Corp",
			StartDate:    prevStart,
			EndDate:      &prevEnd,
		},
	}

	passed, reason := checkEmploymentTenure(workHistory)
	if !passed {
		t.Errorf("passed = false, want true; reason = %q", reason)
	}
	if !contains(reason, "uyğundur") {
		t.Errorf("reason = %q, want 'uyğundur'", reason)
	}
}

// TestCheckEmploymentTenure_Current3PlusPrevious4Gap60_Fail verifies that
// a gap of 60 days (>= 29) causes the previous job to NOT be counted.
func TestCheckEmploymentTenure_Current3PlusPrevious4Gap60_Fail(t *testing.T) {
	now := time.Now()
	prevEnd := now.AddDate(0, -3, -60) // 3 months 60 days ago
	prevStart := prevEnd.AddDate(0, -4, 0)

	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "Current LLC",
			StartDate:    now.AddDate(0, -3, 0),
			EndDate:      nil,
		},
		{
			EmployerName: "Previous Corp",
			StartDate:    prevStart,
			EndDate:      &prevEnd,
		},
	}

	passed, reason := checkEmploymentTenure(workHistory)
	if passed {
		t.Errorf("passed = true, want false (gap >= 29 days)")
	}
	if !contains(reason, "imtina") {
		t.Errorf("reason = %q, want 'imtina'", reason)
	}
	if !contains(reason, "29") {
		t.Errorf("reason = %q, want '29' mentioned (gap threshold)", reason)
	}
}

// TestCheckEmploymentTenure_EmptyWorkHistory_Fail verifies that empty
// work history fails.
func TestCheckEmploymentTenure_EmptyWorkHistory_Fail(t *testing.T) {
	passed, reason := checkEmploymentTenure([]mygov.WorkPlace{})
	if passed {
		t.Errorf("passed = true, want false")
	}
	if !contains(reason, "boşdur") {
		t.Errorf("reason = %q, want 'boşdur'", reason)
	}
}

// TestCheckEmploymentTenure_CurrentJobExactly6Months_Pass verifies the
// boundary: exactly 6 months should pass (>= 6).
func TestCheckEmploymentTenure_CurrentJobExactly6Months_Pass(t *testing.T) {
	now := time.Now()
	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "ABC LLC",
			StartDate:    now.AddDate(0, -6, 0), // exactly 6 months ago
			EndDate:      nil,
		},
	}

	passed, _ := checkEmploymentTenure(workHistory)
	if !passed {
		t.Errorf("passed = false, want true (exactly 6 months should pass)")
	}
}

// TestCheckEmploymentTenure_CurrentJob5Months_Fail verifies that 5 months
// (just under 6) with no previous job fails.
func TestCheckEmploymentTenure_CurrentJob5Months_Fail(t *testing.T) {
	now := time.Now()
	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "ABC LLC",
			StartDate:    now.AddDate(0, -5, 0), // 5 months ago
			EndDate:      nil,
		},
	}

	passed, _ := checkEmploymentTenure(workHistory)
	if passed {
		t.Errorf("passed = true, want false (5 months < 6)")
	}
}

// TestCheckEmploymentTenure_CurrentJobEndDateSet_Fail verifies that if the
// first entry has EndDate set (not currently employed), the check fails.
func TestCheckEmploymentTenure_CurrentJobEndDateSet_Fail(t *testing.T) {
	now := time.Now()
	endDate := now.AddDate(0, -1, 0)
	workHistory := []mygov.WorkPlace{
		{
			EmployerName: "ABC LLC",
			StartDate:    now.AddDate(0, -12, 0),
			EndDate:      &endDate, // not currently employed
		},
	}

	passed, reason := checkEmploymentTenure(workHistory)
	if passed {
		t.Errorf("passed = true, want false (EndDate set on first entry)")
	}
	if !contains(reason, "cari") {
		t.Errorf("reason = %q, want 'cari' mention", reason)
	}
}
