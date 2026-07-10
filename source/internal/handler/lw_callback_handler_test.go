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
//
// Note: this test passes nil for SimaService — the validation paths
// (invalid JSON, missing app_id) don't reach the service. For the success
// path, the handler calls simaService.HandleCallback which would panic
// on nil — so we skip the success test here (it requires a mock service).
func TestLWCallbackHandler_SimaResult_InvalidJSON(t *testing.T) {
        h := NewLWCallbackHandler(nil)

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
        h := NewLWCallbackHandler(nil)

        body := `{"session_id":"SIMA-001","status":"success"}`
        req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader(body))
        w := httptest.NewRecorder()

        h.SimaResult(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// TestLWCallbackHandler_SimaResult_MissingSessionID verifies that a missing
// session_id returns 400.
func TestLWCallbackHandler_SimaResult_MissingSessionID(t *testing.T) {
        h := NewLWCallbackHandler(nil)

        body := `{"application_id":42,"status":"success"}`
        req := httptest.NewRequest("POST", "/api/rdc/callback/sima-result", strings.NewReader(body))
        w := httptest.NewRecorder()

        h.SimaResult(w, req)

        if w.Code != http.StatusBadRequest {
                t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
        }
}

// Ensure json import is used.
var _ = json.NewDecoder

