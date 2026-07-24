package service

import (
        "context"
        "encoding/json"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
        "rdc-source/pkg/mygov"
)

// PR #65: Employment and pension verification service.
//
// This service implements the business rules described in PR #63:
//
//  1. Employment verification (6-month tenure rule):
//     - Current job tenure >= 6 months (30-day months) → PASS
//     - Else if previous job exists AND gap < 29 days:
//       combined tenure (current + previous) >= 6 months → PASS
//     - Else → FAIL → auto-reject
//
//  2. Pension verification (1st-group disability rule):
//     - DisabilityGroup == 1 → auto-reject
//     - Else → PASS
//
// Staj calculation uses 30-day months (per business: "1-30" means 30 days = 1 month).
// Gap rule: "< 29" means gap must be strictly less than 29 days to count previous job.

// MyGovVerifyResponse is returned by the employment and pension verify endpoints.
type MyGovVerifyResponse struct {
        ApplicationID int    `json:"application_id"`
        Verified      bool   `json:"verified"`
        Status        string `json:"status"`         // "passed", "rejected", "pending"
        Reason        string `json:"reason,omitempty"`
        CheckType     string `json:"check_type"`     // "employment" or "pension"
}

// RequestEmploymentVerification generates a MyGov permission link for employment
// data and sends it via SMS to the customer. The customer must open the link,
// grant permission in the MyGov app, then the expert calls VerifyEmployment.
//
// This is a thin wrapper around the existing GenerateLink method — it just
// records the check type so the verify step knows which data to look at.
func (s *MyGovService) RequestEmploymentVerification(ctx context.Context, appID int) (*model.MyGovPermissionResponse, error) {
        slog.Info("employment verification requested",
                "application_id", appID,
                "check_type", "employment")
        // The GenerateLink method already sends the SMS with the MyGov deeplink.
        // The check_type is implicit: the expert knows they clicked "employment".
        return s.GenerateLink(ctx, appID, s.getCustomerPIN(ctx, appID))
}

// RequestPensionVerification generates a MyGov permission link for pension
// data and sends it via SMS to the customer. Same flow as employment, just
// a different semantic label.
func (s *MyGovService) RequestPensionVerification(ctx context.Context, appID int) (*model.MyGovPermissionResponse, error) {
        slog.Info("pension verification requested",
                "application_id", appID,
                "check_type", "pension")
        return s.GenerateLink(ctx, appID, s.getCustomerPIN(ctx, appID))
}

// VerifyEmployment fetches MyGov data and runs the 6-month tenure rule.
// If the rule fails, the application is auto-rejected with a descriptive reason.
func (s *MyGovService) VerifyEmployment(ctx context.Context, appID int) (*MyGovVerifyResponse, error) {
        // 1. Fetch MyGov data (must have been fetched via FetchData first)
        data, err := s.getAuthorizedData(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to get MyGov data: %w", err)
        }

        // 2. Run the 6-month tenure rule
        passed, reason := checkEmploymentTenure(data.WorkHistory)

        resp := &MyGovVerifyResponse{
                ApplicationID: appID,
                Verified:      passed,
                CheckType:     "employment",
        }

        if !passed {
                // Auto-reject the application
                resp.Status = "rejected"
                resp.Reason = reason
                s.autoReject(ctx, appID, "EMPLOYMENT_TENURE")
        } else {
                resp.Status = "passed"
                resp.Reason = reason
        }

        slog.Info("employment verification completed",
                "application_id", appID,
                "passed", passed,
                "reason", reason)

        return resp, nil
}

// VerifyPension fetches MyGov data and checks for 1st-group disability.
// If DisabilityGroup == 1, the application is auto-rejected.
func (s *MyGovService) VerifyPension(ctx context.Context, appID int) (*MyGovVerifyResponse, error) {
        // 1. Fetch MyGov data
        data, err := s.getAuthorizedData(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to get MyGov data: %w", err)
        }

        // 2. Check disability group
        passed := data.DisabilityGroup != 1
        var reason string
        if passed {
                reason = "No 1st-group disability found"
        } else {
                reason = "DISABILITY_GROUP1"
        }

        resp := &MyGovVerifyResponse{
                ApplicationID: appID,
                Verified:      passed,
                CheckType:     "pension",
        }

        if !passed {
                resp.Status = "rejected"
                resp.Reason = reason
                s.autoReject(ctx, appID, "DISABILITY_GROUP1")
        } else {
                resp.Status = "passed"
                resp.Reason = reason
        }

        slog.Info("pension verification completed",
                "application_id", appID,
                "passed", passed,
                "disability_group", data.DisabilityGroup,
                "is_pensioner", data.IsPensioner)

        return resp, nil
}

