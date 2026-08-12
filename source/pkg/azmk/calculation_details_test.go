package azmk

import (
	"encoding/json"
	"strings"
	"testing"
)

// PR #175: calculation_details JSON strukturunu yoxlayan testlər.
//
// Bu testlər təsdiqləyir ki:
// 1. Detail metodları valid JSON qaytarır
// 2. JSON struct-ları düzgün sahələri daşıyır
// 3. creditStatus="007" filtrinin aktiv kreditlərə tətbiqi düzgündür
// 4. Hesablama düsturu (sum, max) düzgün aparılır

// buildTestCreditHistory test üçün nümunə kredit tarixçəsi qurur.
//
//   - Liability #1677: aktiv (007), cari gecikmə 10 gün, history-də 70 gün cəmi gecikmə, 10 ay ödəniş → ratio 7.00
//   - Liability #1544: bağlı (001), cari gecikmə 0 gün, history-də 1 gün cəmi gecikmə, 14 ay ödəniş → ratio 0.07
//   - Liability #7662: aktiv (007), cari gecikmə 0 gün, aylıq ödəniş 143.33 AZN
func buildTestCreditHistory() *CreditHistory {
	return &CreditHistory{
		Inquiry: &InquiryResult{
			Liabilities: &Liabilities{
				Liability: []Liability{
					{
						ID:                   "1677",
						BankName:             "Kapital Bank",
						CreditStatus:         "007",
						DaysInterestOverdue:  10,
						DaysMainSumOverdue:   5,
						MonthlyPaymentAmount: 164.61,
						History: &History{
							HistoryItem: []HistoryItem{
								{OverdueDays: 7, ReportingPeriod: "01x2026"},
								{OverdueDays: 8, ReportingPeriod: "02x2026"},
								{OverdueDays: 9, ReportingPeriod: "03x2026"},
								{OverdueDays: 6, ReportingPeriod: "04x2026"},
								{OverdueDays: 10, ReportingPeriod: "05x2026"},
								{OverdueDays: 5, ReportingPeriod: "06x2026"},
								{OverdueDays: 7, ReportingPeriod: "07x2026"},
								{OverdueDays: 8, ReportingPeriod: "08x2026"},
								{OverdueDays: 5, ReportingPeriod: "09x2026"},
								{OverdueDays: 5, ReportingPeriod: "10x2026"},
							},
						},
					},
					{
						ID:                   "1544",
						BankName:             "ABB",
						CreditStatus:         "001",
						DaysInterestOverdue:  0,
						DaysMainSumOverdue:   0,
						MonthlyPaymentAmount: 100.00,
						History: &History{
							HistoryItem: []HistoryItem{
								{OverdueDays: 1, ReportingPeriod: "01x2026"},
								{OverdueDays: 0, ReportingPeriod: "02x2026"},
								{OverdueDays: 0, ReportingPeriod: "03x2026"},
								{OverdueDays: 0, ReportingPeriod: "04x2026"},
								{OverdueDays: 0, ReportingPeriod: "05x2026"},
								{OverdueDays: 0, ReportingPeriod: "06x2026"},
								{OverdueDays: 0, ReportingPeriod: "07x2026"},
								{OverdueDays: 0, ReportingPeriod: "08x2026"},
								{OverdueDays: 0, ReportingPeriod: "09x2026"},
								{OverdueDays: 0, ReportingPeriod: "10x2026"},
								{OverdueDays: 0, ReportingPeriod: "11x2025"},
								{OverdueDays: 0, ReportingPeriod: "12x2025"},
								{OverdueDays: 0, ReportingPeriod: "01x2026"},
								{OverdueDays: 0, ReportingPeriod: "02x2026"},
							},
						},
					},
					{
						ID:                   "7662",
						BankName:             "PASHA Bank",
						CreditStatus:         "007",
						DaysInterestOverdue:  0,
						DaysMainSumOverdue:   0,
						MonthlyPaymentAmount: 143.33,
						History: &History{
							HistoryItem: []HistoryItem{
								{OverdueDays: 0, ReportingPeriod: "01x2026"},
								{OverdueDays: 0, ReportingPeriod: "02x2026"},
							},
						},
					},
				},
			},
		},
	}
}

