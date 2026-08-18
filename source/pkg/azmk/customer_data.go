package azmk

import (
        "context"
        "database/sql"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "log/slog"
        "net/http"
        "strconv"
        "strings"
        "time"

        "rdc-source/pkg/mygov"
)

// PR #152: AZMK CustomerDataService — yaş yoxlaması üçün şəxsi məlumat servisi.
//
// Real servis: https://web.azmk.az:7077/LW_CREDIT_HOUSE/services/CustomerDataService
// Request: POST { "requestType": "GetPersonalInfo", "finCode": "...", "serialNumber": "..." }
// Response: { "result": 1, "data": { "BirthDate": "1993-08-09", "Name": "...", ... } }
//
// Mock modunda FIN koduna görə fərqli şəxslər imitasiya olunur (finScenarios map).

// CustomerDataProvider is the interface for AZMK CustomerDataService.
type CustomerDataProvider interface {
        GetPersonalInfo(ctx context.Context, finCode, serialNumber string) (*CustomerData, error)
        // PR #159: getOwnerData — qara siyahı, aktiv kredit, gecikmə məlumatları
        GetOwnerData(ctx context.Context, finCode, serialNumber string) (*OwnerData, error)
        // PR #160: getMkrScore — AKB skoru və stop-faktor (response) yoxlaması
        GetMkrScore(ctx context.Context, finCode, serialNumber string) (*MkrScore, error)
        // PR #165: inquireByIdCard — kredit tarixçəsi və gecikmə kesim nöqtələri
        InquireByIdCard(ctx context.Context, finCode, serialNumber string) (*CreditHistory, error)
        // PR #239: getEmployeeInfoByPin — iş yeri məlumatları (AZMK CustomerDataService)
        GetEmployeeInfoByPin(ctx context.Context, finCode, serialNumber string) (*mygov.EmployeeInfoResponse, error)
}

// --- PR #165: inquireByIdCard ---

// InquireByIdCardRequest is the request body for inquireByIdCard.
type InquireByIdCardRequest struct {
        RequestType  string `json:"requestType"`
        RequestID    string `json:"requestId"`
        FinCode      string `json:"finCode"`
        SerialNumber string `json:"serialNumber"`
}

// InquireByIdCardResponse mirrors the real AZMK response structure.
type InquireByIdCardResponse struct {
        Result    json.Number  `json:"result"`
        RequestID string       `json:"requestId"`
        Message   string       `json:"message"`
        Data      *InquireData `json:"data"`
}

// InquireData contains the "return" object (PR #166: real response uses "return" not "Request").
type InquireData struct {
        Return *InquiryResult `json:"return"`
}

// InquiryResult contains borrower, liabilities, score, etc.
// PR #166: real response-dan — data.return obyektidir.
type InquiryResult struct {
        ReportID      string       `json:"reportId"`
        ReportingDate string       `json:"reportingDate"`
        Borrower      *Borrower    `json:"borrower"`
        Liabilities   *Liabilities `json:"liabilities"`
        Score         *AkbScore    `json:"score"`
        Balance       float64      `json:"balance"`
}

// Borrower contains the customer's personal info.
type Borrower struct {
        DocumentNo string `json:"documentNo"`
        Name       string `json:"name"`
        Fin        string `json:"fin"`
        DateOfBirth string `json:"dateOfBirth"`
}

// Liabilities contains the liability array.
type Liabilities struct {
        Liability []Liability `json:"liability"`
}

// Liability represents a single credit/loan.
type Liability struct {
        ID                   string  `json:"id"`
        BankName             string  `json:"bankName"`
        CreditType           string  `json:"creditType"`
        CreditTypeName       string  `json:"creditTypeName"`
        GrantedOn            string  `json:"grantedOn"`
        InitialAmount        float64 `json:"initialAmount"`
        DaysInterestOverdue  int     `json:"daysInterestOverdue"`
        DaysMainSumOverdue   int     `json:"daysMainSumOverdue"`
        OutstandingDebtMain  float64 `json:"outstandingDebtMain"`
        MonthlyPaymentAmount float64 `json:"monthlyPaymentAmount"`
        CreditStatus         string  `json:"creditStatus"`
        CreditStatusName     string  `json:"creditStatusName"`
        History              *History `json:"history"`
}

// History contains the historyItem array.
type History struct {
        HistoryItem []HistoryItem `json:"historyItem"`
}

// HistoryItem represents a single month's delay record.
type HistoryItem struct {
        OverdueDays     int    `json:"overdueDays"`
        ReportingPeriod string `json:"reportingPeriod"`
        CreditStatus    string `json:"creditStatus"`
}

// AkbScore contains the score from the inquiry.
type AkbScore struct {
        Calculated bool   `json:"calculated"`
        Point      int    `json:"point"`
        Response   string `json:"response"`
}

// CreditHistory wraps InquiryResult for convenience and provides
// helper methods for the cutoff checks (PR #165).
type CreditHistory struct {
        Inquiry *InquiryResult
}

// IsActive returns true if the liability is an active credit.
// PR #174: yalnız creditStatus = "007" olanlar aktiv sayılır.
func (l *Liability) IsActive() bool {
        return l.CreditStatus == "007"
}

// CurrentDelayDays returns the max of DaysInterestOverdue and DaysMainSumOverdue.
func (l *Liability) CurrentDelayDays() int {
        if l.DaysInterestOverdue > l.DaysMainSumOverdue {
                return l.DaysInterestOverdue
        }
        return l.DaysMainSumOverdue
}

// TotalDelayDays sums all overdueDays from history items.
func (l *Liability) TotalDelayDays() int {
        if l.History == nil {
                return 0
        }
        total := 0
        for _, h := range l.History.HistoryItem {
                total += h.OverdueDays
        }
        return total
}

// PaymentMonths returns the number of history items (months with records).
func (l *Liability) PaymentMonths() int {
        if l.History == nil {
                return 0
        }
        return len(l.History.HistoryItem)
}

// DelayRatio calculates: totalDelayDays / paymentMonths.
// Returns 0 if paymentMonths is 0.
func (l *Liability) DelayRatio() float64 {
        pm := l.PaymentMonths()
        if pm == 0 {
                return 0
        }
        return float64(l.TotalDelayDays()) / float64(pm)
}

