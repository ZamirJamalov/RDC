package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"rdc-source/internal/model"
	"rdc-source/pkg/videorecord"
)

// StartVideoRecord creates a video record order for the application.
// PR #188: müştəri kredit təsdiq etməzdən əvvəl video identifikasiya keçməlidir.
// PR #189: amount parametri əlavə edildi (frontend-dən seçilən kredit məbləği).
//
// Flow:
//  1. Fetch application + customer info (Name/Surname from AZMK GetPersonalInfo)
//  2. Call video service POST /api/orders (amount ilə)
//  3. Store request/response in video_records table
//  4. Return redirect_url to frontend (for iframe)
func (s *ApplicationService) StartVideoRecord(ctx context.Context, appID int, amount float64) (string, error) {
	if !s.IsVideoRecordEnabled() {
		return "", fmt.Errorf("video record deaktiv")
	}

	// 1. Fetch application
	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return "", fmt.Errorf("application not found: %d", appID)
	}

	// 2. Get customer name — PR #243: early cutoff mərhələsində AZMK GetPersonalInfo
	// cavabından saxlanmış ad varsa onu istifadə et (ikinci AZMK_GET_PERSONAL_INFO
	// sorğusunun qarşısını alır). Boşdursa fail-soft çağırış.
	customerName := app.CustomerFullName
	if customerName != "" {
		slog.Info("video record: using saved customer_full_name (PR #243)",
			"application_id", appID)
	} else if s.customerDataProvider != nil {
		data, err := s.customerDataProvider.GetPersonalInfo(ctx, app.CustomerPIN, app.CustomerSerial)
		if err != nil {
			slog.Warn("video record: failed to fetch customer name from AZMK — using empty", "error", err)
		} else if data != nil {
			customerName = data.FullName()
		}
	}

	// 3. Build request to video service — PR #189: amount frontend-dən gəlir
	// PR #191: appIDExternal artıq UUID (app.PublicID) — numeric INT deyil
	appIDExternal := app.PublicID
	videoReq := &model.CreateVideoOrderRequest{
		AppID:       appIDExternal,
		Phone:       app.CustomerPhone,
		Amount:      amount,
		WebhookURL:  s.videoRecordWebhookURL,
		RedirectURL: s.videoRecordRedirectURL,
		Name:        truncateVideoField(customerName, 100),
		Lang:        "az",
		City:        "",
		// PR #399: faktiki ünvan (ekspert tərəfindən doldurulur) — video service
		// order-in address sahəsinə ötürülür. Boş olsa JSON-dan düşür (omitempty).
		// PR #400: "too long" validasiya xətalarına qarşı rune limiti ilə kəsilir.
		Address: truncateVideoField(app.ActualAddress, 150),
		Salary:  0,
	}

	// PR #260: SetAuditAppID shared mutable state race yaradırdı (PR #259 analizindən).
	// Əvəzinə context value istifadə olunur — thread-safe.
	appIDPtr := appID
	ctx = videorecord.WithAppID(ctx, &appIDPtr)

	// 4. Call video service
	resp, err := s.videoRecordProvider.CreateOrder(ctx, videoReq)
	if err != nil {
		return "", fmt.Errorf("video service call failed: %w", err)
	}

	// PR #401: video service redirect_url-i sondaki "/" ilə qaytarır —
	// səhifə slash-sız açıldığından link kəsilir (iframe, SMS, DB — hamısı üçün).
	resp.RedirectURL = strings.TrimRight(resp.RedirectURL, "/")

	// 5. Build raw JSON for storage
	reqBodyJSON, _ := json.Marshal(videoReq)
	respBodyJSON, _ := json.Marshal(resp)

	// 6. Store in DB
	vr := &model.VideoRecord{
		ApplicationID:    appID,
		AppIDExternal:    appIDExternal,
		OrderRedirectURL: resp.RedirectURL,
		Phone:            app.CustomerPhone,
		Amount:           amount,
		CustomerName:     customerName,
		RequestBody:      string(reqBodyJSON),
		ResponseBody:     string(respBodyJSON),
	}
	if err := s.videoRecordRepo.Insert(ctx, vr); err != nil {
		slog.Warn("video record: failed to store audit row", "error", err)
		// don't fail the request — we have the redirect_url
	}

	slog.Info("video record order started",
		"application_id", appID,
		"app_id_external", appIDExternal,
		"customer_name", customerName,
		"amount", amount,
		"redirect_url", resp.RedirectURL)

	return resp.RedirectURL, nil
}

// CheckVideoRecordStatus polls the video service for the recorded flag.
// PR #188: frontend hər 2 saniyədən bir çağırır.
// If recorded=true, updates the DB row.
func (s *ApplicationService) CheckVideoRecordStatus(ctx context.Context, appID int) (bool, error) {
	if !s.IsVideoRecordEnabled() {
		return false, fmt.Errorf("video record deaktiv")
	}

	// Fetch existing video record row
	vr, err := s.videoRecordRepo.GetByApplication(ctx, appID)
	if err != nil {
		return false, fmt.Errorf("failed to get video record: %w", err)
	}
	if vr == nil {
		return false, fmt.Errorf("video record order not found for application %d", appID)
	}

	// Build status request
	statusReq := model.VideoOrderStatusRequest{AppIDs: []string{vr.AppIDExternal}}
	reqBodyJSON, _ := json.Marshal(statusReq)

	// PR #260: SetAuditAppID → WithAppID (context value, thread-safe)
	appIDPtr := appID
	ctx = videorecord.WithAppID(ctx, &appIDPtr)

	// Call video service
	statusResp, err := s.videoRecordProvider.CheckStatus(ctx, []string{vr.AppIDExternal})
	if err != nil {
		return false, fmt.Errorf("video status check failed: %w", err)
	}

	respBodyJSON, _ := json.Marshal(statusResp)

	// Find recorded flag for our app_id
	recorded := false
	for _, r := range statusResp.Results {
		if r.AppID == vr.AppIDExternal {
			recorded = r.Recorded
			break
		}
	}

	// Update DB
	if err := s.videoRecordRepo.UpdateStatus(ctx, appID, recorded, string(reqBodyJSON), string(respBodyJSON)); err != nil {
		slog.Warn("video record: failed to update status", "error", err)
	}

	slog.Info("video record status checked",
		"application_id", appID,
		"recorded", recorded)

	return recorded, nil
}