// TestMaxDelayRatioDetail_JSON verifies that MaxDelayRatioDetail returns valid JSON
// with the expected structure and calculation values.
func TestMaxDelayRatioDetail_JSON(t *testing.T) {
	ch := buildTestCreditHistory()
	out := ch.MaxDelayRatioDetail()
	if out == "" {
		t.Fatal("expected non-empty JSON, got empty string")
	}

	var detail DelayRatioDetailJSON
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\nraw: %s", err, out)
	}

	if detail.Calculation != "max_delay_ratio" {
		t.Errorf("calculation = %q, want %q", detail.Calculation, "max_delay_ratio")
	}
	if detail.Formula == "" {
		t.Error("formula should not be empty")
	}

	// Liability #1677: 70 delay / 10 months = 7.00
	// Liability #1544: 1 delay / 14 months = 0.07 (connected but still in list)
	// Liability #7662: 0 delay / 2 months = 0.00
	// Max ratio = 7.00
	if len(detail.Liabilities) != 3 {
		t.Errorf("liabilities count = %d, want 3 (all with history)", len(detail.Liabilities))
	}
	if detail.MaxRatio != 7.00 {
		t.Errorf("max_ratio = %.2f, want 7.00", detail.MaxRatio)
	}

	// Verify first liability values
	first := detail.Liabilities[0]
	if first.ID != "1677" {
		t.Errorf("first liability id = %q, want 1677", first.ID)
	}
	if first.TotalDelayDays != 70 {
		t.Errorf("first liability total_delay_days = %d, want 70", first.TotalDelayDays)
	}
	if first.PaymentMonths != 10 {
		t.Errorf("first liability payment_months = %d, want 10", first.PaymentMonths)
	}
	if first.DelayRatio != 7.00 {
		t.Errorf("first liability delay_ratio = %.2f, want 7.00", first.DelayRatio)
	}
	if first.BankName != "Kapital Bank" {
		t.Errorf("first liability bank_name = %q, want 'Kapital Bank'", first.BankName)
	}
	if first.CreditStatus != "007" {
		t.Errorf("first liability credit_status = %q, want 007", first.CreditStatus)
	}
}

// TestMaxCurrentDelayDetail_JSON verifies that MaxCurrentDelayDetail returns valid JSON
// and filters to only creditStatus=007 liabilities.
func TestMaxCurrentDelayDetail_JSON(t *testing.T) {
	ch := buildTestCreditHistory()
	out := ch.MaxCurrentDelayDetail()
	if out == "" {
		t.Fatal("expected non-empty JSON, got empty string")
	}

	var detail CurrentDelayDetailJSON
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\nraw: %s", err, out)
	}

	if detail.Calculation != "max_current_delay" {
		t.Errorf("calculation = %q, want %q", detail.Calculation, "max_current_delay")
	}
	if detail.CreditStatusFilter != "007" {
		t.Errorf("credit_status_filter = %q, want 007", detail.CreditStatusFilter)
	}
	if detail.Formula == "" {
		t.Error("formula should not be empty")
	}

	// Only creditStatus=007 liabilities should be in the list (#1677 and #7662)
	if len(detail.Liabilities) != 2 {
		t.Errorf("liabilities count = %d, want 2 (only creditStatus=007)", len(detail.Liabilities))
	}

	// Max delay = 10 (from #1677)
	if detail.MaxDelayDays != 10 {
		t.Errorf("max_delay_days = %d, want 10", detail.MaxDelayDays)
	}

	// Verify the connected liability (#1544) is NOT in the list
	for _, l := range detail.Liabilities {
		if l.CreditStatus != "007" {
			t.Errorf("liability %s has credit_status %q, want only 007", l.ID, l.CreditStatus)
		}
		if l.ID == "1544" {
			t.Error("liability 1544 (creditStatus=001) should NOT be in active delay list")
		}
	}

	// Verify first entry has current_delay_days = max(daysInterestOverdue, daysMainSumOverdue) = max(10,5) = 10
	first := detail.Liabilities[0]
	if first.ID != "1677" {
		t.Errorf("first liability id = %q, want 1677", first.ID)
	}
	if first.DaysInterestOverdue != 10 {
		t.Errorf("first liability days_interest_overdue = %d, want 10", first.DaysInterestOverdue)
	}
	if first.DaysMainSumOverdue != 5 {
		t.Errorf("first liability days_main_sum_overdue = %d, want 5", first.DaysMainSumOverdue)
	}
	if first.CurrentDelayDays != 10 {
		t.Errorf("first liability current_delay_days = %d, want 10 (max of 10, 5)", first.CurrentDelayDays)
	}
}

