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
type fakePartnerVideoFinder struct {
	info *service.PartnerVideoInfo
	err  error

	// PR #433: PIN-only axını ayrıca konfiqurasiya olunur (siyahı)
	pinOnlyList []*service.PartnerVideoInfo
	pinOnlyErr  error

	lastPIN      string
	lastDay      string
	calls        int
	pinOnlyCalls int
}

func (f *fakePartnerVideoFinder) GetVideoStreamURLByPINAndDate(_ context.Context, pin, day string) (*service.PartnerVideoInfo, error) {
	f.calls++
	f.lastPIN = pin
	f.lastDay = day
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

func (f *fakePartnerVideoFinder) ListVideoStreamURLsByPIN(_ context.Context, pin string) ([]*service.PartnerVideoInfo, error) {
	f.pinOnlyCalls++
	f.lastPIN = pin
	if f.pinOnlyErr != nil {
		return nil, f.pinOnlyErr
	}
	return f.pinOnlyList, nil
}

// doPartnerRequest — handler-i birbaşa deyil, ServeMux üzərindən çağırır:
// r.PathValue yalnız mux pattern-matching-dən sonra dolur (Go 1.22+).
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
	mux.Handle("GET /api/partner/video-url/{pin}", http.HandlerFunc(h.GetVideoURL))
	mux.Handle("GET /api/partner/video-url/{pin}/{date}", http.HandlerFunc(h.GetVideoURL))
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestPartnerVideoHandler_BadPIN(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/ABC/2026-09-07")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPartnerVideoHandler_BadDate(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/07.09.2026")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPartnerVideoHandler_AppNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrApplicationNotFound}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_VideoNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrVideoRecordNotFound}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_NotRecorded(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrVideoNotRecorded}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404 (video hələ çəkilməyib)", w.Code)
	}
}

func TestPartnerVideoHandler_InternalError(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: errors.New("db down")}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestPartnerVideoHandler_Success(t *testing.T) {
	fake := &fakePartnerVideoFinder{
		info: &service.PartnerVideoInfo{
			AppID:         "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
			StreamURL:     "https://rec.azmk.az:8699/video/c8241a89-0038-4b8f-9b28-b2bbc7892b24/stream",
			Recorded:      true,
			ApplicationID: 33, // PR #431: audit üçün (JSON-da görünmür)
		},
	}
	h := NewPartnerVideoHandler(fake, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "c8241a89-0038-4b8f-9b28-b2bbc7892b24") {
		t.Errorf("body should contain app_id: %s", body)
	}
	if !strings.Contains(body, "/stream") {
		t.Errorf("body should contain stream url: %s", body)
	}
	if fake.calls != 1 {
		t.Errorf("svc calls = %d, want 1", fake.calls)
	}
	if fake.lastPIN != "1ABC234" || fake.lastDay != "2026-09-07" {
		t.Errorf("svc got pin=%q day=%q, want 1ABC234/2026-09-07", fake.lastPIN, fake.lastDay)
	}
	// PR #431: application_id cavabda OLMAMALIDI (daxili ID xaricə çıxmır)
	if strings.Contains(w.Body.String(), "application_id") {
		t.Errorf("body should not contain internal application_id: %s", w.Body.String())
	}
}

// TestPartnerVideoHandler_AuditWritten — PR #431: hər çağırış
// service_audit_logs-a PARTNER_VIDEO_URL sətri yazır.
func TestPartnerVideoHandler_AuditWritten(t *testing.T) {
	audit := &fakePartnerAudit{}
	fake := &fakePartnerVideoFinder{
		info: &service.PartnerVideoInfo{
			AppID:         "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
			StreamURL:     "https://rec.azmk.az:8699/video/x/stream",
			Recorded:      true,
			ApplicationID: 33,
		},
	}
	h := NewPartnerVideoHandler(fake, audit)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")
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
		t.Errorf("application_id = %v, want 33", row.ApplicationID)
	}
	if row.Error != "" {
		t.Errorf("error = %q, want empty", row.Error)
	}
}

// TestPartnerVideoHandler_AuditWrittenOnFailure — 404 halında da audit yazılır.
func TestPartnerVideoHandler_AuditWrittenOnFailure(t *testing.T) {
	audit := &fakePartnerAudit{}
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrApplicationNotFound}, audit)

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")
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

// PR #433: PIN-lə axtarış (tarixsiz) — BÜTÜN videoların siyahısı.
func TestPartnerVideoHandler_PinOnly_Success(t *testing.T) {
	fake := &fakePartnerVideoFinder{
		pinOnlyList: []*service.PartnerVideoInfo{
			{
				AppID:     "b7d57b9e-071a-4819-8dbb-26f25539f29d",
				StreamURL: "https://rec.azmk.az:8699/video/b7d57b9e-071a-4819-8dbb-26f25539f29d/stream",
				Recorded:  true,
			},
			{
				AppID:     "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
				StreamURL: "https://rec.azmk.az:8699/video/c8241a89-0038-4b8f-9b28-b2bbc7892b24/stream",
				Recorded:  true,
			},
		},
	}
	h := NewPartnerVideoHandler(fake, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/29G00GF")

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.pinOnlyCalls != 1 {
		t.Errorf("pin-only calls = %d, want 1", fake.pinOnlyCalls)
	}
	if fake.calls != 0 {
		t.Errorf("dated calls = %d, want 0 (PIN-only axını işləməli idi)", fake.calls)
	}
	body := w.Body.String()
	if !strings.Contains(body, "videos") {
		t.Errorf("body should contain videos wrapper: %s", body)
	}
	if !strings.Contains(body, "b7d57b9e") || !strings.Contains(body, "c8241a89") {
		t.Errorf("body should contain BOTH app_ids: %s", body)
	}
}

// PR #433: boş siyahı → 404 (LW düyməsi üçün sadə məntiq).
func TestPartnerVideoHandler_PinOnly_EmptyListIs404(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{pinOnlyList: nil}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/29G00GF")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404 for empty list", w.Code)
	}
}

func TestPartnerVideoHandler_PinOnly_NotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{pinOnlyErr: service.ErrVideoRecordNotFound}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/29G00GF")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_PinOnly_BadPIN(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{}, nil)

	w := doPartnerRequest(h, "/api/partner/video-url/ABC")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
