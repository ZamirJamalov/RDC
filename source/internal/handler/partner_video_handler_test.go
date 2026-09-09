package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// fakePartnerVideoFinder — PartnerVideoFinder interfeysinin test implementation-u.
// PR #435: axtarış kredit müqavilə nömrəsi ilə (köhnə PIN/PIN+tarix silindi).
type fakePartnerVideoFinder struct {
	list []*service.PartnerVideoInfo
	err  error

	lastLoanID string
	calls      int
}

func (f *fakePartnerVideoFinder) ListVideoStreamURLsByAzmkLoanID(_ context.Context, azmkLoanID string) ([]*service.PartnerVideoInfo, error) {
	f.calls++
	f.lastLoanID = azmkLoanID
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

// fakePartnerAudit — PR #431: audit yazılarını tutur (assert üçün).
type fakePartnerAudit struct {
	rows []model.ServiceAuditLog
	err  error
}

func (f *fakePartnerAudit) Insert(_ context.Context, log *model.ServiceAuditLog) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, *log)
	return nil
}

// doPartnerRequest — handler-i birbaşa deyil, ServeMux üzərindən çağırır:
// r.PathValue yalnız mux pattern-matching-dən sonra dolur (Go 1.22+).
func doPartnerRequest(h *PartnerVideoHandler, url string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /api/partner/video-url/{loanId}", http.HandlerFunc(h.GetVideoURL))
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestPartnerVideoHandler_BadLoanID(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{}, nil)

	// Çox qısa (min 3 simvol) — validation tutmalı
	w := doPartnerRequest(h, "/api/partner/video-url/HO")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}

	// Qadağan olunan simvol (tire) — validation tutmalı
	w = doPartnerRequest(h, "/api/partner/video-url/HO-003")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPartnerVideoHandler_AppNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrApplicationNotFound}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_VideoNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrVideoRecordNotFound}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_EmptyListIs404(t *testing.T) {
	// Müdafiəçi hal: service boş siyahı qaytarsa handler 404 çevirməlidir
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{list: nil}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404 for empty list", w.Code)
	}
}

func TestPartnerVideoHandler_InternalError(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: errors.New("db down")}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestPartnerVideoHandler_Success(t *testing.T) {
	fake := &fakePartnerVideoFinder{
		list: []*service.PartnerVideoInfo{
			{
				AppID:         "b7d57b9e-071a-4819-8dbb-26f25539f29d",
				StreamURL:     "https://rec.azmk.az:8699/video/b7d57b9e-071a-4819-8dbb-26f25539f29d/stream",
				Recorded:      true,
				ApplicationID: 34, // ən yeni video — audit üçün
			},
			{
				AppID:         "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
				StreamURL:     "https://rec.azmk.az:8699/video/c8241a89-0038-4b8f-9b28-b2bbc7892b24/stream",
				Recorded:      true,
				ApplicationID: 34,
			},
		},
	}
	h := NewPartnerVideoHandler(fake, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.calls != 1 {
		t.Errorf("svc calls = %d, want 1", fake.calls)
	}
	if fake.lastLoanID != "HO0030210" {
		t.Errorf("svc got loanID=%q, want HO0030210", fake.lastLoanID)
	}
	body := w.Body.String()
	if !strings.Contains(body, "videos") {
		t.Errorf("body should contain videos wrapper: %s", body)
	}
	if !strings.Contains(body, "b7d57b9e") || !strings.Contains(body, "c8241a89") {
		t.Errorf("body should contain BOTH app_ids: %s", body)
	}
	if !strings.Contains(body, "/stream") {
		t.Errorf("body should contain stream url: %s", body)
	}
	// PR #431: application_id cavabda OLMAMALIDI (daxili ID xaricə çıxmır)
	if strings.Contains(body, "application_id") {
		t.Errorf("body should not contain internal application_id: %s", body)
	}
}

// TestPartnerVideoHandler_AuditWritten — PR #431: hər çağırış
// service_audit_logs-a PARTNER_VIDEO_URL sətri yazır.
func TestPartnerVideoHandler_AuditWritten(t *testing.T) {
	audit := &fakePartnerAudit{}
	fake := &fakePartnerVideoFinder{
		list: []*service.PartnerVideoInfo{
			{
				AppID:         "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
				StreamURL:     "https://rec.azmk.az:8699/video/x/stream",
				Recorded:      true,
				ApplicationID: 33,
			},
		},
	}
	h := NewPartnerVideoHandler(fake, audit)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(audit.rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audit.rows))
	}
	row := audit.rows[0]
	if row.ServiceName != "PARTNER_VIDEO_URL" {
		t.Errorf("service_name = %q, want PARTNER_VIDEO_URL", row.ServiceName)
	}
	if row.StatusCode == nil || *row.StatusCode != 200 {
		t.Errorf("status_code = %v, want 200", row.StatusCode)
	}
	if row.ApplicationID == nil || *row.ApplicationID != 33 {
		t.Errorf("application_id = %v, want 33 (ən yeni videodan)", row.ApplicationID)
	}
	if row.Error != "" {
		t.Errorf("error = %q, want empty", row.Error)
	}
}

// TestPartnerVideoHandler_AuditWrittenOnFailure — 404 halında da audit yazılır.
func TestPartnerVideoHandler_AuditWrittenOnFailure(t *testing.T) {
	audit := &fakePartnerAudit{}
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrApplicationNotFound}, audit)

	w := doPartnerRequest(h, "/api/partner/video-url/HO0030210")
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if len(audit.rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audit.rows))
	}
	row := audit.rows[0]
	if row.StatusCode == nil || *row.StatusCode != 404 {
		t.Errorf("status_code = %v, want 404", row.StatusCode)
	}
	if row.Error == "" {
		t.Error("error should be non-empty for failed call")
	}
	if row.ApplicationID != nil {
		t.Errorf("application_id = %v, want nil on failure", row.ApplicationID)
	}
}