// TestTotalActiveMonthlyPaymentsDetail_JSON verifies that TotalActiveMonthlyPaymentsDetail
// returns valid JSON and sums only creditStatus=007 monthly payments.
func TestTotalActiveMonthlyPaymentsDetail_JSON(t *testing.T) {
	ch := buildTestCreditHistory()
	out := ch.TotalActiveMonthlyPaymentsDetail()
	if out == "" {
		t.Fatal("expected non-empty JSON, got empty string")
	}

	var detail MonthlyPaymentsDetailJSON
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\nraw: %s", err, out)
	}

	if detail.Calculation != "total_active_monthly_payments" {
		t.Errorf("calculation = %q, want %q", detail.Calculation, "total_active_monthly_payments")
	}
	if detail.CreditStatusFilter != "007" {
		t.Errorf("credit_status_filter = %q, want 007", detail.CreditStatusFilter)
	}
	if detail.Formula == "" {
		t.Error("formula should not be empty")
	}

	// Active credits: #1677 (164.61) + #7662 (143.33) = 307.94
	// #1544 (100.00) is NOT active (status=001)
	if len(detail.Liabilities) != 2 {
		t.Errorf("liabilities count = %d, want 2 (only creditStatus=007)", len(detail.Liabilities))
	}
	if detail.ActiveCount != 2 {
		t.Errorf("active_count = %d, want 2", detail.ActiveCount)
	}

	expectedTotal := 164.61 + 143.33
	if !floatApproxEqual(detail.Total, expectedTotal, 0.01) {
		t.Errorf("total = %.2f, want %.2f", detail.Total, expectedTotal)
	}

	// Verify the connected liability is NOT in the list
	for _, l := range detail.Liabilities {
		if l.CreditStatus != "007" {
			t.Errorf("liability %s has credit_status %q, want only 007", l.ID, l.CreditStatus)
		}
		if l.ID == "1544" {
			t.Error("liability 1544 (creditStatus=001) should NOT be in monthly payments list")
		}
	}
}

// TestEmptyCreditHistory_Details verifies that detail methods return empty strings
// when there is no inquiry data.
func TestEmptyCreditHistory_Details(t *testing.T) {
	empty := &CreditHistory{}
	if out := empty.MaxDelayRatioDetail(); out != "" {
		t.Errorf("MaxDelayRatioDetail on empty history = %q, want empty", out)
	}
	if out := empty.MaxCurrentDelayDetail(); out != "" {
		t.Errorf("MaxCurrentDelayDetail on empty history = %q, want empty", out)
	}
	if out := empty.TotalActiveMonthlyPaymentsDetail(); out != "" {
		t.Errorf("TotalActiveMonthlyPaymentsDetail on empty history = %q, want empty", out)
	}
}

