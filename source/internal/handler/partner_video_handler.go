package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"rdc-source/internal/service"
)

// PartnerVideoFinder — PR #427: handler-in service asılılığı. Kiçik interfeys
// (lwRouterHandler pattern-i kimi) — testlərdə fake inject olunur,
// *service.ApplicationService isə structurally satisfy edir.
type PartnerVideoFinder interface {
	GetVideoStreamURLByPINAndDate(ctx context.Context, pin, day string) (*service.PartnerVideoInfo, error)
}

// PartnerVideoHandler — PR #427: LW (partner) üçün video stream URL endpoint-i.
// Auth və rate limit router-da middleware kimi sarılır:
// RequirePartnerAPIKey (X-API-Key) + RateLimit(partnerVideoLimiter).
type PartnerVideoHandler struct {
	svc PartnerVideoFinder
}

// NewPartnerVideoHandler — appService (və ya testdə fake) qəbul edir.
func NewPartnerVideoHandler(svc PartnerVideoFinder) *PartnerVideoHandler {
	return &PartnerVideoHandler{svc: svc}
}

// GetVideoURL handles GET /api/partner/video-url/{pin}/{date}.
//
// Path parametrləri:
//   - pin:  müştərinin FIN kodu (7 hərf/rəqəm)
//   - date: müraciətin yaradıldığı gün (yyyy-mm-dd, DB server lokal vaxtı)
//
// Cavab (200): app_id, stream_url, recorded, application_created_at,
// video_created_at. LW tərəfi stream_url-i brauzerdə açır (video servisin
// öz auth-u var).
func (h *PartnerVideoHandler) GetVideoURL(w http.ResponseWriter, r *http.Request) {
	pin := r.PathValue("pin")
	dayStr := r.PathValue("date")

	if !isValidPIN(pin) {
		writeError(w, http.StatusBadRequest, "PIN formatı yanlışdır (7 hərf/rəqəm)")
		return
	}
	if _, err := time.Parse("2006-01-02", dayStr); err != nil {
		writeError(w, http.StatusBadRequest, "tarix formatı yanlışdır (yyyy-mm-dd)")
		return
	}

	info, err := h.svc.GetVideoStreamURLByPINAndDate(r.Context(), pin, dayStr)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrApplicationNotFound):
			writeError(w, http.StatusNotFound, "bu gündə bu PIN-lə müraciət tapılmadı")
		case errors.Is(err, service.ErrVideoRecordNotFound):
			writeError(w, http.StatusNotFound, "bu müraciət üçün video record yoxdur")
		case errors.Is(err, service.ErrVideoNotRecorded):
			writeError(w, http.StatusNotFound, "video hələ çəkilməyib — stream mövcud deyil")
		default:
			slog.Error("partner video url failed", "pin", pin, "error", err)
			writeError(w, http.StatusInternalServerError, "daxili xəta")
		}
		return
	}

	writeJSON(w, http.StatusOK, info)
}
