package azmk

import (
        "context"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "log/slog"
        "net/http"
        "strings"
        "time"
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
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                // Basic Auth (same as existing AZMK provider)
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        resp, err := p.httpClient.Do(req)
        if err != nil {
                return nil, fmt.Errorf("AZMK CustomerDataService request failed: %w", err)
        }
        defer resp.Body.Close()

        var cdResp CustomerDataResponse
        if err := json.NewDecoder(resp.Body).Decode(&cdResp); err != nil {
                return nil, fmt.Errorf("failed to decode response: %w", err)
        }

        if cdResp.Result != 1 {
                return nil, fmt.Errorf("AZMK CustomerDataService error: %s (result=%d)", cdResp.Message, cdResp.Result)
        }

        if cdResp.Data == nil {
                return nil, fmt.Errorf("AZMK CustomerDataService returned no data")
        }

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
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        resp, err := p.httpClient.Do(req)
        if err != nil {
                return nil, fmt.Errorf("AZMK CustomerDataService getOwnerData request failed: %w", err)
        }
        defer resp.Body.Close()

        var odResp OwnerDataResponse
        if err := json.NewDecoder(resp.Body).Decode(&odResp); err != nil {
                return nil, fmt.Errorf("failed to decode getOwnerData response: %w", err)
        }

        if odResp.Result != 1 {
                return nil, fmt.Errorf("AZMK getOwnerData error: %s (result=%d)", odResp.Message, odResp.Result)
        }

        if odResp.Data == nil {
                return nil, fmt.Errorf("AZMK getOwnerData returned no data")
        }

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
                return nil, fmt.Errorf("failed to create request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if p.username != "" && p.password != "" {
                auth := p.username + ":" + p.password
                req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
        }

        resp, err := p.httpClient.Do(req)
        if err != nil {
                return nil, fmt.Errorf("AZMK getMkrScore request failed: %w", err)
        }
        defer resp.Body.Close()

        var msResp MkrScoreResponse
        if err := json.NewDecoder(resp.Body).Decode(&msResp); err != nil {
                return nil, fmt.Errorf("failed to decode getMkrScore response: %w", err)
        }

        if msResp.Result != 1 {
                return nil, fmt.Errorf("AZMK getMkrScore error: %s (result=%d)", msResp.Message, msResp.Result)
        }

        if msResp.Data == nil {
                return nil, fmt.Errorf("AZMK getMkrScore returned no data")
        }

        slog.Info("AZMK getMkrScore success",
                "fin", finCode,
                "point", msResp.Data.Score.Point,
                "response", msResp.Data.Score.Response,
                "pd_rate", msResp.Data.Score.PdRate,
                "calculated", msResp.Data.Score.Calculated,
        )
        return &MkrScore{Score: msResp.Data.Score}, nil
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
