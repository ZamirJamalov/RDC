package service

import (
	"context"
	"errors"
	"testing"
)

// fakeCacheLookup — PR #375: MyGovService cache-inin test ikisisi.
type fakeCacheLookup struct {
	days  int
	body  string
	found bool
	err   error

	// son çağırışın arqumentləri (assert üçün)
	lastSvc  string
	lastPIN  string
	lastDays int

	// PR #379: LogCacheHit çağırışları
	lastHitAppID *int
	lastHitBody  string
}

func (f *fakeCacheLookup) GetCacheDays(_ context.Context, serviceName string) (int, error) {
	f.lastSvc = serviceName
	return f.days, f.err
}

func (f *fakeCacheLookup) GetCachedResponse(_ context.Context, serviceName, customerPIN string, cacheDays int) (string, bool, error) {
	f.lastSvc = serviceName
	f.lastPIN = customerPIN
	f.lastDays = cacheDays
	return f.body, f.found, f.err
}

// LogCacheHit — PR #379: marker row yazılmasını track edir (test assert üçün).
func (f *fakeCacheLookup) LogCacheHit(_ context.Context, appID *int, serviceName, customerPIN, responseBody string) error {
	f.lastHitAppID = appID
	f.lastHitBody = responseBody
	return nil
}

// TestMyGovCachedResponse_NilLookup — cache inject olunmayıbsa həmişə miss.
func TestMyGovCachedResponse_NilLookup(t *testing.T) {
	s := &MyGovService{}
	if _, ok := s.cachedResponse(context.Background(), 1, "AZMK_GET_EMPLOYEE_INFO", "1SBK08P"); ok {
		t.Fatal("nil cacheLookup must always be a cache miss")
	}
}

// TestMyGovCachedResponse_Disabled — cache_days=0 → miss (birbaşa AZMK çağırılır).
func TestMyGovCachedResponse_Disabled(t *testing.T) {
	s := &MyGovService{cacheLookup: &fakeCacheLookup{days: 0, body: "{}", found: true}}
	if _, ok := s.cachedResponse(context.Background(), 1, "AZMK_GET_EMPLOYEE_INFO", "1SBK08P"); ok {
		t.Fatal("cache_days=0 must be a cache miss")
	}
}

// TestMyGovCachedResponse_Hit — cache_days=3 + fresh row → body qaytarılır.
func TestMyGovCachedResponse_Hit(t *testing.T) {
	f := &fakeCacheLookup{days: 3, body: `{"result":1}`, found: true}
	s := &MyGovService{cacheLookup: f}
	body, ok := s.cachedResponse(context.Background(), 1, "AZMK_GET_EMPLOYEE_INFO", "1SBK08P")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if body != `{"result":1}` {
		t.Errorf("body = %q", body)
	}
	if f.lastSvc != "AZMK_GET_EMPLOYEE_INFO" || f.lastPIN != "1SBK08P" || f.lastDays != 3 {
		t.Errorf("lookup args = svc:%s pin:%s days:%d", f.lastSvc, f.lastPIN, f.lastDays)
	}
}

// TestMyGovCachedResponse_RepoError — repo xətası fail-soft miss olmalıdır.
func TestMyGovCachedResponse_RepoError(t *testing.T) {
	s := &MyGovService{cacheLookup: &fakeCacheLookup{days: 3, err: errors.New("db down")}}
	if _, ok := s.cachedResponse(context.Background(), 1, "AZMK_GET_EMPLOYEE_INFO", "1SBK08P"); ok {
		t.Fatal("repo error must be a cache miss (fail-soft)")
	}
}

// TestEmploymentInfoFromCache_Valid — real AZMK response body parse olunmurda
// Data.Response.Active dolu qalmalıdır.
func TestEmploymentInfoFromCache_Valid(t *testing.T) {
	body := `{"result":1,"requestId":"1","message":"SUCCESS","data":{"Status":{"Name":"Successful","Code":0},"Response":{"Active":[{"Contract":{"BeginDate":"01.07.2026"}}]}}}`
	info := employmentInfoFromCache(body)
	if info == nil {
		t.Fatal("expected parsed response, got nil")
	}
	if len(info.Data.Response.Active) != 1 {
		t.Errorf("Active = %d records, want 1", len(info.Data.Response.Active))
	}
}

