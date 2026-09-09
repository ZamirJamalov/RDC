package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// PartnerVideoFinder — PR #427: handler-in service asılılığı. Kiçik interfeys
// (lwRouterHandler pattern-i kimi) — testlərdə fake inject olunur,
// *service.ApplicationService isə structurally satisfy edir.
type PartnerVideoFinder interface {
	GetVideoStreamURLByPINAndDate(ctx context.Context, pin, day string) (*service.PartnerVideoInfo, error)
	// PR #433: PIN-lə axtarış (tarixsiz) — BÜTÜN çəkilmiş videoların siyahısı.
	ListVideoStreamURLsByPIN(ctx context.Context, pin string) ([]*service.PartnerVideoInfo, error)
}

// PartnerAuditWriter — PR #431: service_audit_logs-a yazma interfeysi
// (*repository.ServiceAuditLogRepo satisfy edir; testlərdə fake).
type PartnerAuditWriter interface {
	Insert(ctx context.Context, log *model.ServiceAuditLog) error
}

// PartnerVideoHandler — PR #427: LW (partner) üçün video stream URL endpoint-i.
// Auth və rate limit router-da middleware kimi sarılır:
// RequirePartnerAPIKey (X-API-Key) + RateLimit(partnerVideoLimiter).
//
// PR #431: hər çağırış service_audit_logs-a yazılır (PARTNER_VIDEO_URL) —
// sağlamlıq panelində LW çağırışlarının sayı/uğursuzluqları görünür.
// Qeyd: 401 (açar yanlış) və 429 (rate limit) middleware-də bloklanır və
// audit-ə DÜŞMÜR — yalnız handler-ə çatan sorğular (200/400/404/500) yazılır.
type PartnerVideoHandler struct {
	svc   PartnerVideoFinder
	audit PartnerAuditWriter // nil = audit yazılmır (testlər)
}

// NewPartnerVideoHandler — appService + audit repo (PR #431).
func NewPartnerVideoHandler(svc PartnerVideoFinder, audit PartnerAuditWriter) *PartnerVideoHandler {
	return &PartnerVideoHandler{svc: svc, audit: audit}
}

// GetVideoURL handles GET /api/partner/video-url/{pin} and
// GET /api/partner/video-url/{pin}/{date} (PR #432: hər ikisi bu handler-də).
//
// Path parametrləri:
//   - pin:  müştərinin FIN kodu (7 hərf/rəqəm)
//   - date: müraciətin yaradıldığı gün (yyyy-mm-dd, DB server lokal vaxtı) —
//     VERİLMƏSƏ (PR #432): PIN-in ən son ÇƏKİLMİŞ videosu qaytarılır
//
// Cavab (200): app_id, stream_url, recorded, application_created_at,
// video_created_at. LW tərəfi stream_url-i brauzerdə açır.
func (h *PartnerVideoHandler) GetVideoURL(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	pin := r.PathValue("pin")
	dayStr := r.PathValue("date") // PIN-only route-da boşdur (PR #432)

	if !isValidPIN(pin) {
		writeError(w, http.StatusBadRequest, "PIN formatı yanlışdır (7 hərf/rəqəm)")
		h.writeAudit(r, http.StatusBadRequest, start, "PIN formatı yanlışdır", nil)
		return
	}

	var (
		info  *service.PartnerVideoInfo
		infos []*service.PartnerVideoInfo
		err   error
	)
	if dayStr == "" {
		// PR #433: PIN-lə axtarış — BÜTÜN çəkilmiş videolar (yeni → köhnə)
		infos, err = h.svc.ListVideoStreamURLsByPIN(r.Context(), pin)
		if err == nil && len(infos) == 0 {
			err = service.ErrVideoRecordNotFound // boş siyahı → 404 (LW düyməsi üçün sadə məntiq)
		}
	} else {
		if _, perr := time.Parse("2006-01-02", dayStr); perr != nil {
			writeError(w, http.StatusBadRequest, "tarix formatı yanlışdır (yyyy-mm-dd)")
			h.writeAudit(r, http.StatusBadRequest, start, "tarix formatı yanlışdır", nil)
			return
		}
		info, err = h.svc.GetVideoStreamURLByPINAndDate(r.Context(), pin, dayStr)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrApplicationNotFound):
			writeError(w, http.StatusNotFound, "bu gündə bu PIN-lə müraciət tapılmadı")
			h.writeAudit(r, http.StatusNotFound, start, "müraciət tapılmadı", nil)
		case errors.Is(err, service.ErrVideoRecordNotFound):
			writeError(w, http.StatusNotFound, "bu PIN üçün çəkilmiş video tapılmadı")
			h.writeAudit(r, http.StatusNotFound, start, "video tapılmadı", nil)
		case errors.Is(err, service.ErrVideoNotRecorded):
			writeError(w, http.StatusNotFound, "video hələ çəkilməyib — stream mövcud deyil")
			h.writeAudit(r, http.StatusNotFound, start, "video hələ çəkilməyib", nil)
		default:
			slog.Error("partner video url failed", "pin", pin, "error", err)
			writeError(w, http.StatusInternalServerError, "daxili xəta")
			h.writeAudit(r, http.StatusInternalServerError, start, err.Error(), nil)
		}
		return
	}

	// Uğurlu cavab: PIN-only → {"videos": [...]} (PR #433), tarixli → tək obyekt.
	var auditAppID *int
	if dayStr == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"videos": infos})
		if len(infos) > 0 {
			id := infos[0].ApplicationID // ən yeni müraciət — audit üçün
			auditAppID = &id
		}
	} else {
		writeJSON(w, http.StatusOK, info)
		id := info.ApplicationID
		auditAppID = &id
	}
	h.writeAudit(r, http.StatusOK, start, "", auditAppID)
}

// writeAudit — PR #431: PARTNER_VIDEO_URL sətiri service_audit_logs-a.
// application_id yalnız uğurlu çağırışda məlumdur (xəta halında nil).
func (h *PartnerVideoHandler) writeAudit(r *http.Request, status int, start time.Time, errMsg string, appID *int) {
	if h.audit == nil {
		return
	}
	durationMs := int(time.Since(start).Milliseconds())
	entry := &model.ServiceAuditLog{
		ApplicationID: appID,
		ServiceName:   "PARTNER_VIDEO_URL",
		Method:        "GET",
		URL:           r.URL.Path,
		StatusCode:    &status,
		DurationMs:    &durationMs,
		Error:         errMsg,
	}
	if err := h.audit.Insert(r.Context(), entry); err != nil {
		slog.Warn("PR #431: partner video url audit write failed", "error", err)
	}
}
