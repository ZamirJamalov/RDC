package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLWCallbackHandler_SimaResult_Success verifies that a valid SIMA
// callback is accepted with 200 OK and a confirmation response.
func TestLWCallbackHandler_SimaResult_Success(t *testing.T) {
	h := NewLWCallbackHandler()

	body := `{"application_id":42,"session_id":"SIMA-001","status":"success","detail":"KYC completed","completed_at":"2026-07-10T12:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.SimaResult(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp SimaResultCallbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ApplicationID != 42 {
		t.Errorf("ApplicationID = %d, want 42", resp.ApplicationID)
	}
	if !resp.Received {
		t.Error("Received = false, want true")
	}
}

// TestLWCallbackHandler_SimaResult_InvalidJSON verifies that malformed JSON
// returns 400.
func TestLWCallbackHandler_SimaResult_InvalidJSON(t *testing.T) {
	h := NewLWCallbackHandler()

	req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	h.SimaResult(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLWCallbackHandler_SimaResult_MissingApplicationID verifies that a
// missing/zero application_id returns 400.
func TestLWCallbackHandler_SimaResult_MissingApplicationID(t *testing.T) {
	h := NewLWCallbackHandler()

	body := `{"session_id":"SIMA-001","status":"success"}`
	req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.SimaResult(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLWCallbackHandler_SimaResult_FailedStatus verifies that a "failed"
// SIMA status is still accepted with 200 (we acknowledge receipt even for
// failures — the processing logic will be added in Phase 4).
func TestLWCallbackHandler_SimaResult_FailedStatus(t *testing.T) {
	h := NewLWCallbackHandler()

	body := `{"application_id":42,"session_id":"SIMA-001","status":"failed","detail":"Customer face not recognized"}`
	req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.SimaResult(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (acknowledge even for failures)", w.Code, http.StatusOK)
	}
}
