package azmk

import (
	"encoding/json"
	"testing"
)

// PR #446: kumulativ history overdueDays-lərin TOPLANMASI bug-ının reqressiya testi.
//
// Real hadisə: müştərinin liability-sində history item-lər kumulativ gecikmə
// günlərini daşıyır (159, 108, 77, 49, 18, 0x4). Köhnə məntiq bunları TOPLAYARAQ
// 411 gün çıxarırdı → ratio = 411/9 = 45.67 → yanlış şişirdilmiş DELAY_RATIO_HIGH.
// Faktiki maksimal gecikmə teglərdədir: daysInterestOverdue = 159,
// daysMainSumOverdue = 159 → max = 159 → ratio = 159/9 = 17.67.
func TestDelayRatio_NoCumulativeSum(t *testing.T) {
	lib := Liability{
		ID:                  "1248462074",
		CreditStatus:        "001", // bağlı kredit
		DaysInterestOverdue: 159,
		DaysMainSumOverdue:  159,
		History: &History{
			HistoryItem: []HistoryItem{
				{OverdueDays: 159, ReportingPeriod: "05x2025"},
				{OverdueDays: 108, ReportingPeriod: "04x2025"},
				{OverdueDays: 77, ReportingPeriod: "03x2025"},
				{OverdueDays: 49, ReportingPeriod: "02x2025"},
				{OverdueDays: 18, ReportingPeriod: "01x2025"},
				{OverdueDays: 0, ReportingPeriod: "12x2024"},
				{OverdueDays: 0, ReportingPeriod: "11x2024"},
				{OverdueDays: 0, ReportingPeriod: "10x2024"},
				{OverdueDays: 0, ReportingPeriod: "09x2024"},
			},
		},
	}

	// Teglərdən böyüyük götürülməlidir (159), history cəmi (411) YOX.
	if got := lib.CurrentDelayDays(); got != 159 {
		t.Errorf("CurrentDelayDays() = %d, want 159 (max of tag values)", got)
	}

	// Ratio = 159 / 9 ay = 17.67 (köhnə yanlış: 411 / 9 = 45.67)
	ratio := lib.DelayRatio()
	if ratio < 17.66 || ratio > 17.68 {
		t.Errorf("DelayRatio() = %.2f, want 17.67 (159/9, NOT 45.67 = 411/9)", ratio)
	}

	// CreditHistory səviyyəsində MaxDelayRatio da eyni dəyəri qaytarmalıdır.
	ch := &CreditHistory{Inquiry: &InquiryResult{Liabilities: &Liabilities{Liability: []Liability{lib}}}}
	if mr := ch.MaxDelayRatio(); mr < 17.66 || mr > 17.68 {
		t.Errorf("MaxDelayRatio() = %.2f, want 17.67", mr)
	}

	// Detail JSON-da da yeni sahələr düzgün əks olunmalıdır.
	out := ch.MaxDelayRatioDetail()
	if out == "" {
		t.Fatal("expected non-empty detail JSON")
	}
	var detail DelayRatioDetailJSON
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("detail JSON parse error: %v\nraw: %s", err, out)
	}
	if len(detail.Liabilities) != 1 {
		t.Fatalf("liabilities count = %d, want 1", len(detail.Liabilities))
	}
	d := detail.Liabilities[0]
	if d.CurrentDelayDays != 159 {
		t.Errorf("detail current_delay_days = %d, want 159", d.CurrentDelayDays)
	}
	if d.DaysInterestOverdue != 159 || d.DaysMainSumOverdue != 159 {
		t.Errorf("detail tags = %d/%d, want 159/159", d.DaysInterestOverdue, d.DaysMainSumOverdue)
	}
}

// PR #446: teglər fərqli olanda böyüyük götürülür (daysInterest 3 < daysMain 7 → 7).
func TestDelayRatio_TakesLargerTag(t *testing.T) {
	lib := Liability{
		DaysInterestOverdue: 3,
		DaysMainSumOverdue:  7,
		History: &History{
			HistoryItem: []HistoryItem{
				{OverdueDays: 2, ReportingPeriod: "01x2026"},
				{OverdueDays: 0, ReportingPeriod: "02x2026"},
			},
		},
	}
	if got := lib.CurrentDelayDays(); got != 7 {
		t.Errorf("CurrentDelayDays() = %d, want 7 (larger tag wins)", got)
	}
	if r := lib.DelayRatio(); r != 3.5 {
		t.Errorf("DelayRatio() = %.2f, want 3.50 (7/2)", r)
	}
}

// PR #446: history boş olsa da teglərdə gecikmə varsa ratio 0 qalmamalıdırmı?
// Xeyr — PaymentMonths = 0 olduqda bölünmə mümkün deyil, ratio 0 qayıdır
// (mövcud davranış qorunur — detail-də current_delay_days yenə görünür).
func TestDelayRatio_NoHistory_ZeroRatio(t *testing.T) {
	lib := Liability{
		DaysInterestOverdue: 100,
		DaysMainSumOverdue:  100,
	}
	if r := lib.DelayRatio(); r != 0 {
		t.Errorf("DelayRatio() = %.2f, want 0 (no history months)", r)
	}
}
