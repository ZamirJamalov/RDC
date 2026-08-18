package mygov

import (
	"encoding/json"
	"testing"
)

// --- PR #242: GetPensionInfoByPin types tests ---

// TestPensionInfoResponse_JSON verifies that the documented GetPensionInfoByPin
// response shape deserializes into PensionInfoResponse (envelope + Response block).
func TestPensionInfoResponse_JSON(t *testing.T) {
	raw := `{
		"result": 1,
		"requestId": "1",
		"message": "SUCCESS",
		"data": {
			"Response": {
				"DisabilityGroup": 2,
				"IsPensioner": true,
				"PensionType": "age"
			}
		}
	}`

	var p PensionInfoResponse
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if p.Result != 1 {
		t.Errorf("Result = %d, want 1", p.Result)
	}
	if p.Data == nil || p.Data.Response == nil {
		t.Fatal("Data.Response = nil, want non-nil")
	}
	if p.Data.Response.DisabilityGroup != 2 {
		t.Errorf("DisabilityGroup = %d, want 2", p.Data.Response.DisabilityGroup)
	}
	if !p.Data.Response.IsPensioner {
		t.Errorf("IsPensioner = false, want true")
	}
	if p.Data.Response.PensionType != "age" {
		t.Errorf("PensionType = %q, want %q", p.Data.Response.PensionType, "age")
	}
}

// TestPensionInfoFromAuthorizedData verifies the PR #242 fallback mapping from
// stored AuthorizedData (permission flow) to PensionInfoResponse — used when
// the AZMK pension service is unavailable.
func TestPensionInfoFromAuthorizedData(t *testing.T) {
	data := &AuthorizedData{
		DisabilityGroup: 1,
		IsPensioner:     true,
		PensionType:     "disability",
	}

	p := PensionInfoFromAuthorizedData(data)
	if p == nil || p.Data == nil || p.Data.Response == nil {
		t.Fatal("expected non-nil response chain")
	}
	if p.Data.Response.DisabilityGroup != 1 {
		t.Errorf("DisabilityGroup = %d, want 1", p.Data.Response.DisabilityGroup)
	}
	if !p.Data.Response.IsPensioner {
		t.Errorf("IsPensioner = false, want true")
	}
	if p.Data.Response.PensionType != "disability" {
		t.Errorf("PensionType = %q, want %q", p.Data.Response.PensionType, "disability")
	}
}

// TestPensionInfoFromAuthorizedData_Nil verifies the nil guard.
func TestPensionInfoFromAuthorizedData_Nil(t *testing.T) {
	if PensionInfoFromAuthorizedData(nil) != nil {
		t.Error("PensionInfoFromAuthorizedData(nil) != nil, want nil")
	}
}

// TestPensionInfoFromAuthorizedData_NoDisability verifies the passing case
// (DisabilityGroup = 0 → DISABILITY_GROUP1 cutoff passes).
func TestPensionInfoFromAuthorizedData_NoDisability(t *testing.T) {
	p := PensionInfoFromAuthorizedData(&AuthorizedData{})
	if p.Data.Response.DisabilityGroup != 0 {
		t.Errorf("DisabilityGroup = %d, want 0", p.Data.Response.DisabilityGroup)
	}
	if p.Data.Response.IsPensioner {
		t.Errorf("IsPensioner = true, want false")
	}
}
