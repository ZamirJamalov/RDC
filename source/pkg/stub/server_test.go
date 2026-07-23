package stub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helper: issue a GET request to a handler and return the decoded JSON body.
func doGet(t *testing.T, handler http.HandlerFunc, url string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	var body map[string]any
	if w.Body.Len() > 0 {
		_ = json.NewDecoder(w.Body).Decode(&body)
	}
	return w.Code, body
}

// helper: issue a POST request to a handler and return the decoded JSON body.
func doPost(t *testing.T, handler http.HandlerFunc, url string, payload string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	var body map[string]any
	if w.Body.Len() > 0 {
		_ = json.NewDecoder(w.Body).Decode(&body)
	}
	return w.Code, body
}

// --- AKB Score tests ---

func TestStub_AkbScore_Default(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAkbScore, "/api/router/akb-score?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	ret, ok := body["return"].(map[string]any)
	if !ok {
		t.Fatalf("missing 'return' object")
	}
	if ret["point"].(float64) != 650 {
		t.Errorf("point = %v, want 650", ret["point"])
	}
	if ret["response"] != "" {
		t.Errorf("response = %v, want empty", ret["response"])
	}
}

func TestStub_AkbScore_StopFactor(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAkbScore, "/api/router/akb-score?fin=PIN1&scenario=stop_factor")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	ret := body["return"].(map[string]any)
	if ret["point"].(float64) != 1 {
		t.Errorf("point = %v, want 1", ret["point"])
	}
	if ret["response"] != "AB" {
		t.Errorf("response = %v, want 'AB'", ret["response"])
	}
}

func TestStub_AkbScore_LowScore(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleAkbScore, "/api/router/akb-score?fin=PIN1&scenario=low_score")
	ret := body["return"].(map[string]any)
	if ret["point"].(float64) != 150 {
		t.Errorf("point = %v, want 150", ret["point"])
	}
}

func TestStub_AkbScore_Error(t *testing.T) {
	s := New(0)
	code, _ := doGet(t, s.handleAkbScore, "/api/router/akb-score?fin=PIN1&scenario=error")
	if code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", code)
	}
}

func TestStub_AkbScore_MissingFin(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAkbScore, "/api/router/akb-score")
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
	if body["error"] == nil {
		t.Errorf("missing error field")
	}
}

// --- PersonalInfo tests ---

func TestStub_PersonalInfo_Default(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handlePersonalInfo, "/api/router/personal-info?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["full_name"] == "" {
		t.Errorf("full_name empty")
	}
	if body["date_of_birth"] == "" {
		t.Errorf("date_of_birth empty")
	}
}

func TestStub_PersonalInfo_OldCustomer(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handlePersonalInfo, "/api/router/personal-info?fin=PIN1&scenario=old_customer")
	if body["date_of_birth"] != "1950-01-15" {
		t.Errorf("date_of_birth = %v, want 1950-01-15 (old customer)", body["date_of_birth"])
	}
}

// --- AZMK Blacklist tests ---

func TestStub_AzmkBlacklist_NotBlacklisted(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAzmkBlacklist, "/api/router/azmk-blacklist?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["is_blacklisted"].(bool) {
		t.Errorf("is_blacklisted = true, want false")
	}
}

func TestStub_AzmkBlacklist_Blacklisted(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleAzmkBlacklist, "/api/router/azmk-blacklist?fin=PIN1&scenario=blacklisted")
	if !body["is_blacklisted"].(bool) {
		t.Errorf("is_blacklisted = false, want true")
	}
}

// --- AKB History tests ---

func TestStub_AkbHistory_Empty(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAkbHistory, "/api/router/akb-history?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	liabilities, ok := body["liabilities"].([]any)
	if !ok {
		t.Fatalf("missing liabilities array")
	}
	if len(liabilities) != 0 {
		t.Errorf("liabilities len = %d, want 0 (clean customer)", len(liabilities))
	}
}

func TestStub_AkbHistory_Delay3M(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleAkbHistory, "/api/router/akb-history?fin=PIN1&scenario=delay_3m")
	liabilities := body["liabilities"].([]any)
	if len(liabilities) != 1 {
		t.Fatalf("liabilities len = %d, want 1", len(liabilities))
	}
	lib := liabilities[0].(map[string]any)
	history := lib["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1", len(history))
	}
	entry := history[0].(map[string]any)
	if entry["overdue_days"].(float64) != 25 {
		t.Errorf("overdue_days = %v, want 25", entry["overdue_days"])
	}
}

