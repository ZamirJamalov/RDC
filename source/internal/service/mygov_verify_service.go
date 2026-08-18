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
//  1. Employment verification (EMPLOYMENT_TENURE cutoff, PR #237):
//     - Data source: MLSA GetEmployeeInfoByPin (Active/Deactive records)
//     - Active bölməsində iş yeri olmalıdır
//     - Staj = Contract.SignDate (imza tarixi) → bu gün, 30 günlük aylarla
//     - Staj >= 6 ay → PASS; əks halda → FAIL → auto-reject
//
//  2. Pension verification (1st-group disability rule):
//     - DisabilityGroup == 1 → auto-reject
//     - Else → PASS
//
// Staj calculation uses 30-day months (per business: "1-30" means 30 days = 1 month).

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

// VerifyEmployment fetches MLSA employment data via GetEmployeeInfoByPin and runs
// the EMPLOYMENT_TENURE cutoff rule (PR #237):
//   - Active bölməsində iş yeri məlumatı olmalıdır
//   - Müddət = Contract.SignDate (imza tarixi) → bu gün, 30 günlük aylarla
//   - Müddət >= 6 ay → PASS, əks halda avtomatik imtina (EMPLOYMENT_TENURE)
func (s *MyGovService) VerifyEmployment(ctx context.Context, appID int) (*MyGovVerifyResponse, error) {
        // 1. Get the customer's PIN
        pin := s.getCustomerPIN(ctx, appID)
        if pin == "" {
                return nil, fmt.Errorf("customer PIN not found for application %d", appID)
        }

        // 2. Fetch employment records from the MLSA service
        info, err := s.provider.GetEmployeeInfoByPin(ctx, pin)
        if err != nil {
                return nil, fmt.Errorf("GetEmployeeInfoByPin failed: %w", err)
        }

        // 3. Run the EMPLOYMENT_TENURE cutoff check
        passed, reason := checkEmploymentTenureFromEmployeeInfo(info)

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

// checkEmploymentTenureFromEmployeeInfo implements the EMPLOYMENT_TENURE cutoff
// (PR #237) from the real MLSA GetEmployeeInfoByPin response.
//
// Rules (per business):
//   - Active bölməsində iş yeri məlumatı olmalıdır (boşdursa → imtina)
//   - Əsas iş yeri seçilir (WorkPlaceType.Label == "1"); yoxdursa ilk Active qeydi
//   - Staj = Contract.SignDate (imza tarixi) → bu gün, 30 günlük aylarla hesablanır
//     (SignDate boşdursa BeginDate istifadə olunur)
//   - Staj >= 6 ay → PASS, əks halda FAIL
//
// Returns (passed bool, reason string).
func checkEmploymentTenureFromEmployeeInfo(info *mygov.EmployeeInfoResponse) (bool, string) {
        if info == nil || info.Data == nil || info.Data.Response == nil {
                return false, "İş yeri məlumatı tapılmadı (cavab boşdur)"
        }

        active := info.Data.Response.Active
        if len(active) == 0 {
                return false, "Aktiv iş yeri tapılmadı (Active bölməsi boşdur)"
        }

        // Əsas iş yerini seç (WorkPlaceType Label "1" = Əsas); yoxdursa ilk qeyd
        record := active[0]
        for _, a := range active {
                if a.IsMainJob() {
                        record = a
                        break
                }
        }
        if record.Contract == nil {
                return false, "İş müqaviləsi məlumatı tapılmadı (Contract boşdur)"
        }

        employerName := ""
        if record.Employer != nil && record.Employer.Name != "" {
                employerName = record.Employer.Name
        }

        // İmza tarixi (SignDate); boşdursa BeginDate fallback
        dateStr := record.Contract.SignDate
        dateKind := "imza tarixi"
        if dateStr == "" {
                dateStr = record.Contract.BeginDate
                dateKind = "başlama tarixi"
        }
        if dateStr == "" {
                return false, "Müqavilə tarixi tapılmadı (SignDate/BeginDate boşdur)"
        }

        signDate, err := time.Parse("02.01.2006", dateStr)
        if err != nil {
                return false, fmt.Sprintf("Müqavilə tarixi formatı düzgün deyil: %s", dateStr)
        }

        // Staj: imza tarixindən bu günə, 30 günlük aylarla ("kesim" həddi 6 ay)
        months := time.Since(signDate).Hours() / 24 / 30

        if months >= employmentTenureMinMonths {
                return true, fmt.Sprintf("İş yerində staj %.1f ay (%s, %s — ≥ 6 ay) — uyğundur",
                        months, dateKind, employerName)
        }
        return false, fmt.Sprintf("İş yerində staj %.1f ay (< 6 ay) — imtina (EMPLOYMENT_TENURE)%s",
                months, employerSuffix(employerName))
}

// employmentTenureMinMonths is the EMPLOYMENT_TENURE cutoff threshold.
const employmentTenureMinMonths = 6

// employerSuffix formats the employer name for reason messages.
func employerSuffix(employerName string) string {
        if employerName == "" {
                return ""
        }
        return fmt.Sprintf(" — %s", employerName)
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
