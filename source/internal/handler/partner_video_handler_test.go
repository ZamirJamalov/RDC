package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rdc-source/internal/service"
)

// fakePartnerVideoFinder — PartnerVideoFinder interfeysinin test implementation-u.
type fakePartnerVideoFinder struct {
	info *service.PartnerVideoInfo
	err  error

	lastPIN string
	lastDay string
	calls   int
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

// doPartnerRequest — handler-i birbaşa deyil, ServeMux üzərindən çağırır:
// r.PathValue yalnız mux pattern-matching-dən sonra dolur (Go 1.22+).
func doPartnerRequest(h *PartnerVideoHandler, url string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /api/partner/video-url/{pin}/{date}", http.HandlerFunc(h.GetVideoURL))
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestPartnerVideoHandler_BadPIN(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{})

	w := doPartnerRequest(h, "/api/partner/video-url/ABC/2026-09-07")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPartnerVideoHandler_BadDate(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{})

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/07.09.2026")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPartnerVideoHandler_AppNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrApplicationNotFound})

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_VideoNotFound(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrVideoRecordNotFound})

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestPartnerVideoHandler_NotRecorded(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: service.ErrVideoNotRecorded})

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404 (video hələ çəkilməyib)", w.Code)
	}
}

func TestPartnerVideoHandler_InternalError(t *testing.T) {
	h := NewPartnerVideoHandler(&fakePartnerVideoFinder{err: errors.New("db down")})

	w := doPartnerRequest(h, "/api/partner/video-url/1ABC234/2026-09-07")

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestPartnerVideoHandler_Success(t *testing.T) {
	fake := &fakePartnerVideoFinder{
		info: &service.PartnerVideoInfo{
			AppID:     "c8241a89-0038-4b8f-9b28-b2bbc7892b24",
			StreamURL: "https://rec.azmk.az:8699/video/c8241a89-0038-4b8f-9b28-b2bbc7892b24/stream",
			Recorded:  true,
		},
	}
	h := NewPartnerVideoHandler(fake)

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
}
