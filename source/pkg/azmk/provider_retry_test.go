package azmk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// PR #420: idempotent AZMK çağırışları üçün HTTP-level retry testləri.
// httptest server real HTTPProvider ilə işləyir — connection retry (PR #264)
// burada devreye girmir (server lokal, dərhal cavab verir), yalnız
// withAzmkRetry məntiqi yoxlanılır.

func newRetryTestProvider(t *testing.T, handler http.Handler) *HTTPProvider {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return NewHTTPProvider(ts.URL, "user", "pass", 5)
}

// TestRegisterCard_RetriesOn5xx — 2 dəfə 500, 3-cüdə 200 → uğur.
// Müvəqqəti server xətaları retry edilməlidir.
func TestRegisterCard_RetriesOn5xx(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			http.Error(w, "temporary error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "CARD-123"})
	}))

	id, err := p.RegisterCard(context.Background(), &CardRequest{
		CardData: CardData{PartnerID: "P1", Code: "4169738877117119", Expiring: "2030-01-01"},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if id != "CARD-123" {
		t.Errorf("expected CARD-123, got %s", id)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 calls (2 fail + 1 success), got %d", got)
	}
}

// TestRegisterCard_4xxNotRetried — 400 validasiya xətası dərhal qaytarılır,
// retry OLUNMAMALI (təkrar sorğu nəticəni dəyişməz — Günel "Invalid code"
// hadisəsindəki kimi).
func TestRegisterCard_4xxNotRetried(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "Invalid code", http.StatusBadRequest)
	}))

	_, err := p.RegisterCard(context.Background(), &CardRequest{
		CardData: CardData{PartnerID: "P1", Code: "4098584464056264", Expiring: "2030-01-01"},
	})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("4xx must NOT be retried — expected 1 call, got %d", got)
	}
}

// TestRegisterCard_ExhaustsRetries — hər dəfə 500 → 3 cəhddən sonra uğursuz.
func TestRegisterCard_ExhaustsRetries(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))

	_, err := p.RegisterCard(context.Background(), &CardRequest{
		CardData: CardData{PartnerID: "P1", Code: "4169738877117119", Expiring: "2030-01-01"},
	})
	if err == nil {
		t.Fatal("expected error when all retries exhausted, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", got)
	}
}

// TestRegisterPartner_RetriesOn5xx — PUT /partner da 5xx-də retry olunur.
func TestRegisterPartner_RetriesOn5xx(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "PARTNER-9"})
	}))

	id, err := p.RegisterPartner(context.Background(), &PartnerRequest{})
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if id != "PARTNER-9" {
		t.Errorf("expected PARTNER-9, got %s", id)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

// TestCreateApplication_5xxNotRetried — qeyri-idempotent əməliyyat:
// 500 cavabı belə təkrar sorğu GÖNDƏRİLMƏMƏLİDİR (cüt application riski).
func TestCreateApplication_5xxNotRetried(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "Sifariş olunmuş məbləğ: Dəyər icazə verilmiş həddən kənardır", http.StatusInternalServerError)
	}))

	_, err := p.CreateApplication(context.Background(), &ApplicationCreateRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("non-idempotent create must NOT retry 5xx — expected 1 call, got %d", got)
	}
}

// TestDisburse_5xxNotRetried — qeyri-idempotent əməliyyat:
// cüt köçürmə riskinə görə 5xx-də belə retry YOXDUR.
func TestDisburse_5xxNotRetried(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "internal", http.StatusInternalServerError)
	}))

	err := p.Disburse(context.Background(), &DisburseRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("disburse must NEVER retry 5xx — expected 1 call, got %d", got)
	}
}

// TestGetCards_404NotRetried — 404 "kart yoxdur" normal haldır:
// retry olunmur, boş siyahı qaytarılır (PR #313 semantikası qorunur).
func TestGetCards_404NotRetried(t *testing.T) {
	var calls int32
	p := newRetryTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "Invalidid", http.StatusNotFound)
	}))

	cards, err := p.GetCards(context.Background(), "UNKNOWN")
	if err != nil {
		t.Fatalf("404 must map to empty list, got error: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected empty list, got %d cards", len(cards))
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("404 must NOT be retried — expected 1 call, got %d", got)
	}
}

// TestWithAzmkRetry_ContextCancel — backoff gözləməsi zamanı context ləğv
// olunarsa dərhal qaytarır (asılı bloklarlıq yoxdur).
func TestWithAzmkRetry_ContextCancel(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := withAzmkRetry(ctx, "/test", func() (string, error) {
		return doPostForTest(ctx, srv.URL)
	})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("context cancel must stop retries early, took %v", elapsed)
	}
}

// doPostForTest — test köməkçisi: sadə POST, 5xx olanda typed httpError qaytarır.
func doPostForTest(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpError{Path: "/test", Status: resp.StatusCode, Body: "down"}
	}
	return "ok", nil
}