// TestEmploymentInfoFromCache_Invalid — xarab/boş body → nil (cache miss sayılır,
// fiziki AZMK çağırışı edilir — fail-soft).
func TestEmploymentInfoFromCache_Invalid(t *testing.T) {
	for _, bad := range []string{
		"",
		"not json",
		`{"result":1}`,           // data yoxdur
		`{"result":1,"data":{}}`, // Response yoxdur
	} {
		if got := employmentInfoFromCache(bad); got != nil {
			t.Errorf("employmentInfoFromCache(%q) = %v, want nil", bad, got)
		}
	}
}

// TestPensionInfoFromCache_Valid — DisabilityGroup saxlanmalıdır.
func TestPensionInfoFromCache_Valid(t *testing.T) {
	body := `{"result":1,"requestId":"1","message":"SUCCESS","data":{"Response":{"DisabilityGroup":2,"IsPensioner":true}}}`
	p := pensionInfoFromCache(body)
	if p == nil {
		t.Fatal("expected parsed response, got nil")
	}
	if p.Data.Response.DisabilityGroup != 2 || !p.Data.Response.IsPensioner {
		t.Errorf("DisabilityGroup = %d, IsPensioner = %v", p.Data.Response.DisabilityGroup, p.Data.Response.IsPensioner)
	}
}

// TestMkrScoreFromCache_Valid — real AZMK getMkrScore body parse (PR #380).
func TestMkrScoreFromCache_Valid(t *testing.T) {
	body := `{"result":1,"requestId":"1","message":"SUCCESS","data":{"score":{"response":"B","point":819,"pdRate":0.01,"calculated":true}}}`
	m := mkrScoreFromCache(body)
	if m == nil {
		t.Fatal("expected parsed MkrScore, got nil")
	}
	if m.Score.Point != 819 || m.Score.Response != "B" || !m.Score.Calculated {
		t.Errorf("score = %+v", m.Score)
	}
}

// TestMkrScoreFromCache_Invalid — calculated=false / xarab body → nil.
func TestMkrScoreFromCache_Invalid(t *testing.T) {
	for _, bad := range []string{
		"",
		"not json",
		`{"result":1}`, // data yoxdur
		`{"result":1,"data":{"score":{"point":5,"calculated":false}}}`, // hesablanmayıb
	} {
		if got := mkrScoreFromCache(bad); got != nil {
			t.Errorf("mkrScoreFromCache(%q) = %v, want nil", bad, got)
		}
	}
}

// TestCreditHistoryFromCache_Valid — real AZMK inquireByIdCard body parse (PR #380).
func TestCreditHistoryFromCache_Valid(t *testing.T) {
	body := `{"result":1,"requestId":"1","message":"SUCCESS","data":{"return":{"reportId":"743428500","borrower":{"fin":"1SBK08P"},"liabilities":{"liability":[{"creditStatus":"007","daysMainSumOverdue":0,"monthlyPaymentAmount":8.11}]}}}}`
	h := creditHistoryFromCache(body)
	if h == nil {
		t.Fatal("expected parsed CreditHistory, got nil")
	}
	if h.Inquiry == nil || h.Inquiry.Borrower == nil || h.Inquiry.Borrower.Fin != "1SBK08P" {
		t.Fatalf("inquiry = %+v", h.Inquiry)
	}
	if len(h.Inquiry.Liabilities.Liability) != 1 {
		t.Errorf("liability count = %d, want 1", len(h.Inquiry.Liabilities.Liability))
	}
}

// TestCreditHistoryFromCache_Invalid — xarab body / data.return yox → nil.
func TestCreditHistoryFromCache_Invalid(t *testing.T) {
	for _, bad := range []string{
		"",
		"not json",
		`{"result":1}`,           // data yoxdur
		`{"result":1,"data":{}}`, // return yoxdur
	} {
		if got := creditHistoryFromCache(bad); got != nil {
			t.Errorf("creditHistoryFromCache(%q) = %v, want nil", bad, got)
		}
	}
}

// TestPensionInfoFromCache_Invalid — xarab/boş body → nil.
func TestPensionInfoFromCache_Invalid(t *testing.T) {
	for _, bad := range []string{"", "not json", `{"result":1}`, `{"result":1,"data":{}}`} {
		if got := pensionInfoFromCache(bad); got != nil {
			t.Errorf("pensionInfoFromCache(%q) = %v, want nil", bad, got)
		}
	}
}