func TestStub_AkbHistory_HighMonthlyPayments(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleAkbHistory, "/api/router/akb-history?fin=PIN1&scenario=high_monthly_payments")
	liabilities := body["liabilities"].([]any)
	if len(liabilities) != 2 {
		t.Fatalf("liabilities len = %d, want 2", len(liabilities))
	}
}

// --- LW Loans tests ---

func TestStub_LwLoans_Default(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleLwLoans, "/api/lw/loans?pin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["loan_count"].(float64) != 0 {
		t.Errorf("loan_count = %v, want 0", body["loan_count"])
	}
}

func TestStub_LwLoans_ActiveLoan(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleLwLoans, "/api/lw/loans?pin=PIN1&scenario=active_loan")
	loans := body["loans"].([]any)
	if len(loans) != 1 {
		t.Fatalf("loans len = %d, want 1", len(loans))
	}
	loan := loans[0].(map[string]any)
	if loan["status"] != "active" {
		t.Errorf("status = %v, want 'active'", loan["status"])
	}
}

func TestStub_LwLoans_Trusted(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleLwLoans, "/api/lw/loans?pin=PIN1&scenario=trusted")
	loans := body["loans"].([]any)
	if len(loans) != 2 {
		t.Fatalf("loans len = %d, want 2 (trusted setup)", len(loans))
	}
}

// --- LW ApproveLoan tests ---

func TestStub_LwApprove_Default(t *testing.T) {
	s := New(0)
	code, body := doPost(t, s.handleLwApprove, "/api/lw/loans/approve", `{"application_id": 42}`)
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["contract_status"] != "signed" {
		t.Errorf("contract_status = %v, want 'signed'", body["contract_status"])
	}
	if body["transfer_status"] != "completed" {
		t.Errorf("transfer_status = %v, want 'completed'", body["transfer_status"])
	}
}

func TestStub_LwApprove_ContractFailed(t *testing.T) {
	s := New(0)
	_, body := doPost(t, s.handleLwApprove, "/api/lw/loans/approve?scenario=contract_failed", `{"application_id": 42}`)
	if body["contract_status"] != "failed" {
		t.Errorf("contract_status = %v, want 'failed'", body["contract_status"])
	}
}

func TestStub_LwApprove_MethodNotAllowed(t *testing.T) {
	s := New(0)
	req := httptest.NewRequest(http.MethodGet, "/api/lw/loans/approve", nil)
	w := httptest.NewRecorder()
	s.handleLwApprove(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- LW Blacklist tests ---

func TestStub_LwBlacklist_Default(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleLwBlacklist, "/api/lw/blacklist?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["is_blacklisted"].(bool) {
		t.Errorf("is_blacklisted = true, want false")
	}
}

func TestStub_LwBlacklist_Blacklisted(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleLwBlacklist, "/api/lw/blacklist?fin=PIN1&scenario=blacklisted")
	if !body["is_blacklisted"].(bool) {
		t.Errorf("is_blacklisted = false, want true")
	}
}

// --- ASAN Finance tests ---

func TestStub_AsanFinance_Default(t *testing.T) {
	s := New(0)
	code, body := doGet(t, s.handleAsanFinance, "/api/router/asan-finance?fin=PIN1")
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["official_income"].(float64) != 500 {
		t.Errorf("official_income = %v, want 500", body["official_income"])
	}
}

func TestStub_AsanFinance_HighIncome(t *testing.T) {
	s := New(0)
	_, body := doGet(t, s.handleAsanFinance, "/api/router/asan-finance?fin=PIN1&scenario=high_income")
	if body["official_income"].(float64) != 1500 {
		t.Errorf("official_income = %v, want 1500", body["official_income"])
	}
}

// --- PortFromString helper ---

func TestPortFromString(t *testing.T) {
	tests := []struct {
		input    string
		fallback int
		want     int
	}{
		{"8090", 8000, 8090},
		{"", 8000, 8000},
		{"abc", 8000, 8000},
		{"0", 8000, 8000},
		{"70000", 8000, 8000},
		{"-1", 8000, 8000},
	}
	for _, tc := range tests {
		got := PortFromString(tc.input, tc.fallback)
		if got != tc.want {
			t.Errorf("PortFromString(%q, %d) = %d, want %d", tc.input, tc.fallback, got, tc.want)
		}
	}
}
