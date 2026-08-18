package service

import (
	"testing"
	"time"

	"rdc-source/pkg/mygov"
)

// --- PR #237: checkEmploymentTenureFromEmployeeInfo tests ---
//
// The EMPLOYMENT_TENURE cutoff is based on the real MLSA GetEmployeeInfoByPin
// response: Active[].Contract.SignDate (imza tarixi) → today, 30-day months,
// threshold 6 months.

// helper: build an EmployeeInfoResponse with one Active record.
func empInfo(signDate string, beginDate string) *mygov.EmployeeInfoResponse {
	rec := mygov.EmploymentRecord{
		Employer: &mygov.EmployerInfo{Voen: "1701618531", Name: "TEST ŞİRKƏT MMC"},
		Employee: &mygov.EmployeeInfo{
			WorkPlaceType: &mygov.LabelDescription{Label: "1", Description: "Əsas"},
			Position:      "Aparıcı mütəxəssis",
		},
		Contract: &mygov.ContractInfo{
			SignDate:  signDate,
			BeginDate: beginDate,
		},
	}
	return &mygov.EmployeeInfoResponse{
		Result: 1, RequestID: "1", Message: "SUCCESS",
		Data: &mygov.EmployeeInfoData{
			Status:   &mygov.EmployeeStatus{Name: "Successful", Code: 0},
			Response: &mygov.EmployeeRecords{Active: []mygov.EmploymentRecord{rec}},
		},
	}
}

// TestCheckEmploymentTenure_SignDate8Months_Pass verifies that a customer
// whose active contract was signed 8 months ago passes the tenure check.
func TestCheckEmploymentTenure_SignDate8Months_Pass(t *testing.T) {
	sign := time.Now().AddDate(0, -8, 0).Format("02.01.2006")

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign))
	if !passed {
		t.Errorf("passed = false, want true; reason = %q", reason)
	}
	if !contains(reason, "uyğundur") {
		t.Errorf("reason = %q, want 'uyğundur'", reason)
	}
}

// TestCheckEmploymentTenure_SignDate3Months_Fail verifies that a customer
// whose active contract was signed 3 months ago fails (< 6 months).
func TestCheckEmploymentTenure_SignDate3Months_Fail(t *testing.T) {
	sign := time.Now().AddDate(0, -3, 0).Format("02.01.2006")

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign))
	if passed {
		t.Errorf("passed = true, want false (3 months < 6)")
	}
	if !contains(reason, "EMPLOYMENT_TENURE") {
		t.Errorf("reason = %q, want EMPLOYMENT_TENURE mention", reason)
	}
}

// TestCheckEmploymentTenure_SignDateExactly6Months_Pass verifies the boundary:
// exactly 6 months should pass (>= 6).
func TestCheckEmploymentTenure_SignDateExactly6Months_Pass(t *testing.T) {
	sign := time.Now().AddDate(0, -6, 0).Format("02.01.2006")

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign))
	if !passed {
		t.Errorf("passed = false, want true (exactly 6 months)")
	}
}

// TestCheckEmploymentTenure_SignDate5Months_Fail verifies that 5 months
// (just under 6) fails.
func TestCheckEmploymentTenure_SignDate5Months_Fail(t *testing.T) {
	sign := time.Now().AddDate(0, -5, 0).Format("02.01.2006")

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign))
	if passed {
		t.Errorf("passed = true, want false (5 months < 6)")
	}
}

// TestCheckEmploymentTenure_EmptyActive_Fail verifies that an empty Active
// section (no workplace info) fails.
func TestCheckEmploymentTenure_EmptyActive_Fail(t *testing.T) {
	info := &mygov.EmployeeInfoResponse{
		Result: 1, RequestID: "1", Message: "SUCCESS",
		Data: &mygov.EmployeeInfoData{
			Status:   &mygov.EmployeeStatus{Name: "Successful", Code: 0},
			Response: &mygov.EmployeeRecords{},
		},
	}

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info)
	if passed {
		t.Errorf("passed = true, want false")
	}
	if !contains(reason, "Aktiv iş yeri tapılmadı") {
		t.Errorf("reason = %q, want 'Aktiv iş yeri tapılmadı'", reason)
	}
}

// TestCheckEmploymentTenure_NilResponse_Fail verifies the nil guards.
func TestCheckEmploymentTenure_NilResponse_Fail(t *testing.T) {
	cases := []*mygov.EmployeeInfoResponse{
		nil,
		{Result: 1, Data: nil},
		{Result: 1, Data: &mygov.EmployeeInfoData{Response: nil}},
	}
	for i, info := range cases {
		passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info)
		if passed {
			t.Errorf("case %d: passed = true, want false", i)
		}
		if !contains(reason, "tapılmadı") {
			t.Errorf("case %d: reason = %q, want 'tapılmadı'", i, reason)
		}
	}
}