// MaxDelayInPeriod returns the highest single overdueDays from history items
// within the last N months (based on reportingPeriod).
// PR #166: reportingPeriod format is "08x2026" (month x year).
func (l *Liability) MaxDelayInPeriod(months int) int {
        if l.History == nil {
                return 0
        }
        cutoff := time.Now().AddDate(0, -months, 0)
        max := 0
        for _, h := range l.History.HistoryItem {
                t := parseReportingPeriod(h.ReportingPeriod)
                if t.IsZero() {
                        // Can't parse — include it (fail-safe)
                        if h.OverdueDays > max {
                                max = h.OverdueDays
                        }
                        continue
                }
                if t.After(cutoff) || t.Equal(cutoff) {
                        if h.OverdueDays > max {
                                max = h.OverdueDays
                        }
                }
        }
        return max
}

// parseReportingPeriod parses "08x2026" format → time.Time (2026-08-01).
// PR #166: real AZMK response uses "MMxYYYY" format.
func parseReportingPeriod(s string) time.Time {
        s = strings.TrimSpace(s)
        // Try "08x2026" format
        parts := strings.Split(s, "x")
        if len(parts) == 2 {
                month, err1 := strconv.Atoi(parts[0])
                year, err2 := strconv.Atoi(parts[1])
                if err1 == nil && err2 == nil && month >= 1 && month <= 12 {
                        return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
                }
        }
        // Try "2006-01" format (fallback)
        if t, err := time.Parse("2006-01", s); err == nil {
                return t
        }
        // Try "2006-01-02" format
        if t, err := time.Parse("2006-01-02", s); err == nil {
                return t
        }
        return time.Time{}
}

// --- CreditHistory helper methods for cutoff checks ---

// MaxDelayRatio returns the highest delay ratio across all liabilities.
// Kesim #1: əmsal 6-dən yüksək → imtina
func (ch *CreditHistory) MaxDelayRatio() float64 {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return 0
        }
        max := 0.0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                r := l.DelayRatio()
                if r > max {
                        max = r
                }
        }
        return max
}

// MaxCurrentDelay returns the highest current delay days across active liabilities.
// Kesim #2: cari gecikmə 5-dən artıq → imtina
func (ch *CreditHistory) MaxCurrentDelay() int {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return 0
        }
        max := 0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                if l.IsActive() && l.CurrentDelayDays() > max {
                        max = l.CurrentDelayDays()
                }
        }
        return max
}

// MaxDelay3M returns the highest single-delay in last 3 months.
// Kesim #3: son 3 ay 20+ → imtina
func (ch *CreditHistory) MaxDelay3M() int {
        return ch.maxDelayInPeriod(3)
}

// MaxDelay6M returns the highest single-delay in last 6 months.
// Kesim #4: son 6 ay 30+ → imtina
func (ch *CreditHistory) MaxDelay6M() int {
        return ch.maxDelayInPeriod(6)
}

// MaxDelay12M returns the highest single-delay in last 12 months.
// Kesim #5: son 12 ay 45+ → imtina
func (ch *CreditHistory) MaxDelay12M() int {
        return ch.maxDelayInPeriod(12)
}

// MaxDelay18M returns the highest single-delay in last 18 months.
// Kesim #6: son 18 ay 60+ → imtina
func (ch *CreditHistory) MaxDelay18M() int {
        return ch.maxDelayInPeriod(18)
}

// maxDelayInPeriod returns the highest single overdueDays across all liabilities
// within the last N months.
func (ch *CreditHistory) maxDelayInPeriod(months int) int {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return 0
        }
        max := 0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                d := l.MaxDelayInPeriod(months)
                if d > max {
                        max = d
                }
        }
        return max
}

// TotalActiveMonthlyPayments returns the sum of monthly payments for active liabilities.
// Kesim #7: aktiv aylıq ödəniş 2000-dən artıq → imtina
func (ch *CreditHistory) TotalActiveMonthlyPayments() float64 {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return 0
        }
        total := 0.0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                if l.IsActive() {
                        total += l.MonthlyPaymentAmount
                }
        }
        return total
}

// --- PR #175: calculation_details JSON structures ---
//
// calculation_details sütununda məlumat JSON formatında saxlanılır.
// Hər kompleks kesim növü üçün ayrı struct var — bu struct-lar
// həm proqrammatically parse oluna bilər, həm də insanda oxunaqlıdır.

// DelayRatioDetailJSON describes how MaxDelayRatio was calculated.
// Kesim: DELAY_RATIO_HIGH — bütün kreditlər üzrə maksimal gecikmə əmsalı.
type DelayRatioDetailJSON struct {
        Calculation string                  `json:"calculation"`     // "max_delay_ratio"
        MaxRatio    float64                 `json:"max_ratio"`       // yekun dəyər (thresholdla müqayisə olunan)
        Formula     string                  `json:"formula"`         // hesablama düsturu (insan üçün)
        Liabilities  []DelayRatioLiability  `json:"liabilities"`     // hər kreditin dəyərləri
}

// DelayRatioLiability is a single liability entry in DelayRatioDetailJSON.
type DelayRatioLiability struct {
        ID             string  `json:"id"`
        BankName       string  `json:"bank_name"`
        CreditStatus   string  `json:"credit_status"`
        TotalDelayDays int     `json:"total_delay_days"`
        PaymentMonths  int     `json:"payment_months"`
        DelayRatio     float64 `json:"delay_ratio"`
}

// CurrentDelayDetailJSON describes how MaxCurrentDelay was calculated.
// Kesim: ACTIVE_DELAY_HIGH — yalnız creditStatus=007 olan aktiv kreditlərin cari gecikməsi.
type CurrentDelayDetailJSON struct {
        Calculation         string                   `json:"calculation"`           // "max_current_delay"
        MaxDelayDays        int                      `json:"max_delay_days"`        // yekun dəyər
        CreditStatusFilter  string                   `json:"credit_status_filter"`  // "007"
        Formula             string                   `json:"formula"`               // düstur
        Liabilities         []CurrentDelayLiability  `json:"liabilities"`           // aktiv kreditlər
}

// CurrentDelayLiability is a single liability entry in CurrentDelayDetailJSON.
type CurrentDelayLiability struct {
        ID                  string `json:"id"`
        BankName            string `json:"bank_name"`
        CreditStatus        string `json:"credit_status"`
        DaysInterestOverdue int    `json:"days_interest_overdue"`
        DaysMainSumOverdue  int    `json:"days_main_sum_overdue"`
        CurrentDelayDays    int    `json:"current_delay_days"`
}

// MonthlyPaymentsDetailJSON describes how TotalActiveMonthlyPayments was calculated.
// Kesim: MONTHLY_PAYMENTS_HIGH — yalnız creditStatus=007 olan kreditlərin aylıq ödəniş cəmi.
type MonthlyPaymentsDetailJSON struct {
        Calculation        string                       `json:"calculation"`          // "total_active_monthly_payments"
        Total              float64                      `json:"total"`                // yekun cəm
        ActiveCount        int                          `json:"active_count"`         // aktiv kredit sayı
        CreditStatusFilter string                       `json:"credit_status_filter"` // "007"
        Formula            string                       `json:"formula"`              // düstur
        Liabilities        []MonthlyPaymentLiability    `json:"liabilities"`          // aktiv kreditlər
}