// checkEmploymentTenure implements the 6-month tenure rule (PR #65).
//
// Rules (per business):
//   - Tenure is measured in 30-day months (30 days = 1 month)
//   - Current job tenure >= 6 months → PASS
//   - Else if previous job exists AND gap < 29 days:
//     combined (current + previous) >= 6 months → PASS
//   - Else → FAIL
//
// Returns (passed bool, reason string).
func checkEmploymentTenure(workHistory []mygov.WorkPlace) (bool, string) {
        if len(workHistory) == 0 {
                return false, "İş yeri məlumatı tapılmadı (WorkHistory boşdur)"
        }

        now := time.Now()
        current := workHistory[0] // first entry is current job (EndDate == nil)
        if current.EndDate != nil {
                return false, "Cari iş yeri tapılmadı (ilk entry-nin EndDate-i doludur)"
        }

        // Calculate current job tenure in 30-day months
        currentDays := now.Sub(current.StartDate).Hours() / 24
        currentMonths := currentDays / 30

        if currentMonths >= 6 {
                return true, fmt.Sprintf("Cari iş yerində staj %.1f ay (≥ 6 ay) — uyğundur", currentMonths)
        }

        // Current tenure < 6 months — check previous job
        if len(workHistory) < 2 {
                return false, fmt.Sprintf("Cari iş yerində staj %.1f ay (< 6 ay) və əvvəlki iş yeri yoxdur — imtina", currentMonths)
        }

        prev := workHistory[1]
        if prev.EndDate == nil {
                return false, "Əvvəlki iş yerinin EndDate-i boşdur (məlumat səhvdir)"
        }

        // Calculate gap between previous job end and current job start
        gapDays := current.StartDate.Sub(*prev.EndDate).Hours() / 24
        if gapDays < 0 {
                return false, "Əvvəlki iş yeri cari işdən sonra bitib (tarixlər düzgün deyil)"
        }

        // Gap must be < 29 days to count previous job
        if gapDays >= 29 {
                return false, fmt.Sprintf("Cari staj %.1f ay, əvvəlki iş yerinə fasilə %.0f gün (≥ 29 gün) — əvvəlki iş nəzərə alınmır, imtina",
                        currentMonths, gapDays)
        }

        // Gap < 29 — combine current + previous tenure
        prevDays := prev.EndDate.Sub(prev.StartDate).Hours() / 24
        prevMonths := prevDays / 30
        combinedMonths := currentMonths + prevMonths

        if combinedMonths >= 6 {
                return true, fmt.Sprintf("Cari staj %.1f ay + əvvəlki %.1f ay = %.1f ay (fasilə %.0f gün < 29) — uyğundur",
                        currentMonths, prevMonths, combinedMonths, gapDays)
        }

        return false, fmt.Sprintf("Cari staj %.1f ay + əvvəlki %.1f ay = %.1f ay (< 6 ay) — imtina",
                currentMonths, prevMonths, combinedMonths)
}

// getAuthorizedData reads the stored MyGov data from the DB and unmarshals it.
// Returns an error if no data has been fetched yet.
func (s *MyGovService) getAuthorizedData(ctx context.Context, appID int) (*mygov.AuthorizedData, error) {
        perm, err := s.repo.GetByApplicationID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("failed to get MyGov permission: %w", err)
        }
        if perm.DataJSON == "" {
                return nil, fmt.Errorf("MyGov data not yet fetched — call FetchData first")
        }
        var data mygov.AuthorizedData
        if err := json.Unmarshal([]byte(perm.DataJSON), &data); err != nil {
                return nil, fmt.Errorf("failed to parse MyGov data: %w", err)
        }
        return &data, nil
}

// autoReject marks the application as rejected with the given reason.
// Used when employment or pension verification fails.
// PR #85: Also sends SMS to the customer informing them of the rejection.
func (s *MyGovService) autoReject(ctx context.Context, appID int, reason string) {
        app, err := s.appRepo.GetApplicationByID(ctx, appID)
        if err != nil {
                slog.Error("auto-reject: failed to get application",
                        "application_id", appID,
                        "error", err)
                return
        }

        // Don't override a final status (approved/rejected already set)
        if app.Status == model.StatusApproved || app.Status == model.StatusRejected {
                slog.Warn("auto-reject: application already in final status, skipping",
                        "application_id", appID,
                        "current_status", app.Status)
                return
        }

        if err := s.appRepo.UpdateApplicationDecision(ctx, appID,
                model.StatusRejected, app.CreditLevel, reason,
                app.ApprovedAmount, app.ApprovedRate, app.TotalAmount); err != nil {
                slog.Error("auto-reject: failed to update application status",
                        "application_id", appID,
                        "error", err)
                return
        }

        slog.Info("application auto-rejected by MyGov verification",
                "application_id", appID,
                "reason", reason)

        // PR #85: Send SMS to customer about the rejection
        if app.CustomerPhone != "" {
                smsMessage := "Hormetli musteri, sizin kredit muracietiniz heyata kecirilmeyib. Etrafli melumat ucun 157."
                if err := s.smsProvider.Send(ctx, app.CustomerPhone, smsMessage); err != nil {
                        slog.Error("auto-reject: failed to send rejection SMS",
                                "application_id", appID,
                                "phone", app.CustomerPhone,
                                "error", err)
                } else {
                        slog.Info("rejection SMS sent to customer",
                                "application_id", appID,
                                "phone", app.CustomerPhone)
                }
        }
}

// getCustomerPIN fetches the customer PIN for an application.
func (s *MyGovService) getCustomerPIN(ctx context.Context, appID int) string {
        app, err := s.appRepo.GetApplicationByID(ctx, appID)
        if err != nil {
                return ""
        }
        return app.CustomerPIN
}
