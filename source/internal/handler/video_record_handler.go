package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// startVideoRecordRequest is the JSON body for POST /api/applications/{id}/video-record/start.
// PR #189: amount frontend-dən göndərilir (müştərinin seçdiyi kredit məbləği).
type startVideoRecordRequest struct {
	Amount float64 `json:"amount"`
}

// StartVideoRecord handles POST /api/applications/{id}/video-record/start.
// PR #188: müştəri kredit təsdiq etməzdən əvvəl video identifikasiya başladır.
// PR #189: amount body-dən oxunur, video service-ə ötürülür.
// PR #191: public_id UUID qəbul edir.
// Returns redirect_url for iframe embedding.
//
// Public endpoint (müştəri özü çağırır, auth tələb olunmur — application_id audit üçün kifayət).
func (h *ApplicationHandler) StartVideoRecord(w http.ResponseWriter, r *http.Request) {
	publicID, err := parsePathUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// PR #149: IDOR protection — fetch app, return 404 if not found (don't leak existence)
	app, err := h.service.GetApplicationByPublicID(r.Context(), publicID)
	if err != nil || app == nil {
		writeError(w, http.StatusNotFound, "müraciət tapılmadı")
		return
	}
	appID := app.ID

	if !h.service.IsVideoRecordEnabled() {
		writeError(w, http.StatusBadRequest, "video record deaktiv")
		return
	}

	// PR #189: amount body-dən oxu (boş body də qəbul olunsun — default 0)
	var req startVideoRecordRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Amount < 0 {
		writeError(w, http.StatusBadRequest, "amount 0-dan kiçik ola bilməz")
		return
	}

	redirectURL, err := h.service.StartVideoRecord(r.Context(), appID, req.Amount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "video record başladıla bilmədi: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"redirect_url": redirectURL,
	})
}

// CheckVideoRecordStatus handles GET /api/applications/{id}/video-record/status.
// PR #188: frontend hər 2 saniyədən bir çağırır, recorded=true qayıdanda modalı bağlayır.
// PR #191: public_id UUID qəbul edir.
func (h *ApplicationHandler) CheckVideoRecordStatus(w http.ResponseWriter, r *http.Request) {
	publicID, err := parsePathUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// IDOR protection
	app, err := h.service.GetApplicationByPublicID(r.Context(), publicID)
	if err != nil || app == nil {
		writeError(w, http.StatusNotFound, "müraciət tapılmadı")
		return
	}
	appID := app.ID

	if !h.service.IsVideoRecordEnabled() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"recorded":       true,
			"video_required": false,
		})
		return
	}

	recorded, err := h.service.CheckVideoRecordStatus(r.Context(), appID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status yoxlana bilmədi: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recorded":       recorded,
		"video_required": true,
	})
}

// GetVideoRecord handles GET /api/applications/{id}/video-record.
// PR #188: mövcud video record məlumatını qaytarır (redirect_url, recorded).
// PR #191: public_id UUID qəbul edir.
func (h *ApplicationHandler) GetVideoRecord(w http.ResponseWriter, r *http.Request) {
	publicID, err := parsePathUUID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// IDOR protection
	app, err := h.service.GetApplicationByPublicID(r.Context(), publicID)
	if err != nil || app == nil {
		writeError(w, http.StatusNotFound, "müraciət tapılmadı")
		return
	}
	appID := app.ID

	if !h.service.IsVideoRecordEnabled() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"video_required": false,
			"recorded":       true,
		})
		return
	}

	redirectURL, recorded, err := h.service.GetVideoRecordRedirectURL(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"video_required": true,
			"recorded":       false,
			"redirect_url":   "",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"video_required": true,
		"recorded":       recorded,
		"redirect_url":   redirectURL,
	})
}

// GetApplicationVideoRequired returns whether video record is required for an application.
// PR #188: apply.html button aktivləşdirmək üçün yoxlayır.
// PR #191: public_id UUID qəbul edir (app_id query param).
// Public endpoint (no auth).
func (h *ApplicationHandler) GetApplicationVideoRequired(w http.ResponseWriter, r *http.Request) {
	publicIDStr := r.URL.Query().Get("app_id")
	publicID, err := uuid.Parse(publicIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "app_id tələb olunur (UUID format)")
		return
	}

	app, err := h.service.GetApplicationByPublicID(r.Context(), publicID.String())
	if err != nil || app == nil {
		writeError(w, http.StatusNotFound, "müraciət tapılmadı")
		return
	}

	videoRequired := h.service.IsVideoRecordEnabled()
	recorded := false
	if videoRequired {
		recorded, _ = h.service.IsVideoRecorded(r.Context(), app.ID)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"video_required": videoRequired,
		"recorded":       recorded,
	})
}