// MonthlyPaymentLiability is a single liability entry in MonthlyPaymentsDetailJSON.
type MonthlyPaymentLiability struct {
        ID                   string  `json:"id"`
        BankName             string  `json:"bank_name"`
        CreditStatus         string  `json:"credit_status"`
        MonthlyPaymentAmount float64 `json:"monthly_payment_amount"`
}

// MaxDelayRatioDetail returns a JSON string showing how the max delay ratio was calculated.
// PR #174: hər kredit üçün delayDays, paymentMonths və ratio göstərir.
// PR #175: JSON strukturuna keçid — artıq pipe-separated string yox.
//
// Example output:
//
//      {
//        "calculation": "max_delay_ratio",
//        "max_ratio": 7.00,
//        "formula": "max(totalDelayDays / paymentMonths) across all liabilities",
//        "liabilities": [
//          {"id":"1677","bank_name":"...","credit_status":"007","total_delay_days":70,"payment_months":10,"delay_ratio":7.00},
//          {"id":"1544","bank_name":"...","credit_status":"001","total_delay_days":1,"payment_months":14,"delay_ratio":0.07}
//        ]
//      }
func (ch *CreditHistory) MaxDelayRatioDetail() string {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return ""
        }
        detail := DelayRatioDetailJSON{
                Calculation: "max_delay_ratio",
                Formula:     "max(totalDelayDays / paymentMonths) across all liabilities with history",
        }
        maxRatio := 0.0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                if l.History == nil || len(l.History.HistoryItem) == 0 {
                        continue
                }
                ratio := l.DelayRatio()
                if ratio > maxRatio {
                        maxRatio = ratio
                }
                detail.Liabilities = append(detail.Liabilities, DelayRatioLiability{
                        ID:             l.ID,
                        BankName:       l.BankName,
                        CreditStatus:   l.CreditStatus,
                        TotalDelayDays: l.TotalDelayDays(),
                        PaymentMonths:  l.PaymentMonths(),
                        DelayRatio:     ratio,
                })
        }
        if len(detail.Liabilities) == 0 {
                return ""
        }
        detail.MaxRatio = maxRatio
        b, err := json.Marshal(detail)
        if err != nil {
                slog.Warn("MaxDelayRatioDetail: json marshal failed", "error", err)
                return ""
        }
        return string(b)
}

// TotalActiveMonthlyPaymentsDetail returns a JSON string showing which payments were summed.
// PR #174: yalnız creditStatus=007 olan kreditlərin aylıq ödənişləri.
// PR #175: JSON strukturuna keçid.
//
// Example output:
//
//      {
//        "calculation": "total_active_monthly_payments",
//        "total": 307.94,
//        "active_count": 2,
//        "credit_status_filter": "007",
//        "formula": "sum(monthlyPaymentAmount) where creditStatus=007",
//        "liabilities": [
//          {"id":"1677","bank_name":"...","credit_status":"007","monthly_payment_amount":164.61},
//          {"id":"7662","bank_name":"...","credit_status":"007","monthly_payment_amount":143.33}
//        ]
//      }
func (ch *CreditHistory) TotalActiveMonthlyPaymentsDetail() string {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return ""
        }
        detail := MonthlyPaymentsDetailJSON{
                Calculation:        "total_active_monthly_payments",
                CreditStatusFilter: "007",
                Formula:            "sum(monthlyPaymentAmount) where creditStatus=007",
        }
        total := 0.0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                if l.IsActive() {
                        detail.Liabilities = append(detail.Liabilities, MonthlyPaymentLiability{
                                ID:                   l.ID,
                                BankName:             l.BankName,
                                CreditStatus:         l.CreditStatus,
                                MonthlyPaymentAmount: l.MonthlyPaymentAmount,
                        })
                        total += l.MonthlyPaymentAmount
                }
        }
        detail.Total = total
        detail.ActiveCount = len(detail.Liabilities)
        b, err := json.Marshal(detail)
        if err != nil {
                slog.Warn("TotalActiveMonthlyPaymentsDetail: json marshal failed", "error", err)
                return ""
        }
        return string(b)
}

// MaxCurrentDelayDetail returns a JSON string showing active credits and their current delay.
// PR #174: yalnız creditStatus=007 olan kreditlərin cari gecikməsi.
// PR #175: JSON strukturuna keçid.
//
// Example output:
//
//      {
//        "calculation": "max_current_delay",
//        "max_delay_days": 10,
//        "credit_status_filter": "007",
//        "formula": "max(currentDelayDays) where creditStatus=007",
//        "liabilities": [
//          {"id":"1677","bank_name":"...","credit_status":"007","days_interest_overdue":10,"days_main_sum_overdue":5,"current_delay_days":10}
//        ]
//      }
func (ch *CreditHistory) MaxCurrentDelayDetail() string {
        if ch.Inquiry == nil || ch.Inquiry.Liabilities == nil {
                return ""
        }
        detail := CurrentDelayDetailJSON{
                Calculation:        "max_current_delay",
                CreditStatusFilter: "007",
                Formula:            "max(currentDelayDays) where creditStatus=007; currentDelayDays = max(daysInterestOverdue, daysMainSumOverdue)",
        }
        maxDelay := 0
        for _, l := range ch.Inquiry.Liabilities.Liability {
                if l.IsActive() {
                        delay := l.CurrentDelayDays()
                        if delay > maxDelay {
                                maxDelay = delay
                        }
                        detail.Liabilities = append(detail.Liabilities, CurrentDelayLiability{
                                ID:                  l.ID,
                                BankName:            l.BankName,
                                CreditStatus:        l.CreditStatus,
                                DaysInterestOverdue: l.DaysInterestOverdue,
                                DaysMainSumOverdue:  l.DaysMainSumOverdue,
                                CurrentDelayDays:    delay,
                        })
                }
        }
        detail.MaxDelayDays = maxDelay
        b, err := json.Marshal(detail)
        if err != nil {
                slog.Warn("MaxCurrentDelayDetail: json marshal failed", "error", err)
                return ""
        }
        return string(b)
}

// --- PR #239: GetEmployeeInfoByPin ---

// EmployeeInfoRequest is the request body for GetEmployeeInfoByPin.
// PR #239: AZMK CustomerDataService-ə sorğu göndərilir.
type EmployeeInfoRequest struct {
        RequestType   string `json:"requestType"`
        RequestID     string `json:"requestId"`
        FinCode       string `json:"finCode"`
        SerialNumber  string `json:"serialNumber"`
}

