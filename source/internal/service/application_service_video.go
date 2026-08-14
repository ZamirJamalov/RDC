package service

import (
        "context"
        "encoding/json"
        "fmt"
        "log/slog"
        "strconv"

        "rdc-source/internal/model"
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

        // 2. Get customer name from AZMK (GetPersonalInfo returns Name + Surname + Patronymic)
        customerName := ""
        if s.customerDataProvider != nil {
                data, err := s.customerDataProvider.GetPersonalInfo(ctx, app.CustomerPIN, app.CustomerSerial)
                if err != nil {
                        slog.Warn("video record: failed to fetch customer name from AZMK — using empty", "error", err)
                } else if data != nil {
                        customerName = data.FullName()
                }
        }

        // 3. Build request to video service — PR #189: amount frontend-dən gəlir
        appIDExternal := strconv.Itoa(appID)
        videoReq := &model.CreateVideoOrderRequest{
                AppID:       appIDExternal,
                Phone:       app.CustomerPhone,
                Amount:      amount,
                WebhookURL:  s.videoRecordWebhookURL,
                RedirectURL: s.videoRecordRedirectURL,
                Name:        customerName,
                Lang:        "az",
                City:        "",
                Address:     "",
                Salary:      0,
        }

        // Set audit appID for HTTP provider
        if htp, ok := s.videoRecordProvider.(interface{ SetAuditAppID(*int) }); ok {
                appIDPtr := appID
                htp.SetAuditAppID(&appIDPtr)
        }

        // 4. Call video service
        resp, err := s.videoRecordProvider.CreateOrder(ctx, videoReq)
        if err != nil {
                return "", fmt.Errorf("video service call failed: %w", err)
        }

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

        // Set audit appID for HTTP provider
        if htp, ok := s.videoRecordProvider.(interface{ SetAuditAppID(*int) }); ok {
                appIDPtr := appID
                htp.SetAuditAppID(&appIDPtr)
        }

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
        return vr.OrderRedirectURL, vr.Recorded, nil
}