// TestCheckEmploymentTenure_SignDateFallbackToBeginDate verifies that when
// SignDate is empty, BeginDate is used as the anchor.
func TestCheckEmploymentTenure_SignDateFallbackToBeginDate(t *testing.T) {
	begin := time.Now().AddDate(0, -8, 0).Format("02.01.2006")

	// SignDate empty → fallback to BeginDate (8 months ago → pass)
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo("", begin))
	if !passed {
		t.Errorf("passed = false, want true (BeginDate fallback); reason = %q", reason)
	}
	if !contains(reason, "başlama tarixi") {
		t.Errorf("reason = %q, want 'başlama tarixi' mention", reason)
	}
}

// TestCheckEmploymentTenure_NoDates_Fail verifies that a contract without
// SignDate and BeginDate fails with a clear message.
func TestCheckEmploymentTenure_NoDates_Fail(t *testing.T) {
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo("", ""))
	if passed {
		t.Errorf("passed = true, want false (no dates)")
	}
	if !contains(reason, "tarixi tapılmadı") {
		t.Errorf("reason = %q, want 'tarixi tapılmadı'", reason)
	}
}

// TestCheckEmploymentTenure_MainJobPreferred verifies that when multiple
// Active records exist, the main (Əsas) workplace is used for the check even
// if it appears later in the list.
func TestCheckEmploymentTenure_MainJobPreferred(t *testing.T) {
	long := time.Now().AddDate(0, -8, 0).Format("02.01.2006")
	short := time.Now().AddDate(0, -2, 0).Format("02.01.2006")

	// First entry: part-time (Label "2") signed 8 months ago.
	// Second entry: main job (Label "1" Əsas) signed 2 months ago → must FAIL,
	// proving the main job (not the first entry) drives the check.
	partTime := mygov.EmploymentRecord{
		Employer: &mygov.EmployerInfo{Name: "Part-time LLC"},
		Employee: &mygov.EmployeeInfo{
			WorkPlaceType: &mygov.LabelDescription{Label: "2", Description: "Əlavə"},
		},
		Contract: &mygov.ContractInfo{SignDate: long, BeginDate: long},
	}
	info := empInfo(short, short)
	info.Data.Response.Active = append(info.Data.Response.Active, partTime)

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info)
	if passed {
		t.Errorf("passed = true, want false (main job 2 months < 6)")
	}
	if !contains(reason, "TEST ŞİRKƏT MMC") {
		t.Errorf("reason = %q, want main-job employer name", reason)
	}
}

// TestCheckEmploymentTenure_InvalidDateFormat_Fail verifies that an
// unparseable date fails with a clear message instead of panicking.
func TestCheckEmploymentTenure_InvalidDateFormat_Fail(t *testing.T) {
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo("2026-07-01", ""))
	if passed {
		t.Errorf("passed = true, want false (invalid date format)")
	}
	if !contains(reason, "formatı düzgün deyil") {
		t.Errorf("reason = %q, want 'formatı düzgün deyil'", reason)
	}
}

// --- PR #242: months return value tests (cutoff_results.actual_value üçün) ---

// TestCheckEmploymentTenure_ReturnsMonths verifies that the computed tenure
// in months is returned alongside pass/fail (~8 for an 8-month-old contract).
func TestCheckEmploymentTenure_ReturnsMonths(t *testing.T) {
	sign := time.Now().AddDate(0, -8, 0).Format("02.01.2006")

	_, months, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign))
	if months < 7.5 || months > 8.5 {
		t.Errorf("months = %.1f, want ~8.0", months)
	}
}

// TestCheckEmploymentTenure_MonthsNegativeOnMissingData verifies months = -1
// when the tenure cannot be computed (nil response, no dates, bad format).
func TestCheckEmploymentTenure_MonthsNegativeOnMissingData(t *testing.T) {
	cases := []*mygov.EmployeeInfoResponse{
		nil,
		{Result: 1, Data: nil},
		empInfo("", ""),           // SignDate + BeginDate boş
		empInfo("2026-07-01", ""), // düzgün olmayan format
	}
	for i, info := range cases {
		_, months, _ := checkEmploymentTenureFromEmployeeInfo(info)
		if months >= 0 {
			t.Errorf("case %d: months = %.1f, want -1", i, months)
		}
	}
}
