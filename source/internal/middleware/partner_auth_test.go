package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// PR #427: RequirePartnerAPIKey middleware testləri.
func TestRequirePartnerAPIKey_EmptyKeyDisabled(t *testing.T) {
	handler := RequirePartnerAPIKey("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called when API key is empty")
	}))

	req := httptest.NewRequest("GET", "/api/partner/video-url/1ABC234/2026-09-07", nil)
	req.Header.Set("X-API-Key", "anything")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404 (endpoint disabled)", w.Code)
	}
}

func TestRequirePartnerAPIKey_MissingHeader(t *testing.T) {
	handler := RequirePartnerAPIKey("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called without header")
	}))

	req := httptest.NewRequest("GET", "/api/partner/video-url/1ABC234/2026-09-07", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequirePartnerAPIKey_WrongKey(t *testing.T) {
	handler := RequirePartnerAPIKey("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called with wrong key")
	}))

	req := httptest.NewRequest("GET", "/api/partner/video-url/1ABC234/2026-09-07", nil)
	req.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequirePartnerAPIKey_CorrectKey(t *testing.T) {
	called := false
	handler := RequirePartnerAPIKey("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/partner/video-url/1ABC234/2026-09-07", nil)
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !called {
		t.Error("next handler should be called with correct key")
	}
}