// TestNoActiveCredits_StillReturnsJSON verifies that when there are no active credits
// (status=007), the monthly payments and current delay details still return valid JSON
// (with empty liabilities array and zero totals) rather than empty string.
// This is important for forensic analysis — the JSON structure documents that no
// active credits were found, instead of returning an opaque empty string.
func TestNoActiveCredits_StillReturnsJSON(t *testing.T) {
	ch := &CreditHistory{
		Inquiry: &InquiryResult{
			Liabilities: &Liabilities{
				Liability: []Liability{
					{
						ID:           "1544",
						CreditStatus: "001", // closed
						History: &History{
							HistoryItem: []HistoryItem{
								{OverdueDays: 1, ReportingPeriod: "01x2026"},
							},
						},
					},
				},
			},
		},
	}

	// MaxCurrentDelayDetail: no active credits → still returns JSON with empty array
	out := ch.MaxCurrentDelayDetail()
	if out == "" {
		t.Fatal("MaxCurrentDelayDetail should return JSON even with no active credits")
	}
	var cdDetail CurrentDelayDetailJSON
	if err := json.Unmarshal([]byte(out), &cdDetail); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(cdDetail.Liabilities) != 0 {
		t.Errorf("expected 0 liabilities, got %d", len(cdDetail.Liabilities))
	}
	if cdDetail.MaxDelayDays != 0 {
		t.Errorf("expected max_delay_days=0, got %d", cdDetail.MaxDelayDays)
	}

	// TotalActiveMonthlyPaymentsDetail: no active credits → still returns JSON
	out = ch.TotalActiveMonthlyPaymentsDetail()
	if out == "" {
		t.Fatal("TotalActiveMonthlyPaymentsDetail should return JSON even with no active credits")
	}
	var mpDetail MonthlyPaymentsDetailJSON
	if err := json.Unmarshal([]byte(out), &mpDetail); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(mpDetail.Liabilities) != 0 {
		t.Errorf("expected 0 liabilities, got %d", len(mpDetail.Liabilities))
	}
	if mpDetail.Total != 0 {
		t.Errorf("expected total=0, got %.2f", mpDetail.Total)
	}
	if mpDetail.ActiveCount != 0 {
		t.Errorf("expected active_count=0, got %d", mpDetail.ActiveCount)
	}
}

// TestJSON_OutputContainsExpectedKeys verifies the JSON contains all expected top-level keys.
// This is a structural smoke test — if a developer renames a struct field without updating
// the json tag, this test will catch it.
func TestJSON_OutputContainsExpectedKeys(t *testing.T) {
	ch := buildTestCreditHistory()

	// MaxDelayRatioDetail keys
	out := ch.MaxDelayRatioDetail()
	for _, key := range []string{"calculation", "max_ratio", "formula", "liabilities"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("MaxDelayRatioDetail JSON missing key %q\nraw: %s", key, out)
		}
	}
	for _, key := range []string{"id", "bank_name", "credit_status", "total_delay_days", "payment_months", "delay_ratio"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("MaxDelayRatioDetail JSON missing liability key %q\nraw: %s", key, out)
		}
	}

	// MaxCurrentDelayDetail keys
	out = ch.MaxCurrentDelayDetail()
	for _, key := range []string{"calculation", "max_delay_days", "credit_status_filter", "formula", "liabilities"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("MaxCurrentDelayDetail JSON missing key %q\nraw: %s", key, out)
		}
	}
	for _, key := range []string{"id", "days_interest_overdue", "days_main_sum_overdue", "current_delay_days"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("MaxCurrentDelayDetail JSON missing liability key %q\nraw: %s", key, out)
		}
	}

	// TotalActiveMonthlyPaymentsDetail keys
	out = ch.TotalActiveMonthlyPaymentsDetail()
	for _, key := range []string{"calculation", "total", "active_count", "credit_status_filter", "formula", "liabilities"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("TotalActiveMonthlyPaymentsDetail JSON missing key %q\nraw: %s", key, out)
		}
	}
	for _, key := range []string{"id", "monthly_payment_amount"} {
		if !strings.Contains(out, "\""+key+"\"") {
			t.Errorf("TotalActiveMonthlyPaymentsDetail JSON missing liability key %q\nraw: %s", key, out)
		}
	}
}

// floatApproxEqual compares two floats with a tolerance — needed because
// floating-point sums can have tiny rounding errors.
func floatApproxEqual(a, b, tol float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tol
}
