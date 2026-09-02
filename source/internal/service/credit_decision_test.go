package service

import "testing"

// PR #364: kredit məbləğində qəpik varsa yuxarı yuvarlaqlaşdırılır (ceil).
// total = principal × 100/(100−commission); nəticə tam manata yuvarlaqlaşır.
func TestCalculateTotalAmount_CeilQepik(t *testing.T) {
	cases := []struct {
		name       string
		principal  float64
		commission float64
		want       float64
	}{
		{"100 AZN 11% → 112.36 → ceil 113", 100, 11, 113},
		{"20 AZN 11% → 22.47 → ceil 23 (loqdakı real nümunə)", 20, 11, 23},
		{"500 AZN 11% → 561.80 → ceil 562", 500, 11, 562},
		{"300 AZN 15% → 352.94 → ceil 353", 300, 15, 353},
		{"521.80 əsaslı 11% → 586.29 → ceil 587", 521.80, 11, 587},
	}
	for _, tc := range cases {
		got := calculateTotalAmount(tc.principal, tc.commission)
		if got != tc.want {
			t.Errorf("%s: calculateTotalAmount(%v, %v) = %v, want %v",
				tc.name, tc.principal, tc.commission, got, tc.want)
		}
	}
}

// PR #364: komissiya sıfır/batsız yollar dəyişməz qalır — principal olduğu kimi.
func TestCalculateTotalAmount_NoCommission_Unchanged(t *testing.T) {
	if got := calculateTotalAmount(100.50, 0); got != 100.50 {
		t.Errorf("commission=0: got %v, want 100.50 (principal unchanged)", got)
	}
	if got := calculateTotalAmount(100.50, 100); got != 100.50 {
		t.Errorf("commission=100: got %v, want 100.50 (principal unchanged)", got)
	}
}
