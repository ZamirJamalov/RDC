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

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign), 6)
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

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign), 6)
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

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign), 6)
	if !passed {
		t.Errorf("passed = false, want true (exactly 6 months)")
	}
}

// TestCheckEmploymentTenure_SignDate5Months_Fail verifies that 5 months
// (just under 6) fails.
func TestCheckEmploymentTenure_SignDate5Months_Fail(t *testing.T) {
	sign := time.Now().AddDate(0, -5, 0).Format("02.01.2006")

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign), 6)
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

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if passed {
		t.Errorf("passed = true, want false")
	}
	if !contains(reason, "Aktiv iş yeri yoxdur") {
		t.Errorf("reason = %q, want 'Aktiv iş yeri yoxdur' (PR #277 message)", reason)
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
		passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info, 6)
		if passed {
			t.Errorf("case %d: passed = true, want false", i)
		}
		if !contains(reason, "tapılmadı") {
			t.Errorf("case %d: reason = %q, want 'tapılmadı'", i, reason)
		}
	}
}

// TestCheckEmploymentTenure_BeginDateFallbackToSignDate — PR #255
// verifies that when BeginDate is empty, SignDate is used as the anchor.
func TestCheckEmploymentTenure_BeginDateFallbackToSignDate(t *testing.T) {
	sign := time.Now().AddDate(0, -8, 0).Format("02.01.2006")

	// BeginDate empty → fallback to SignDate (8 months ago → pass)
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, ""), 6)
	if !passed {
		t.Errorf("passed = false, want true (SignDate fallback); reason = %q", reason)
	}
	if !contains(reason, "imza tarixi") {
		t.Errorf("reason = %q, want 'imza tarixi' mention", reason)
	}
}

// TestCheckEmploymentTenure_NoDates_Fail verifies that a contract without
// SignDate and BeginDate fails with a clear message.
func TestCheckEmploymentTenure_NoDates_Fail(t *testing.T) {
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo("", ""), 6)
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

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info, 6)
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
	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo("2026-07-01", ""), 6)
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

	_, months, _ := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, sign), 6)
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
		_, months, _ := checkEmploymentTenureFromEmployeeInfo(info, 6)
		if months >= 0 {
			t.Errorf("case %d: months = %.1f, want -1", i, months)
		}
	}
}

// TestCheckEmploymentTenure_BeginDatePreferredOverSignDate — PR #255
// verifies that when both BeginDate and SignDate are present,
// BeginDate is used as the anchor (not SignDate).
func TestCheckEmploymentTenure_BeginDatePreferredOverSignDate(t *testing.T) {
	// BeginDate 8 ay əvvəl (pass), SignDate 2 ay əvvəl (fail)
	// PR #255: BeginDate istifadə olunmalı → pass
	begin := time.Now().AddDate(0, -8, 0).Format("02.01.2006")
	sign := time.Now().AddDate(0, -2, 0).Format("02.01.2006")

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(empInfo(sign, begin), 6)
	if !passed {
		t.Errorf("passed = false, want true (BeginDate should be used, not SignDate); reason = %q", reason)
	}
	if !contains(reason, "başlama tarixi") {
		t.Errorf("reason = %q, want 'başlama tarixi' mention (BeginDate used)", reason)
	}
}

// deactiveRec — PR #363 testləri üçün deaktiv iş yeri qeydi.
func deactiveRec(terminate, endDate string) mygov.EmploymentRecord {
	return mygov.EmploymentRecord{
		Employer: &mygov.EmployerInfo{Name: "ƏVVƏLKİ İŞ MMC"},
		Contract: &mygov.ContractInfo{TerminateDate: terminate, EndDate: endDate},
	}
}

// TestCheckEmploymentTenure_ContinuousJobChange_Pass — PR #363:
// aktiv staj 2 ay (< 6), amma əvvəlki iş 1 gün əvvəl bitib (kəsintisiz
// keçid) → PASS.
func TestCheckEmploymentTenure_ContinuousJobChange_Pass(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	term := begin.AddDate(0, 0, -1)
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{deactiveRec(term.Format("02.01.2006"), "")}

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if !passed {
		t.Errorf("passed = false, want true (seamless job change); reason = %q", reason)
	}
	if !contains(reason, "kəsintisiz") {
		t.Errorf("reason = %q, want 'kəsintisiz' mention", reason)
	}
}

// TestCheckEmploymentTenure_JobGapOver29Days_Fail — PR #363: aktiv staj
// 2 ay (< 6) və əvvəlki işlə fasilə 40 gün (> 29) → FAIL.
func TestCheckEmploymentTenure_JobGapOver29Days_Fail(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	term := begin.AddDate(0, 0, -40)
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{deactiveRec(term.Format("02.01.2006"), "")}

	passed, _, reason := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if passed {
		t.Errorf("passed = true, want false (40 days gap > 29)")
	}
	if !contains(reason, "fasilə") {
		t.Errorf("reason = %q, want 'fasilə' mention", reason)
	}
}

// TestCheckEmploymentTenure_GapExactly29Days_Pass — PR #363: fasilə tam
// 29 gün (sərhəd dəyəri, ≤ 29) → PASS.
func TestCheckEmploymentTenure_GapExactly29Days_Pass(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	term := begin.AddDate(0, 0, -29)
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{deactiveRec(term.Format("02.01.2006"), "")}

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if !passed {
		t.Error("passed = false, want true (29 days gap is the boundary, ≤ 29 passes)")
	}
}

// TestCheckEmploymentTenure_EndDateFallback_Pass — PR #363: deaktiv
// qeyddə TerminateDate yoxdursa EndDate istifadə olunur (5 gün fasilə → PASS).
func TestCheckEmploymentTenure_EndDateFallback_Pass(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	end := begin.AddDate(0, 0, -5)
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{deactiveRec("", end.Format("02.01.2006"))}

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if !passed {
		t.Error("passed = false, want true (EndDate fallback with 5-day gap)")
	}
}

// TestCheckEmploymentTenure_OverlapZeroGap_Pass — PR #363: əvvəlki işın
// bitməsi aktiv işin başlamasından SONRADIR (overlap) → fasilə 0 → PASS.
func TestCheckEmploymentTenure_OverlapZeroGap_Pass(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	term := begin.AddDate(0, 0, 10) // aktiv başlamadan 10 gün SONRA bitib
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{deactiveRec(term.Format("02.01.2006"), "")}

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if !passed {
		t.Error("passed = false, want true (overlapping contracts, no salary gap)")
	}
}

// TestCheckEmploymentTenure_LatestEndDateWins — PR #363: bir neçə deaktiv
// qeyddən ƏN SON tarix götürülür (eski qeyd 1 il əvvəl, yenisi 1 gün əvvəl
// bitib → PASS; əks halda fasilə 1 il olardı).
func TestCheckEmploymentTenure_LatestEndDateWins(t *testing.T) {
	begin := time.Now().AddDate(0, -2, 0)
	old := begin.AddDate(-1, 0, 0)
	recent := begin.AddDate(0, 0, -1)
	info := empInfo(begin.Format("02.01.2006"), begin.Format("02.01.2006"))
	info.Data.Response.Deactive = []mygov.EmploymentRecord{
		deactiveRec(old.Format("02.01.2006"), ""),
		deactiveRec(recent.Format("02.01.2006"), ""),
	}

	passed, _, _ := checkEmploymentTenureFromEmployeeInfo(info, 6)
	if !passed {
		t.Error("passed = false, want true (latest deactive end must be used, not the oldest)")
	}
}
