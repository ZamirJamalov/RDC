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
//     - Staj = Contract.BeginDate (başlama tarixi) → bu gün, 30 günlük aylarla (PR #255)
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
	Status        string `json:"status"` // "passed", "rejected", "pending"
	Reason        string `json:"reason,omitempty"`
	CheckType     string `json:"check_type"` // "employment" or "pension"
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
//   - Müddət = Contract.BeginDate (başlama tarixi) → bu gün, 30 günlük aylarla (PR #255)
//   - Müddət >= 6 ay → PASS, əks halda avtomatik imtina (EMPLOYMENT_TENURE)
//
// PR #242: nəticə cutoff_results cədvəlinə yazılır (plan/fakt).
func (s *MyGovService) VerifyEmployment(ctx context.Context, appID int) (*MyGovVerifyResponse, error) {
	// 1. Get the customer's PIN
	pin := s.getCustomerPIN(ctx, appID)
	if pin == "" {
		return nil, fmt.Errorf("customer PIN not found for application %d", appID)
	}

	// 2. Fetch employment records from AZMK CustomerDataService (PR #239)
	pin, serial := s.getCustomerPINAndSerial(ctx, appID)
	serviceName := "AZMK_GET_EMPLOYEE_INFO"
	var info *mygov.EmployeeInfoResponse
	var err error
	if s.customerDataProvider != nil {
		info, err = s.customerDataProvider.GetEmployeeInfoByPin(ctx, pin, serial)
	} else {
		// Fallback: MyGov provider (localhost:8083)
		serviceName = "MYGOV_GET_EMPLOYEE_INFO"
		info, err = s.provider.GetEmployeeInfoByPin(ctx, pin)
	}
	if err != nil {
		// PR #242: servis xətası da cutoff_results-a yazılır (checked=false)
		s.logCutoff(ctx, appID, "EMPLOYMENT_TENURE", "İş yerində minimum 6 ay staj", serviceName,
			false, false, "", ">= 6 ay", fmt.Sprintf("service error: %v", err))
		return nil, fmt.Errorf("GetEmployeeInfoByPin failed: %w", err)
	}

	// 3. Run the EMPLOYMENT_TENURE cutoff check
	passed, months, reason := checkEmploymentTenureFromEmployeeInfo(info, s.employmentTenureMinMonths)

	// PR #242: nəticəni cutoff_results-a yaz
	actualValue := ""
	if months >= 0 {
		actualValue = fmt.Sprintf("%.1f ay", months)
	}
	s.logCutoff(ctx, appID, "EMPLOYMENT_TENURE", fmt.Sprintf("İş yerində minimum %d ay staj", s.employmentTenureMinMonths), serviceName,
		true, passed, actualValue, fmt.Sprintf(">= %d ay", s.employmentTenureMinMonths), reason)

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

// VerifyPension fetches pension/disability data via GetPensionInfoByPin (PR #242 —
// PIN üzərindən, permission token tələb olunmur; iş yeri yoxlaması ilə eyni qayda)
// and checks for 1st-group disability. If DisabilityGroup == 1, the application
// is auto-rejected. Nəticə cutoff_results cədvəlinə yazılır (DISABILITY_GROUP1).
//
// Mənbə prioriteti:
//  1. AZMK CustomerDataService — GetPensionInfoByPin(fin, serial)
//  2. Fallback: saxlanmış MyGov AuthorizedData (permission flow ilə əvvəl çəkilmişibsə)
func (s *MyGovService) VerifyPension(ctx context.Context, appID int) (*MyGovVerifyResponse, error) {
	// 1. Get the customer's PIN + serial
	pin, serial := s.getCustomerPINAndSerial(ctx, appID)
	if pin == "" {
		return nil, fmt.Errorf("customer PIN not found for application %d", appID)
	}

	// 2. Fetch pension/disability data — AZMK first (PR #242)
	serviceName := "AZMK_GET_PENSION_INFO"
	var pension *mygov.PensionInfoResponse
	var azmkErr error
	if s.customerDataProvider != nil {
		pension, azmkErr = s.customerDataProvider.GetPensionInfoByPin(ctx, pin, serial)
		if azmkErr != nil {
			slog.Warn("pension verify: AZMK GetPensionInfoByPin failed — trying stored MyGov data",
				"application_id", appID, "error", azmkErr)
		}
	}
	if pension == nil {
		// Fallback: əvvəl çəkilmiş MyGov authorized data (köhnə permission flow)
		data, err := s.getAuthorizedData(ctx, appID)
		if err != nil {
			if azmkErr != nil {
				s.logCutoff(ctx, appID, "DISABILITY_GROUP1", "1-ci qrup əlillik olduqda imtina", serviceName,
					false, false, "", "disability_group != 1",
					fmt.Sprintf("AZMK error: %v; MyGov fallback error: %v", azmkErr, err))
				return nil, fmt.Errorf("GetPensionInfoByPin failed: %v; MyGov fallback failed: %w", azmkErr, err)
			}
			s.logCutoff(ctx, appID, "DISABILITY_GROUP1", "1-ci qrup əlillik olduqda imtina", serviceName,
				false, false, "", "disability_group != 1",
				fmt.Sprintf("service error: %v", err))
			return nil, fmt.Errorf("failed to get pension data: %w", err)
		}
		pension = mygov.PensionInfoFromAuthorizedData(data)
		serviceName = "MYGOV_AUTHORIZED_DATA"
	}
	if pension == nil || pension.Data == nil || pension.Data.Response == nil {
		s.logCutoff(ctx, appID, "DISABILITY_GROUP1", "1-ci qrup əlillik olduqda imtina", serviceName,
			false, false, "", "disability_group != 1", "pension data missing in response")
		return nil, fmt.Errorf("pension data missing in GetPensionInfoByPin response")
	}

	// 3. Check disability group
	disabilityGroup := pension.Data.Response.DisabilityGroup
	isPensioner := pension.Data.Response.IsPensioner
	passed := disabilityGroup != 1
	var reason string
	if passed {
		reason = "No 1st-group disability found"
	} else {
		reason = "DISABILITY_GROUP1"
	}

	// PR #242: nəticəni cutoff_results-a yaz
	s.logCutoff(ctx, appID, "DISABILITY_GROUP1", "1-ci qrup əlillik olduqda imtina", serviceName,
		true, passed,
		fmt.Sprintf("disability_group = %d, is_pensioner = %v", disabilityGroup, isPensioner),
		"disability_group != 1", reason)

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
		"source", serviceName,
		"disability_group", disabilityGroup,
		"is_pensioner", isPensioner)

	return resp, nil
}