// IsVideoRecorded checks whether video has been recorded for the application.
// PR #188: confirm düyməsini aktivləşdirmək üçün yoxlanılır.
// Returns true if video is disabled (no requirement) OR if video has been recorded.
func (s *ApplicationService) IsVideoRecorded(ctx context.Context, appID int) (bool, error) {
	if !s.IsVideoRecordEnabled() {
		return true, nil // no requirement
	}
	return s.videoRecordRepo.IsRecorded(ctx, appID)
}

// GetVideoRecordRedirectURL returns the redirect_url for an existing video order.
// PR #188: əgər müştəri modalı bağlayıb yenidən açmaq istəsə, redirect_url qaytarırıq.
func (s *ApplicationService) GetVideoRecordRedirectURL(ctx context.Context, appID int) (string, bool, error) {
	if !s.IsVideoRecordEnabled() {
		return "", true, nil
	}
	vr, err := s.videoRecordRepo.GetByApplication(ctx, appID)
	if err != nil {
		return "", false, err
	}
	if vr == nil {
		return "", false, fmt.Errorf("video record order not found")
	}
	// PR #401: köhnə DB sətirlərində sondaki "/" ola bilər — oxuyarkən kəsilir
	return strings.TrimRight(vr.OrderRedirectURL, "/"), vr.Recorded, nil
}

// SendVideoRecordSMS — PR #399: dashboard "Video müraciət göndər" axını.
//  1. Video service-də order yaradır (amount = approved_amount, boşdursa amount —
//     karta köçürüləcək kredit məbləği), 2) qayıdan redirect_url-i müştərinin
//
// OTP-təsdiqli nömrəsinə SMS ilə göndərir (movcud SMS provider üzərindən).
// SMS mətni maksimum 160 simvol (GSM-7) saxlanılır.
func (s *ApplicationService) SendVideoRecordSMS(ctx context.Context, appID int) (string, error) {
	if !s.IsVideoRecordEnabled() {
		return "", fmt.Errorf("video record deaktiv")
	}

	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return "", fmt.Errorf("application not found: %d", appID)
	}
	if app.CustomerPhone == "" {
		return "", fmt.Errorf("customer_phone tapılmadı — müraciətdə OTP-təsdiqli nömrə yoxdur")
	}
	if s.smsProvider == nil {
		return "", fmt.Errorf("SMS provider konfiqurasiya olunmayıb")
	}

	// Karta köçürüləcək kredit məbləği: təsdiq olunubsa approved_amount,
	// hələ yoxdursa istənilən məbləğ (amount)
	amount := app.ApprovedAmount
	if amount <= 0 {
		amount = app.Amount
	}

	// Order yaradılır (audit video_records cədvəlinə yazılır)
	redirectURL, err := s.StartVideoRecord(ctx, appID, amount)
	if err != nil {
		return "", err
	}

	// SMS — maks 160 simvol (nümunə URL ~97 simvol, cəm ~156)
	msg := fmt.Sprintf("Video eynilesdirme ucun linke kecid etmeyiniz xahis olunur. %s", redirectURL)
	if len(msg) > 160 {
		slog.Warn("video record SMS exceeds 160 chars — gateway may split it",
			"application_id", appID, "length", len(msg))
	}
	if err := s.smsProvider.Send(ctx, app.CustomerPhone, msg); err != nil {
		slog.Error("video record: failed to send SMS",
			"application_id", appID,
			"phone", app.CustomerPhone,
			"error", err)
		return "", fmt.Errorf("SMS göndərilə bilmədi: %w", err)
	}

	slog.Info("video record SMS sent",
		"application_id", appID,
		"phone", app.CustomerPhone,
		"amount", amount,
		"chars", len(msg))

	return redirectURL, nil
}

// truncateVideoField — PR #400: video service tərəfindən "too long" xətası
// qaytarılan sahələri (name, address) maksimal rune sayına kəsir. Azərbaycan
// hərfləri (ə, ü, ö...) çoxbaytlı olduğundan bayt yox, rune hesabı ilə kəsilir.
func truncateVideoField(s string, maxRunes int) string {
	if s == "" || maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

// GetVideoStreamURL — PR #399: dashboard "Videya bax" dialoqu üçün stream linki.
// {VIDEO_URL}/video/{video_application_id}/stream formatında qurulur;
// video_application_id = video service-ə göndərilən app_id (= app.PublicID).
func (s *ApplicationService) GetVideoStreamURL(ctx context.Context, appID int) (string, error) {
	if s.videoStreamBaseURL == "" {
		return "", fmt.Errorf("video stream base URL konfiqurasiya olunmayıb")
	}

	vr, err := s.videoRecordRepo.GetByApplication(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("failed to get video record: %w", err)
	}
	if vr == nil {
		return "", fmt.Errorf("video order tapılmadı — əvvəlcə \"Video müraciət göndər\" düyməsini işlədin")
	}

	return strings.TrimRight(s.videoStreamBaseURL, "/") + "/video/" + vr.AppIDExternal + "/stream", nil
}
