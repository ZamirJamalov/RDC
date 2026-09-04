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

// TestPensionInfoFromCache_Invalid — xarab/boş body → nil.
func TestPensionInfoFromCache_Invalid(t *testing.T) {
	for _, bad := range []string{"", "not json", `{"result":1}`, `{"result":1,"data":{}}`} {
		if got := pensionInfoFromCache(bad); got != nil {
			t.Errorf("pensionInfoFromCache(%q) = %v, want nil", bad, got)
		}
	}
}