// checkEmploymentTenureFromEmployeeInfo implements the EMPLOYMENT_TENURE cutoff
// (PR #237) from the real MLSA GetEmployeeInfoByPin response.
//
// Rules (per business):
//   - Active bölməsində iş yeri məlumatı olmalıdır (boşdursa → imtina)
//   - Əsas iş yeri seçilir (WorkPlaceType.Label == "1"); yoxdursa ilk Active qeydi
//   - Staj = Contract.BeginDate (başlama tarixi) → bu gün, 30 günlük aylarla hesablanır
//     (BeginDate boşdursa SignDate istifadə olunur — PR #255)
//   - Staj >= 6 ay → PASS, əks halda FAIL
//
// PR #242: months da qaytarılır (cutoff_results.actual_value üçün);
// tarix hesablana bilmədikdə months = -1.
//
// Returns (passed bool, months float64, reason string).
func checkEmploymentTenureFromEmployeeInfo(info *mygov.EmployeeInfoResponse, minMonths int) (bool, float64, string) {
	if info == nil || info.Data == nil || info.Data.Response == nil {
		return false, -1, "İş yeri məlumatı tapılmadı (cavab boşdur)"
	}

	active := info.Data.Response.Active
	deactive := info.Data.Response.Deactive

	// PR #277: Aktiv iş yeri yoxdursa — deaktiv qeydlərə baxıb aydın mesaj ver.
	// İstifadəçi heç vaxt işləməyibsə (hər ikisi boş) və ya işləyib amma
	// hazırda deaktivdirsə — cutoff reject edir (EMPLOYMENT_TENURE).
	if len(active) == 0 {
		if len(deactive) > 0 {
			return false, -1, "Aktiv iş yeri yoxdur — əvvəlki iş yerləri deaktivdir (EMPLOYMENT_TENURE)"
		}
		return false, -1, "Aktiv iş yeri yoxdur — iş məlumatı tapılmadı (heç vaxt işləməyib, EMPLOYMENT_TENURE)"
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
		return false, -1, "İş müqaviləsi məlumatı tapılmadı (Contract boşdur)"
	}

	employerName := ""
	if record.Employer != nil && record.Employer.Name != "" {
		employerName = record.Employer.Name
	}

	// PR #255: Başlama tarixi (BeginDate) əsas; boşdursa SignDate fallback.
	// Business: BeginDate işçinin işə başladığı tarixi göstərir (staj hesabı üçün düzgün anchor).
	dateStr := record.Contract.BeginDate
	dateKind := "başlama tarixi"
	if dateStr == "" {
		dateStr = record.Contract.SignDate
		dateKind = "imza tarixi"
	}
	if dateStr == "" {
		return false, -1, "Müqavilə tarixi tapılmadı (BeginDate/SignDate boşdur)"
	}

	signDate, err := time.Parse("02.01.2006", dateStr)
	if err != nil {
		return false, -1, fmt.Sprintf("Müqavilə tarixi formatı düzgün deyil: %s", dateStr)
	}

	// Staj: başlama tarixindən bu günə, 30 günlük aylarla ("kesim" həddi 6 ay) — PR #255
	months := time.Since(signDate).Hours() / 24 / 30

	if months >= float64(minMonths) {
		return true, months, fmt.Sprintf("İş yerində staj %.1f ay (%s, %s — ≥ %d ay) — uyğundur",
			months, dateKind, employerName, minMonths)
	}
	return false, months, fmt.Sprintf("İş yerində staj %.1f ay (< %d ay) — imtina (EMPLOYMENT_TENURE)%s",
		months, minMonths, employerSuffix(employerName))
}

