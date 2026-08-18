package azmk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetPensionInfoByPin_RequestType verifies that the wire requestType sent to
// AZMK CustomerDataService is "GetPensionInfo" — NOT "GetPensionInfoByPin".
//
// PR #244 regression test: əvvəl "GetPensionInfoByPin" göndərilirdi və AZMK
// "Unknown requestType: GetPensionInfoByPin (result=0)" xətası qaytarırdı.
// (Müqayisə üçün: iş yeri sorğusunda requestType "GetEmployeeInfoByPin"-dir —
// pensiya servisində "ByPin" şəkilçisi yoxdur.)
func TestGetPensionInfoByPin_RequestType(t *testing.T) {
	var gotRequestType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if rt, ok := body["requestType"].(string); ok {
			gotRequestType = rt
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":1,"requestId":"1","message":"SUCCESS","data":{"Response":{"DisabilityGroup":0,"IsPensioner":false,"PensionType":""}}}`))
	}))
	defer srv.Close()

	p := NewHTTPCustomerDataProvider(srv.URL, "", "", 5)
	resp, err := p.GetPensionInfoByPin(context.Background(), "1SBK08P", "AA1234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotRequestType != "GetPensionInfo" {
		t.Errorf("requestType = %q, want %q (PR #244)", gotRequestType, "GetPensionInfo")
	}
	if resp == nil || resp.Data == nil || resp.Data.Response == nil {
		t.Fatal("expected non-nil response chain")
	}
	if resp.Data.Response.DisabilityGroup != 0 {
		t.Errorf("DisabilityGroup = %d, want 0", resp.Data.Response.DisabilityGroup)
	}
	if resp.Data.Response.IsPensioner {
		t.Errorf("IsPensioner = true, want false")
	}
}

// TestGetPensionInfoByPin_ResultNotOne_Errors verifies that a non-1 result
// (e.g. the old "Unknown requestType" response) surfaces as an error.
func TestGetPensionInfoByPin_ResultNotOne_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":0,"requestId":"1","message":"Unknown requestType: GetPensionInfoByPin","data":null}`))
	}))
	defer srv.Close()

	p := NewHTTPCustomerDataProvider(srv.URL, "", "", 5)
	_, err := p.GetPensionInfoByPin(context.Background(), "1SBK08P", "AA1234567")
	if err == nil {
		t.Fatal("expected error for result != 1, got nil")
	}
}