// GetEmployeeInfoByPin retrieves employment records from AZMK CustomerDataService.
// PR #239: requestType = "GetEmployeeInfoByPin", AZMK URL + Basic Auth.
func (p *HTTPCustomerDataProvider) GetEmployeeInfoByPin(ctx context.Context, finCode, serialNumber string) (*mygov.EmployeeInfoResponse, error) {
        reqBody := EmployeeInfoRequest{
                RequestType:  "GetEmployeeInfoByPin",
                RequestID:    "1",
                FinCode:      finCode,
                SerialNumber: serialNumber,
        }
        jsonBody, _ := json.Marshal(reqBody)
        url := p.baseURL

        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
        if err != nil {
                p.auditLog("AZMK_GET_EMPLOYEE_INFO", "POST", url, string(jsonBody), "", 0, 0, err.Error())
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog("AZMK_GET_EMPLOYEE_INFO", "POST", url, string(jsonBody), "", 0, durationMs, err.Error())
                return nil, fmt.Errorf("AZMK GetEmployeeInfoByPin request failed: %w", err)
        }
        defer resp.Body.Close()

        respBodyBytes, _ := io.ReadAll(resp.Body)
        respBodyStr := string(respBodyBytes)

        var empResp mygov.EmployeeInfoResponse
        if err := json.Unmarshal(respBodyBytes, &empResp); err != nil {
                p.auditLog("AZMK_GET_EMPLOYEE_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, fmt.Sprintf("decode error: %v", err))
                return nil, fmt.Errorf("failed to decode employee info response: %w", err)
        }
        if empResp.Result != 1 {
                errMsg := fmt.Sprintf("AZMK GetEmployeeInfoByPin error: %s (result=%d)", empResp.Message, empResp.Result)
                p.auditLog("AZMK_GET_EMPLOYEE_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }
        p.auditLog("AZMK_GET_EMPLOYEE_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "")
        slog.Info("AZMK GetEmployeeInfoByPin success",
                "fin", finCode,
                "active_count", len(empResp.Data.Response.Active),
                "deactive_count", len(empResp.Data.Response.Deactive),
                "duration_ms", durationMs)
        return &empResp, nil
}

// MkrScoreRequest is the request body for getMkrScore.
// PR #160: AKB skoru və stop-faktor yoxlaması üçün.
type MkrScoreRequest struct {
        RequestType  string `json:"requestType"`
        RequestID    string `json:"requestId"`
        FinCode      string `json:"finCode"`
        SerialNumber string `json:"serialNumber"`
}

// MkrScoreResponse is the response from getMkrScore.
type MkrScoreResponse struct {
        Result    int       `json:"result"`
        RequestID string    `json:"requestId"`
        Message   string    `json:"message"`
        Data      *MkrScoreData `json:"data"`
}

// MkrScoreData contains the score object from getMkrScore response.
type MkrScoreData struct {
        Score MkrScoreDetail `json:"score"`
}

// MkrScoreDetail contains the actual score details.
type MkrScoreDetail struct {
        Response   string  `json:"response"`   // "A", "B", "C" — stop-faktor yoxlaması
        Point      int     `json:"point"`      // 839 — skor müqayisəsi üçün (< 200?)
        PdRate     float64 `json:"pdRate"`     // 0.01 — probability of default
        Calculated bool    `json:"calculated"` // true — hesablanıb mı?
}

// MkrScore wraps MkrScoreDetail for convenience.
type MkrScore struct {
        Score MkrScoreDetail
}

// OwnerDataRequest is the request body for getOwnerData.
// PR #159: qara siyahı və kredit məlumatları üçün.
type OwnerDataRequest struct {
        RequestType  string `json:"requestType"`
        RequestID    string `json:"requestId"`
        FinCode      string `json:"finCode"`
        SerialNumber string `json:"serialNumber"`
}

// OwnerDataResponse is the response from getOwnerData.
type OwnerDataResponse struct {
        Result    int       `json:"result"`
        RequestID string    `json:"requestId"`
        Message   string    `json:"message"`
        Data      *OwnerCheckData `json:"data"`
}

// OwnerCheckData contains the customerCheck object from getOwnerData response.
type OwnerCheckData struct {
        CustomerCheck CustomerCheck `json:"customerCheck"`
}

// CustomerCheck contains blacklist, active credits, and delay information.
type CustomerCheck struct {
        PinCode                    string        `json:"Pin_code"`
        ActiveCredits              []interface{} `json:"activeCredits"`
        IsExistingCustomer         bool          `json:"isExistingCustomer"`
        BlacklistStatus            bool          `json:"blacklistStatus"`
        TotalDelayDaysCumulative   int           `json:"totalDelayDaysCumulative"`
        HasActiveCredit            bool          `json:"hasActiveCredit"`
}

// OwnerData wraps CustomerCheck for convenience.
type OwnerData struct {
        CustomerCheck CustomerCheck
}

// CustomerDataRequest is the request body for GetPersonalInfo.
type CustomerDataRequest struct {
        RequestType   string `json:"requestType"`
        RequestID     string `json:"requestId"`
        FinCode       string `json:"finCode"`
        SerialNumber  string `json:"serialNumber"`
}

// CustomerDataResponse is the response from GetPersonalInfo.
type CustomerDataResponse struct {
        Result    int    `json:"result"`
        RequestID string `json:"requestId"`
        Message   string `json:"message"`
        Data      *CustomerData `json:"data"`
}

// CustomerData contains the personal information returned by AZMK.
type CustomerData struct {
        Flat               string `json:"Flat"`
        BirthDate          string `json:"BirthDate"`           // "1993-08-09" — yaş yoxlaması üçün
        House              string `json:"House"`
        Surname            string `json:"Surname"`
        RegistrationAddress string `json:"RegistrationAddress"`
        Street             string `json:"Street"`
        Gender             string `json:"Gender"`
        GivenDate          string `json:"GivenDate"`
        BirthAddress       string `json:"BirthAddress"`
        GivenOrganization  string `json:"GivenOrganization"`
        ExpireDate         string `json:"ExpireDate"`
        MaritalStatus      string `json:"MaritalStatus"`
        City               string `json:"City"`
        Patronymic         string `json:"Patronymic"`
        Name               string `json:"Name"`
        DocumentSeriaNumber string `json:"DocumentSeriaNumber"`
}

// FullName returns "Name Surname Patronymic".
func (cd *CustomerData) FullName() string {
        if cd == nil {
                return ""
        }
        parts := []string{cd.Name, cd.Surname, cd.Patronymic}
        return strings.Join(filterEmpty(parts), " ")
}

// Age calculates age from BirthDate (format "2006-01-02").
// Returns 0 if BirthDate is empty or invalid (fail-soft).
func (cd *CustomerData) Age() int {
        if cd == nil || cd.BirthDate == "" {
                return 0
        }
        dob, err := time.Parse("2006-01-02", cd.BirthDate)
        if err != nil {
                return 0
        }
        now := time.Now()
        age := now.Year() - dob.Year()
        if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
                age--
        }
        if age < 0 {
                return 0
        }
        return age
}

func filterEmpty(ss []string) []string {
        var result []string
        for _, s := range ss {
                if s != "" {
                        result = append(result, s)
                }
        }
        return result
}

// --- HTTP Provider ---

// HTTPCustomerDataProvider calls the real AZMK CustomerDataService.
type HTTPCustomerDataProvider struct {
        baseURL    string
        username   string
        password   string
        timeout    time.Duration
        httpClient *http.Client
        // PR #163: audit log
        auditDB *sql.DB
        appID   *int
}

// SetAuditDB sets the DB connection and application ID for audit logging.
// PR #163: hər AZMK CustomerDataService çağırış üçün audit log yazmaq.
func (p *HTTPCustomerDataProvider) SetAuditDB(db *sql.DB, appID *int) {
        p.auditDB = db
        p.appID = appID
}

// SetAuditAppID sets the current application ID for audit logging.
// PR #168: hər müraciət üçün dinamik olaraq appID set etmək.
func (p *HTTPCustomerDataProvider) SetAuditAppID(appID *int) {
        p.appID = appID
}

// auditLog writes a service call audit log to the database.
func (p *HTTPCustomerDataProvider) auditLog(serviceName, method, url, reqBody, respBody string, statusCode int, durationMs int, errMsg string) {
        if p.auditDB == nil {
                return
        }
        _, err := p.auditDB.Exec(`
                INSERT INTO service_audit_logs
                        (application_id, service_name, method, url, request_body, response_body, status_code, duration_ms, error)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
                p.appID, serviceName, method, url, reqBody, respBody, statusCode, durationMs, errMsg)
        if err != nil {
                slog.Warn("failed to write audit log", "error", err, "service", serviceName)
        }
}

// NewHTTPCustomerDataProvider creates a new HTTPCustomerDataProvider.
func NewHTTPCustomerDataProvider(baseURL, username, password string, timeoutS int) *HTTPCustomerDataProvider {
        timeout := time.Duration(timeoutS) * time.Second
        return &HTTPCustomerDataProvider{
                baseURL:  strings.TrimRight(baseURL, "/"),
                username: username,
                password: password,
                timeout:  timeout,
                httpClient: &http.Client{
                        Timeout: timeout,
                },
        }
}

// GetPersonalInfo calls the real AZMK CustomerDataService.
func (p *HTTPCustomerDataProvider) GetPersonalInfo(ctx context.Context, finCode, serialNumber string) (*CustomerData, error) {
        reqBody := CustomerDataRequest{
                RequestType:  "GetPersonalInfo",
                RequestID:    "1",
                FinCode:      finCode,
                SerialNumber: serialNumber,
        }
        jsonBody, _ := json.Marshal(reqBody)
        url := p.baseURL
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
        if err != nil {
                p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), "", 0, 0, err.Error())
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), "", 0, durationMs, err.Error())
                return nil, fmt.Errorf("AZMK CustomerDataService request failed: %w", err)
        }
        defer resp.Body.Close()

        respBodyBytes, _ := io.ReadAll(resp.Body)
        respBodyStr := string(respBodyBytes)

        // Re-decode from the bytes we read
        var cdResp CustomerDataResponse
        if err := json.Unmarshal(respBodyBytes, &cdResp); err != nil {
                p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, err.Error())
                return nil, fmt.Errorf("failed to decode response: %w", err)
        }

        if cdResp.Result != 1 {
                errMsg := fmt.Sprintf("AZMK CustomerDataService error: %s (result=%d)", cdResp.Message, cdResp.Result)
                p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }

        if cdResp.Data == nil {
                p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "no data")
                return nil, fmt.Errorf("AZMK CustomerDataService returned no data")
        }

        p.auditLog("AZMK_GET_PERSONAL_INFO", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "")
        slog.Info("AZMK CustomerDataService success",
                "fin", finCode,
                "name", cdResp.Data.FullName(),
                "birth_date", cdResp.Data.BirthDate,
                "age", cdResp.Data.Age(),
        )
        return cdResp.Data, nil
}

// GetOwnerData calls the real AZMK CustomerDataService with getOwnerData request type.
// PR #159: qara siyahı (blacklistStatus), aktiv kredit, gecikmə məlumatları üçün.
func (p *HTTPCustomerDataProvider) GetOwnerData(ctx context.Context, finCode, serialNumber string) (*OwnerData, error) {
        reqBody := OwnerDataRequest{
                RequestType:  "getOwnerData",
                RequestID:    "1",
                FinCode:      finCode,
                SerialNumber: serialNumber,
        }
        jsonBody, _ := json.Marshal(reqBody)
        url := p.baseURL
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
        if err != nil {
                p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), "", 0, 0, err.Error())
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), "", 0, durationMs, err.Error())
                return nil, fmt.Errorf("AZMK CustomerDataService getOwnerData request failed: %w", err)
        }
        defer resp.Body.Close()

        respBodyBytes, _ := io.ReadAll(resp.Body)
        respBodyStr := string(respBodyBytes)

        var odResp OwnerDataResponse
        if err := json.Unmarshal(respBodyBytes, &odResp); err != nil {
                p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, err.Error())
                return nil, fmt.Errorf("failed to decode getOwnerData response: %w", err)
        }

        if odResp.Result != 1 {
                errMsg := fmt.Sprintf("AZMK getOwnerData error: %s (result=%d)", odResp.Message, odResp.Result)
                p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }

        if odResp.Data == nil {
                p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "no data")
                return nil, fmt.Errorf("AZMK getOwnerData returned no data")
        }

        p.auditLog("AZMK_GET_OWNER_DATA", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "")
        slog.Info("AZMK getOwnerData success",
                "fin", finCode,
                "blacklist_status", odResp.Data.CustomerCheck.BlacklistStatus,
                "is_existing_customer", odResp.Data.CustomerCheck.IsExistingCustomer,
                "has_active_credit", odResp.Data.CustomerCheck.HasActiveCredit,
                "total_delay_days", odResp.Data.CustomerCheck.TotalDelayDaysCumulative,
        )
        return &OwnerData{CustomerCheck: odResp.Data.CustomerCheck}, nil
}

// GetMkrScore calls the real AZMK CustomerDataService with getMkrScore request type.
// PR #160: AKB skoru (point) və stop-faktor (response) yoxlaması üçün.
func (p *HTTPCustomerDataProvider) GetMkrScore(ctx context.Context, finCode, serialNumber string) (*MkrScore, error) {
        reqBody := MkrScoreRequest{
                RequestType:  "getMkrScore",
                RequestID:    "1",
                FinCode:      finCode,
                SerialNumber: serialNumber,
        }
        jsonBody, _ := json.Marshal(reqBody)
        url := p.baseURL
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
        if err != nil {
                p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), "", 0, 0, err.Error())
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), "", 0, durationMs, err.Error())
                return nil, fmt.Errorf("AZMK getMkrScore request failed: %w", err)
        }
        defer resp.Body.Close()

        respBodyBytes, _ := io.ReadAll(resp.Body)
        respBodyStr := string(respBodyBytes)

        var msResp MkrScoreResponse
        if err := json.Unmarshal(respBodyBytes, &msResp); err != nil {
                p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, err.Error())
                return nil, fmt.Errorf("failed to decode getMkrScore response: %w", err)
        }

        if msResp.Result != 1 {
                errMsg := fmt.Sprintf("AZMK getMkrScore error: %s (result=%d)", msResp.Message, msResp.Result)
                p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }

        if msResp.Data == nil {
                p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "no data")
                return nil, fmt.Errorf("AZMK getMkrScore returned no data")
        }

        p.auditLog("AZMK_GET_MKR_SCORE", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "")
        slog.Info("AZMK getMkrScore success",
                "fin", finCode,
                "point", msResp.Data.Score.Point,
                "response", msResp.Data.Score.Response,
                "pd_rate", msResp.Data.Score.PdRate,
                "calculated", msResp.Data.Score.Calculated,
        )
        return &MkrScore{Score: msResp.Data.Score}, nil
}

// InquireByIdCard calls the real AZMK CustomerDataService with inquireByIdCard request type.
// PR #165: kredit tarixçəsi və gecikmə kesim nöqtələri üçün.
// URL: https://web.azmk.az:7077/LW_AKP/services/CustomerDataService
func (p *HTTPCustomerDataProvider) InquireByIdCard(ctx context.Context, finCode, serialNumber string) (*CreditHistory, error) {
        reqBody := InquireByIdCardRequest{
                RequestType:  "inquireByIdCard",
                RequestID:    "1",
                FinCode:      finCode,
                SerialNumber: serialNumber,
        }
        jsonBody, _ := json.Marshal(reqBody)
        url := p.baseURL
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(jsonBody)))
        if err != nil {
                p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), "", 0, 0, err.Error())
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        start := time.Now()
        resp, err := p.httpClient.Do(req)
        durationMs := int(time.Since(start).Milliseconds())
        if err != nil {
                p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), "", 0, durationMs, err.Error())
                return nil, fmt.Errorf("AZMK inquireByIdCard request failed: %w", err)
        }
        defer resp.Body.Close()

        respBodyBytes, _ := io.ReadAll(resp.Body)
        respBodyStr := string(respBodyBytes)

        var inqResp InquireByIdCardResponse
        if err := json.Unmarshal(respBodyBytes, &inqResp); err != nil {
                p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, err.Error())
                return nil, fmt.Errorf("failed to decode inquireByIdCard response: %w", err)
        }

        // Check result — may be string "1" or int 1
        resultStr := inqResp.Result.String()
        if resultStr != "1" {
                errMsg := fmt.Sprintf("AZMK inquireByIdCard error: %s (result=%s)", inqResp.Message, resultStr)
                p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, errMsg)
                return nil, fmt.Errorf("%s", errMsg)
        }

        if inqResp.Data == nil || inqResp.Data.Return == nil {
                p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "no data")
                return nil, fmt.Errorf("AZMK inquireByIdCard returned no inquiry data")
        }

        p.auditLog("AZMK_INQUIRE_BY_ID_CARD", "POST", url, string(jsonBody), respBodyStr, resp.StatusCode, durationMs, "")

        liabCount := 0
        if inqResp.Data.Return.Liabilities != nil {
                liabCount = len(inqResp.Data.Return.Liabilities.Liability)
        }
        slog.Info("AZMK inquireByIdCard success",
                "fin", finCode,
                "liabilities_count", liabCount,
        )

        return &CreditHistory{Inquiry: inqResp.Data.Return}, nil
}

// --- Mock Provider ---

// MockCustomerDataProvider returns canned data based on FIN code.
// PR #152: FIN koduna görə fərqli şəxslər imitasiya olunur.
//
// Ssenari seçimi (prioritet sırası):
//   1. finScenarios map-də FIN varsa — həmin ssenari
//   2. FIN "TEST" ilə başlayırsa — TEST age_map-dən yaş
//   3. Default — 35 yaşlı müştəri
type MockCustomerDataProvider struct {
        finScenarios map[string]string
}

// NewMockCustomerDataProvider creates a new MockCustomerDataProvider.
func NewMockCustomerDataProvider() *MockCustomerDataProvider {
        return &MockCustomerDataProvider{
                finScenarios: defaultFinScenarios,
        }
}

// defaultFinScenarios maps FIN codes to scenario names.
// PR #153: Bütün FIN kodları 7 simvol olmalıdır (validation: ^[A-Za-z0-9]{7}$).
// İstifadəçi bu map-ə öz FIN-lərini əlavə edə bilər.
var defaultFinScenarios = map[string]string{
        // Yaş ssenariləri (hər biri 7 simvol)
        "YAS1800": "young_adult",      // 18 yaş
        "YAS2500": "young_adult_25",   // 25 yaş
        "YAS3500": "adult",            // 35 yaş (default)
        "YAS5000": "middle_aged",      // 50 yaş
        "YAS6500": "senior",           // 65 yaş
        "YAS7000": "old_customer",     // 70 yaş → AGE_OVER_69 cutoff
        "YAS8000": "very_old",         // 80 yaş → AGE_OVER_69 cutoff

        // Real FIN nümunəsi (istifadəçinin verdiyi) — artıq 7 simvol
        "60R99CP": "real_example",   // 1993-08-09 doğum — ~33 yaş

        // PR #159: Qara siyahı ssenariləri
        "BLK0001": "blacklisted",    // qara siyahıdadır
        "ACT0001": "active_credit",  // aktiv kredit var
        "DLY0001": "delay_history",  // gecikmə tarixçəsi var

        // PR #160: MKR skor ssenariləri
        "SCR0150": "low_score",      // point=150 → AKB_SCORE_LOW (< 200)
        "SCR0500": "stop_factor",    // response="AB" → AKB_STOP_FACTOR
        "SCR0839": "good_score",     // point=839, response="B" → keçir

        // PR #165: Kredit tarixçəsi kesim ssenariləri
        "DLR0001": "delay_ratio_high",     // gecikmə əmsalı > 6
        "ACTDLY1": "active_delay_high",    // aktiv kredit cari gecikmə > 5
        "DLY3M01": "delay_3m_high",        // son 3 ay max gecikmə ≥ 20
        "DLY6M01": "delay_6m_high",        // son 6 ay max gecikmə ≥ 30
        "DLY12M01": "delay_12m_high",      // son 12 ay max gecikmə ≥ 45
        "DLY18M01": "delay_18m_high",      // son 18 ay max gecikmə ≥ 60
        "PAY2001": "monthly_payments_high", // aktiv aylıq ödəniş > 2000
        "CLR0001": "clean_history",         // təmiz kredit tarixçəsi

        // Error ssenarisi (7 simvol)
        "ERROR01": "error",
}

// GetPersonalInfo returns mock customer data based on FIN code.
func (m *MockCustomerDataProvider) GetPersonalInfo(_ context.Context, finCode, serialNumber string) (*CustomerData, error) {
        scenario, ok := m.finScenarios[finCode]
        if !ok {
                scenario = "adult" // default
        }

        switch scenario {
        case "young_adult":
                return mockCustomerData(finCode, serialNumber, "Sadiq", "Quliyev", "Eldar oğlu", "2008-05-15", "Bakı", "Kişi"), nil
        case "young_adult_25":
                return mockCustomerData(finCode, serialNumber, "Nicat", "Hüseynli", "Rəşid oğlu", "2001-03-20", "Gəncə", "Kişi"), nil
        case "adult":
                return mockCustomerData(finCode, serialNumber, "Əli", "Əliyev", "Vahid oğlu", "1991-03-10", "Bakı", "Kişi"), nil
        case "middle_aged":
                return mockCustomerData(finCode, serialNumber, "Vüqar", "Məmmədov", "Səlim oğlu", "1976-07-22", "Sumqayıt", "Kişi"), nil
        case "senior":
                return mockCustomerData(finCode, serialNumber, "Rafiq", "Hüseynov", "Tofiq oğlu", "1961-11-03", "Bakı", "Kişi"), nil
        case "old_customer":
                return mockCustomerData(finCode, serialNumber, "Sabir", "Nərimanov", "Qara oğlu", "1955-01-15", "Bakı", "Kişi"), nil
        case "very_old":
                return mockCustomerData(finCode, serialNumber, "Tofiq", "Bəkirov", "Nəsib oğlu", "1945-06-08", "Mingəçevir", "Kişi"), nil
        case "real_example":
                // İstifadəçinin verdiyi real nümunə
                return &CustomerData{
                        Name:                "Sadiq",
                        Surname:             "Ələkpərov",
                        Patronymic:          "Sabir oğlu",
                        BirthDate:           "1993-08-09",
                        BirthAddress:        "Azərbaycan Respublikası,UCAR",
                        Gender:              "Kişi",
                        MaritalStatus:       "Evli",
                        City:                "Bakı",
                        Street:              "Tusi",
                        House:               "Ev 55,55a,57,57a,59",
                        Flat:                "3",
                        RegistrationAddress: "BAKI ŞƏHƏRİ, XƏTAİ RAYONU, TUSİ KÜÇƏSİ, EV 55,55A,57,57A,59, MƏNZİL 3",
                        DocumentSeriaNumber: serialNumber,
                        GivenDate:           "2020-03-10",
                        ExpireDate:          "2030-03-09",
                        GivenOrganization:   "Asan 1",
                }, nil
        case "error":
                return nil, fmt.Errorf("mock: simulated AZMK CustomerDataService error for FIN %s", finCode)
        default:
                return mockCustomerData(finCode, serialNumber, "Mock", "Müştəri", "", "1991-03-10", "Bakı", "Kişi"), nil
        }
}

// mockCustomerData creates a CustomerData with the given fields.
func mockCustomerData(fin, serial, name, surname, patronymic, birthDate, city, gender string) *CustomerData {
        return &CustomerData{
                Name:                name,
                Surname:             surname,
                Patronymic:          patronymic,
                BirthDate:           birthDate,
                BirthAddress:        city + ", Azərbaycan",
                Gender:              gender,
                MaritalStatus:       "Evli",
                City:                city,
                Street:              "Nizami",
                House:               "12",
                Flat:                "45",
                RegistrationAddress: city + " şəhəri, Nizami küçəsi, ev 12, mənzil 45",
                DocumentSeriaNumber: serial,
                GivenDate:           "2020-01-15",
                ExpireDate:          "2030-01-14",
                GivenOrganization:   "Asan 1",
        }
}

// GetOwnerData returns mock owner data (blacklist, active credits, delays) based on FIN code.
// PR #159: FIN koduna görə fərqli ssenarilər imitasiya olunur.
func (m *MockCustomerDataProvider) GetOwnerData(_ context.Context, finCode, serialNumber string) (*OwnerData, error) {
        scenario, ok := m.finScenarios[finCode]
        if !ok {
                scenario = "adult" // default
        }

        switch scenario {
        case "blacklisted":
                return &OwnerData{
                        CustomerCheck: CustomerCheck{
                                PinCode:                  finCode,
                                IsExistingCustomer:       true,
                                BlacklistStatus:          true, // qara siyahıdadır
                                HasActiveCredit:          false,
                                TotalDelayDaysCumulative: 0,
                        },
                }, nil
        case "active_credit":
                return &OwnerData{
                        CustomerCheck: CustomerCheck{
                                PinCode:                  finCode,
                                IsExistingCustomer:       true,
                                BlacklistStatus:          false,
                                HasActiveCredit:          true, // aktiv kredit var
                                TotalDelayDaysCumulative: 0,
                        },
                }, nil
        case "delay_history":
                return &OwnerData{
                        CustomerCheck: CustomerCheck{
                                PinCode:                  finCode,
                                IsExistingCustomer:       true,
                                BlacklistStatus:          false,
                                HasActiveCredit:          false,
                                TotalDelayDaysCumulative: 45, // gecikmə tarixçəsi var
                        },
                }, nil
        case "error":
                return nil, fmt.Errorf("mock: simulated AZMK getOwnerData error for FIN %s", finCode)
        default:
                // Default: təmiz müştəri
                return &OwnerData{
                        CustomerCheck: CustomerCheck{
                                PinCode:                  finCode,
                                IsExistingCustomer:       false,
                                BlacklistStatus:          false,
                                HasActiveCredit:          false,
                                TotalDelayDaysCumulative: 0,
                        },
                }, nil
        }
}

// GetMkrScore returns mock MKR score data based on FIN code.
// PR #160: AKB skoru (point) və stop-faktor (response) yoxlaması üçün.
func (m *MockCustomerDataProvider) GetMkrScore(_ context.Context, finCode, serialNumber string) (*MkrScore, error) {
        scenario, ok := m.finScenarios[finCode]
        if !ok {
                scenario = "adult" // default
        }

        switch scenario {
        case "low_score":
                return &MkrScore{
                        Score: MkrScoreDetail{
                                Point:      150,  // < 200 → AKB_SCORE_LOW
                                Response:   "C",
                                PdRate:     0.85,
                                Calculated: true,
                        },
                }, nil
        case "stop_factor":
                return &MkrScore{
                        Score: MkrScoreDetail{
                                Point:      500,
                                Response:   "AB", // stop-faktor → AKB_STOP_FACTOR
                                PdRate:     0.45,
                                Calculated: true,
                        },
                }, nil
        case "good_score":
                return &MkrScore{
                        Score: MkrScoreDetail{
                                Point:      839,  // > 200 → keçir
                                Response:   "B",  // stop-faktor yox
                                PdRate:     0.01,
                                Calculated: true,
                        },
                }, nil
        case "error":
                return nil, fmt.Errorf("mock: simulated AZMK getMkrScore error for FIN %s", finCode)
        default:
                // Default: yaxşı skor
                return &MkrScore{
                        Score: MkrScoreDetail{
                                Point:      750,
                                Response:   "B",
                                PdRate:     0.02,
                                Calculated: true,
                        },
                }, nil
        }
}

// InquireByIdCard returns mock credit history data based on FIN code.
// PR #165: kredit tarixçəsi və gecikmə kesim nöqtələri üçün.
func (m *MockCustomerDataProvider) InquireByIdCard(_ context.Context, finCode, serialNumber string) (*CreditHistory, error) {
        scenario, ok := m.finScenarios[finCode]
        if !ok {
                scenario = "clean_history"
        }

        now := time.Now()
        m1 := now.AddDate(0, -1, 0).Format("01x2006")
        m2 := now.AddDate(0, -2, 0).Format("01x2006")
        m3 := now.AddDate(0, -3, 0).Format("01x2006")
        m4 := now.AddDate(0, -4, 0).Format("01x2006")
        m5 := now.AddDate(0, -5, 0).Format("01x2006")
        m6 := now.AddDate(0, -6, 0).Format("01x2006")
        m7 := now.AddDate(0, -7, 0).Format("01x2006")
        m8 := now.AddDate(0, -8, 0).Format("01x2006")
        m9 := now.AddDate(0, -9, 0).Format("01x2006")
        m10 := now.AddDate(0, -10, 0).Format("01x2006")
        m15 := now.AddDate(0, -15, 0).Format("01x2006")
        m20 := now.AddDate(0, -20, 0).Format("01x2006")

        switch scenario {
        case "delay_ratio_high":
                return mockCH([]Liability{
                        mockL(70, 8, 0, 150, "007", []HistoryItem{{7, m1, ""}, {7, m2, ""}, {7, m3, ""}, {7, m4, ""}, {7, m5, ""}, {7, m6, ""}, {7, m7, ""}, {7, m8, ""}, {7, m9, ""}, {7, m10, ""}}),
                }), nil
        case "active_delay_high":
                return mockCH([]Liability{
                        mockL(8, 8, 2, 200, "007", []HistoryItem{{3, m1, ""}, {1, m2, ""}}),
                }), nil
        case "delay_3m_high":
                return mockCH([]Liability{
                        mockL(25, 25, 0, 200, "001", []HistoryItem{{25, m1, ""}, {5, m4, ""}}),
                }), nil
        case "delay_6m_high":
                return mockCH([]Liability{
                        mockL(35, 35, 0, 200, "001", []HistoryItem{{35, m3, ""}, {5, m8, ""}}),
                }), nil
        case "delay_12m_high":
                return mockCH([]Liability{
                        mockL(50, 50, 0, 200, "001", []HistoryItem{{50, m5, ""}, {5, m15, ""}}),
                }), nil
        case "delay_18m_high":
                return mockCH([]Liability{
                        mockL(65, 65, 0, 200, "001", []HistoryItem{{65, m10, ""}, {5, m20, ""}}),
                }), nil
        case "monthly_payments_high":
                return mockCH([]Liability{
                        mockL(0, 0, 0, 1200, "007", []HistoryItem{{0, m1, ""}}),
                        mockL(0, 0, 0, 900, "007", []HistoryItem{{0, m1, ""}}),
                }), nil
        case "clean_history":
                return mockCH([]Liability{
                        mockL(5, 5, 0, 300, "001", []HistoryItem{{2, m5, ""}, {3, m8, ""}}),
                }), nil
        case "error":
                return nil, fmt.Errorf("mock: simulated AZMK inquireByIdCard error for FIN %s", finCode)
        default:
                return mockCH(nil), nil
        }
}

func mockL(delay, currentDelay, _ int, monthly float64, status string, hist []HistoryItem) Liability {
        return Liability{
                DaysInterestOverdue:  currentDelay,
                DaysMainSumOverdue:   currentDelay,
                MonthlyPaymentAmount: monthly,
                CreditStatus:         status,
                History:              &History{HistoryItem: hist},
        }
}

func mockCH(liabs []Liability) *CreditHistory {
        return &CreditHistory{
                Inquiry: &InquiryResult{
                        Liabilities: &Liabilities{Liability: liabs},
                },
        }
}

// GetEmployeeInfoByPin — PR #239: mock implementation.
func (m *MockCustomerDataProvider) GetEmployeeInfoByPin(_ context.Context, finCode, serialNumber string) (*mygov.EmployeeInfoResponse, error) {
        return &mygov.EmployeeInfoResponse{
                Result:  1,
                Message: "SUCCESS",
                Data: &mygov.EmployeeInfoData{
                        Response: &mygov.EmployeeRecords{
                            Active: []mygov.EmployeeRecord{
                                {
                                    Employer: mygov.EmployerInfo{Name: "Test Employer", Voen: "1234567890"},
                                    Employee: mygov.EmployeeInfo{Position: "Developer", Salary: 3000},
                                    Contract: mygov.ContractInfo{SignDate: "01.01.2020", BeginDate: "01.01.2020", EndDate: "31.12.2026"},
                                },
                            },
                        },
                },
        }, nil
}