// PR #279: employmentTenureMinMonths artıq MyGovService field-dır (config: EMPLOYMENT_TENURE_MIN_MONTHS)

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

// logCutoff writes a cutoff check result to the database (PR #242).
// VerifyEmployment (EMPLOYMENT_TENURE) və VerifyPension (DISABILITY_GROUP1)
// nəticələri cutoff_results cədvəlinə yazılır — ApplicationService.logCutoff
// ilə eyni strukturda (plan/fakt hesabatları üçün).
func (s *MyGovService) logCutoff(ctx context.Context, appID int, code, name, service string, checked, passed bool, actualValue, threshold, details string) {
	if s.cutoffRepo == nil {
		return
	}
	cr := &model.CutoffResult{
		ApplicationID: appID,
		CutoffCode:    code,
		CutoffName:    name,
		ServiceName:   service,
		Checked:       checked,
		Passed:        passed,
		ActualValue:   actualValue,
		Threshold:     threshold,
		Details:       details,
	}
	if err := s.cutoffRepo.Insert(ctx, cr); err != nil {
		slog.Warn("failed to log cutoff result", "error", err, "cutoff_code", code)
	}
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

// getCustomerPINAndSerial fetches the customer PIN and serial for an application.
// PR #239: AZMK GetEmployeeInfoByPin həm FIN, həm serial tələb edir.
func (s *MyGovService) getCustomerPINAndSerial(ctx context.Context, appID int) (string, string) {
	app, err := s.appRepo.GetApplicationByID(ctx, appID)
	if err != nil {
		return "", ""
	}
	return app.CustomerPIN, app.CustomerSerial
}

// getCustomerPIN fetches the customer PIN for an application.
func (s *MyGovService) getCustomerPIN(ctx context.Context, appID int) string {
	pin, _ := s.getCustomerPINAndSerial(ctx, appID)
	return pin
}
