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
// PR #435: axtarış kredit müqavilə nömrəsi ilə (köhnə PIN/PIN+tarix silindi).
type PartnerVideoFinder interface {
	ListVideoStreamURLsByAzmkLoanID(ctx context.Context, azmkLoanID string) ([]*service.PartnerVideoInfo, error)
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

// GetVideoURL handles GET /api/partner/video-url/{loanId} (PR #435).
//
// Path parametri:
//   - loanId: LW-dən gələn kredit müqavilə nömrəsi (loan_applications.azmk_loan_id,
//     məs. "HO0030210") — GET /application/{id}/status cavabından saxlanılır
//
// Cavab (200): {"videos": [...]} — həmin müqaviləyə uyğun müraciətin ÇƏKİLMİŞ
// videoları (yeni → köhnə). Hər element: app_id, stream_url, recorded,
// application_created_at, video_created_at. LW tərəfi stream_url-i brauzerdə açır.
func (h *PartnerVideoHandler) GetVideoURL(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	loanID := r.PathValue("loanId")

	if !isValidAzmkLoanID(loanID) {
		writeError(w, http.StatusBadRequest, "kredit müqavilə nömrəsi formatı yanlışdır")
		h.writeAudit(r, http.StatusBadRequest, start, "loan id formatı yanlışdır", nil)
		return
	}

	videos, err := h.svc.ListVideoStreamURLsByAzmkLoanID(r.Context(), loanID)
	if err == nil && len(videos) == 0 {
		// Müdafiəçi hal — service boş siyahı yerinə ErrVideoRecordNotFound qaytarır,
		// amma gələcəkdə dəyişsə LW düyməsi üçün sadə məntiq qorunur.
		err = service.ErrVideoRecordNotFound
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrApplicationNotFound):
			writeError(w, http.StatusNotFound, "bu kredit müqavilə nömrəsi ilə müraciət tapılmadı")
			h.writeAudit(r, http.StatusNotFound, start, "müraciət tapılmadı", nil)
		case errors.Is(err, service.ErrVideoRecordNotFound):
			writeError(w, http.StatusNotFound, "bu kredit müqaviləsi üçün çəkilmiş video tapılmadı")
			h.writeAudit(r, http.StatusNotFound, start, "video tapılmadı", nil)
		default:
			slog.Error("partner video url failed", "azmk_loan_id", loanID, "error", err)
			writeError(w, http.StatusInternalServerError, "daxili xəta")
			h.writeAudit(r, http.StatusInternalServerError, start, err.Error(), nil)
		}
		return
	}

	// Uğurlu cavab: {"videos": [...]} (PR #433 formatı qorunur).
	writeJSON(w, http.StatusOK, map[string]interface{}{"videos": videos})
	var auditAppID *int
	if len(videos) > 0 {
		id := videos[0].ApplicationID // ən yeni video — audit üçün
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
