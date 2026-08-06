// Package stub implements a lightweight HTTP server that mimics the LW router
// responses. It is intended for DEVELOPMENT ONLY — when the real LW router is
// not yet available, set LW_USE_STUB=true and the HTTPProvider will point to
// this stub server (started in-process on the configured port).
//
// All responses match the format that the real LW router will return:
//   - AKB Score: SOAP-derived JSON {return: {response, point}} (PR #55)
//   - PersonalInfo: JSON {fin, full_name, date_of_birth, ...}
//   - AKB History: JSON {report_id, borrower, liabilities[]}
//   - AZMK Blacklist: JSON {fin, is_blacklisted}
//   - LW Blacklist: JSON {fin, is_blacklisted}
//   - ASAN Finance: JSON {fin, official_income, ...}
//   - LW Loans: JSON {customer_pin, loans[]}
//   - LW ApproveLoan: JSON {application_id, contract_status, ...}
//
// Each endpoint accepts a `?scenario=xxx` query parameter that controls which
// canned response is returned. This lets Postman tests exercise different
// flows (stop factor, low score, error, etc.) without modifying the stub.
//
// When LW is ready, set LW_USE_STUB=false and LW_USE_MOCK=false and point
// LW_BASE_URL to the real router — no code changes needed because the
// HTTPProvider already uses the same response formats.
package stub

import (
        "encoding/json"
        "fmt"
        "log/slog"
        "net/http"
        "strconv"
        "time"
)

// Server is the LW router stub. Construct with New(), then call Start() to
// launch it in a goroutine.
type Server struct {
        addr string
        srv  *http.Server
}

// New creates a stub server listening on the given port.
func New(port int) *Server {
        addr := fmt.Sprintf(":%d", port)
        s := &Server{addr: addr}
        mux := http.NewServeMux()
        s.registerRoutes(mux)
        s.srv = &http.Server{
                Addr:         addr,
                Handler:      mux,
                ReadTimeout:  10 * time.Second,
                WriteTimeout: 10 * time.Second,
        }
        return s
}

// Start launches the stub server in the current goroutine (blocks). Typically
// called as `go stub.Start(port)` from main.go.
func (s *Server) Start() {
        slog.Info("LW stub server starting (development only)", "addr", s.addr)
        if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                slog.Error("LW stub server failed", "error", err)
        }
}

// registerRoutes wires every endpoint to its handler.
func (s *Server) registerRoutes(mux *http.ServeMux) {
        // External router endpoints (forwarded to AKB/AZMK/DIN/ASAN by real LW)
        mux.HandleFunc("/api/router/personal-info", s.handlePersonalInfo)
        mux.HandleFunc("/api/router/akb-score", s.handleAkbScore)
        mux.HandleFunc("/api/router/akb-history", s.handleAkbHistory)
        mux.HandleFunc("/api/router/azmk-blacklist", s.handleAzmkBlacklist)
        mux.HandleFunc("/api/router/asan-finance", s.handleAsanFinance)

        // LW own operations
        mux.HandleFunc("/api/lw/blacklist", s.handleLwBlacklist)
        mux.HandleFunc("/api/lw/loans", s.handleLwLoans)           // GET ?pin=...
        mux.HandleFunc("/api/lw/loans/approve", s.handleLwApprove) // POST

        // PR #64: MyGov endpoints (employment + pension data)
        // Real MyGov is a separate service, but for stub mode we serve it here
        // so the MyGov HTTPProvider can be pointed at the same stub URL.
        mux.HandleFunc("/api/mygov/permission/generate", s.handleMyGovGenerateLink) // POST
        mux.HandleFunc("/api/mygov/permission/data", s.handleMyGovFetchData)       // GET ?token=...

        // Health check
        mux.HandleFunc("/stub/health", func(w http.ResponseWriter, _ *http.Request) {
                writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "lw-stub"})
        })
}

// =====================================================================
// Helpers
// =====================================================================

func writeJSON(w http.ResponseWriter, status int, body any) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
        writeJSON(w, status, map[string]string{"error": msg})
}

// scenario extracts the ?scenario= query parameter (empty when not set).
func scenario(r *http.Request) string {
        return r.URL.Query().Get("scenario")
}

// =====================================================================
// /api/router/personal-info — DIN personal info
// =====================================================================

func (s *Server) handlePersonalInfo(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        sc := scenario(r)
        switch sc {
        case "old_customer":
                // Born 1950 → age ~76 → triggers rule 3 (age > 69)
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "full_name":      "Yaşlı Müştəri",
                        "date_of_birth":  "1950-01-15",
                        "place_of_birth": "Bakı, Azərbaycan",
                        "address":        "Bakı, Nizami r.",
                })
        case "young_customer":
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "full_name":      "Gənc Müştəri",
                        "date_of_birth":  "2000-05-20",
                        "place_of_birth": "Gəncə, Azərbaycan",
                        "address":        "Gəncə,",
                })
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated DIN service error")
        case "":
                // Default: 35-year-old customer
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "full_name":      "Test Müştəri",
                        "date_of_birth":  "1991-03-10",
                        "place_of_birth": "Bakı, Azərbaycan",
                        "address":        "Bakı, Səbail r., 28 May 12",
                })
        default:
                writeError(w, http.StatusBadRequest, "stub: unknown scenario '"+sc+"' for personal-info")
        }
}

// =====================================================================
// /api/router/akb-score — AKB credit score (SOAP-derived JSON, PR #55)
// =====================================================================

func (s *Server) handleAkbScore(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        // PR #111: helper to build real AKB Score envelope (data.score)
        // point STRING kimi göndərilir (real AKB Skor servisi formatına uyğun)
        buildScoreEnvelope := func(pointStr, response string) map[string]any {
                return map[string]any{
                        "result":    0,
                        "requestId": 123,
                        "message":   "OK",
                        "data": map[string]any{
                                "score": map[string]any{
                                        "calculated": true,
                                        "point":       pointStr,  // STRING — real AKB format
                                        "response":    response,
                                },
                        },
                }
        }

        sc := scenario(r)
        switch sc {
        case "stop_factor":
                // Point=1 → stop factor present, Response=2-letter code
                writeJSON(w, http.StatusOK, buildScoreEnvelope("1", "AB"))
        case "low_score":
                // Point=150 → below 200 threshold (rule 1)
                writeJSON(w, http.StatusOK, buildScoreEnvelope("150", ""))
        case "high_score":
                // Point=750 → triggers valuable override (AKB 700+)
                writeJSON(w, http.StatusOK, buildScoreEnvelope("750", ""))
        case "no_data":
                // Point=0 → AKB returned no usable data
                writeJSON(w, http.StatusOK, buildScoreEnvelope("0", ""))
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated AKB service error")
        case "":
                // Default: normal score 650
                writeJSON(w, http.StatusOK, buildScoreEnvelope("650", ""))
        default:
                writeError(w, http.StatusBadRequest, "stub: unknown scenario '"+sc+"' for akb-score")
        }
}

// =====================================================================
// /api/router/akb-history — AKB full history (PR #52)
// =====================================================================

func (s *Server) handleAkbHistory(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        now := time.Now()
        formatPeriod := func(monthsAgo int) string {
                return now.AddDate(0, -monthsAgo, 0).Format("2006-01")
        }

        // PR #111: helper to build real AKB History envelope (data.Request.inquiryResult.*)
        buildHistoryEnvelope := func(reportID, customerName string, liabilities []map[string]any, balance float64) map[string]any {
                return map[string]any{
                        "result":    0,
                        "requestId": 456,
                        "message":   "OK",
                        "data": map[string]any{
                                "Request": map[string]any{
                                        "inquiryResult": map[string]any{
                                                "reportId":      reportID,
                                                "reportingDate": "2026-07-01T00:00:00",
                                                "borrower": map[string]any{
                                                        "fin":    fin,
                                                        "name":   customerName,
                                                        "status": "active",
                                                },
                                                "liabilities": map[string]any{
                                                        "liability": liabilities,
                                                },
                                                "score": map[string]any{
                                                        "calculated": true,
                                                        "point":       650,  // INT — history formatında point int kimi
                                                        "response":    "",
                                                },
                                                "balance": balance,
                                        },
                                        "serviceResponse": map[string]any{
                                                "code":    "0",
                                                "message": "OK",
                                        },
                                },
                        },
                }
        }

        // PR #111: helper to build a single liability (camelCase, nested history)
        buildLiability := func(id, status string, daysOverdue int, monthlyPayment float64, historyItems []map[string]any) map[string]any {
                return map[string]any{
                        "id":                   id,
                        "creditStatus":         status,
                        "daysMainSumOverdue":   daysOverdue,
                        "monthlyPaymentAmount": monthlyPayment,
                        "history": map[string]any{
                                "historyItem": historyItems,
                        },
                }
        }

        // PR #111: helper to build a history item (camelCase)
        buildHistoryItem := func(period string, overdueDays int) map[string]any {
                return map[string]any{
                        "reportingPeriod": period,
                        "overdueDays":     overdueDays,
                        "creditStatus":    "active",
                }
        }

        sc := scenario(r)
        switch sc {
        case "delay_ratio_high":
                history := []map[string]any{}
                for i := 0; i < 12; i++ {
                        history = append(history, buildHistoryItem(formatPeriod(i), 7))
                }
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-DELAY-RATIO", "Delay Ratio Customer",
                        []map[string]any{buildLiability("L-DELAY", "closed", 0, 0, history)},
                        0,
                ))

        case "active_delay_high":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-ACTIVE-DELAY", "Active Delay Customer",
                        []map[string]any{buildLiability("L-ACTIVE", "active", 10, 200, []map[string]any{})},
                        200,
                ))

        case "delay_3m":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-DELAY-3M", "Delay 3M Customer",
                        []map[string]any{buildLiability("L-3M", "closed", 0, 0, []map[string]any{
                                buildHistoryItem(formatPeriod(1), 25),
                        })},
                        0,
                ))

        case "delay_6m":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-DELAY-6M", "Delay 6M Customer",
                        []map[string]any{buildLiability("L-6M", "closed", 0, 0, []map[string]any{
                                buildHistoryItem(formatPeriod(4), 35),
                        })},
                        0,
                ))

        case "delay_12m":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-DELAY-12M", "Delay 12M Customer",
                        []map[string]any{buildLiability("L-12M", "closed", 0, 0, []map[string]any{
                                buildHistoryItem(formatPeriod(8), 50),
                        })},
                        0,
                ))

        case "delay_18m":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-DELAY-18M", "Delay 18M Customer",
                        []map[string]any{buildLiability("L-18M", "closed", 0, 0, []map[string]any{
                                buildHistoryItem(formatPeriod(14), 65),
                        })},
                        0,
                ))

        case "high_monthly_payments":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-HIGH-PAYMENTS", "High Payments Customer",
                        []map[string]any{
                                buildLiability("L-PAY1", "active", 0, 1200, []map[string]any{}),
                                buildLiability("L-PAY2", "active", 0, 900, []map[string]any{}),
                        },
                        2100,
                ))

        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated AKB history service error")

        case "", "empty":
                writeJSON(w, http.StatusOK, buildHistoryEnvelope(
                        "MOCK-CLEAN", "Clean Customer",
                        []map[string]any{},
                        0,
                ))

        default:
                writeError(w, http.StatusBadRequest, "stub: unknown scenario '"+sc+"' for akb-history")
        }
}

// =====================================================================
// /api/router/azmk-blacklist — AZMK Central Credit Register (PR #53)
// =====================================================================

func (s *Server) handleAzmkBlacklist(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        sc := scenario(r)
        switch sc {
        case "blacklisted":
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "is_blacklisted": true,
                })
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated AZMK service error")
        default:
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "is_blacklisted": false,
                })
        }
}

// =====================================================================
// /api/lw/blacklist — LW own blacklist (T-1.5)
// =====================================================================

func (s *Server) handleLwBlacklist(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        sc := scenario(r)
        switch sc {
        case "blacklisted":
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "is_blacklisted": true,
                })
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated LW blacklist service error")
        default:
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":            fin,
                        "is_blacklisted": false,
                })
        }
}

// =====================================================================
// /api/router/asan-finance — official income (T-5.1)
// =====================================================================

func (s *Server) handleAsanFinance(w http.ResponseWriter, r *http.Request) {
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        sc := scenario(r)
        switch sc {
        case "low_income":
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             fin,
                        "official_income": 200,
                        "currency":        "AZN",
                        "employer_name":   "Mock Employer",
                        "query_date":      "2026-07-01",
                })
        case "high_income":
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             fin,
                        "official_income": 1500,
                        "currency":        "AZN",
                        "employer_name":   "Mock Employer",
                        "query_date":      "2026-07-01",
                })
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated ASAN Finance service error")
        default:
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             fin,
                        "official_income": 500,
                        "currency":        "AZN",
                        "employer_name":   "Mock Employer",
                        "query_date":      "2026-07-01",
                })
        }
}

// =====================================================================
// /api/lw/loans — customer loan history (LW own DB)
// =====================================================================

func (s *Server) handleLwLoans(w http.ResponseWriter, r *http.Request) {
        pin := r.URL.Query().Get("pin")
        if pin == "" {
                writeError(w, http.StatusBadRequest, "pin query parameter is required")
                return
        }

        sc := scenario(r)
        switch sc {
        case "trusted":
                // 2 completed loans at "new" level, 0 delay, 3-month term → promotes to trusted
                writeJSON(w, http.StatusOK, map[string]any{
                        "customer_pin":       pin,
                        "has_existing_loans": true,
                        "loan_count":         2,
                        "loans": []map[string]any{
                                {
                                        "id": 1, "customer_pin": pin, "lms_loan_id": "L1", "loan_type": "consumer",
                                        "amount": 200, "term_months": 3, "start_date": "2024-01-01", "end_date": "2024-04-01",
                                        "status": "completed", "remaining_amount": 0, "was_on_time": true,
                                        "early_completion": false, "delay_days": 0, "level_at_close": "new", "closed_at": "2024-04-01",
                                },
                                {
                                        "id": 2, "customer_pin": pin, "lms_loan_id": "L2", "loan_type": "consumer",
                                        "amount": 250, "term_months": 3, "start_date": "2024-05-01", "end_date": "2024-08-01",
                                        "status": "completed", "remaining_amount": 0, "was_on_time": true,
                                        "early_completion": false, "delay_days": 0, "level_at_close": "new", "closed_at": "2024-08-01",
                                },
                        },
                })

        case "active_loan":
                // 1 active loan → triggers "has active loan" rejection
                writeJSON(w, http.StatusOK, map[string]any{
                        "customer_pin":       pin,
                        "has_existing_loans": true,
                        "loan_count":         1,
                        "loans": []map[string]any{
                                {
                                        "id": 1, "customer_pin": pin, "lms_loan_id": "L-ACTIVE", "loan_type": "consumer",
                                        "amount": 1000, "term_months": 6, "start_date": "2026-01-01", "end_date": "2026-07-01",
                                        "status": "active", "remaining_amount": 500, "was_on_time": true,
                                        "early_completion": false, "delay_days": 0, "level_at_close": "", "closed_at": "",
                                },
                        },
                })

        case "late_payment":
                // 1 completed loan with 5 days delay → triggers "late payment" rejection
                writeJSON(w, http.StatusOK, map[string]any{
                        "customer_pin":       pin,
                        "has_existing_loans": true,
                        "loan_count":         1,
                        "loans": []map[string]any{
                                {
                                        "id": 1, "customer_pin": pin, "lms_loan_id": "L-LATE", "loan_type": "consumer",
                                        "amount": 500, "term_months": 3, "start_date": "2024-01-01", "end_date": "2024-04-01",
                                        "status": "completed", "remaining_amount": 0, "was_on_time": false,
                                        "early_completion": false, "delay_days": 5, "level_at_close": "new", "closed_at": "2024-04-06",
                                },
                        },
                })

        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated LW loans service error")

        default:
                // Default: no loans (new customer)
                writeJSON(w, http.StatusOK, map[string]any{
                        "customer_pin":       pin,
                        "has_existing_loans": false,
                        "loan_count":         0,
                        "loans":              []map[string]any{},
                })
        }
}

// =====================================================================
// /api/lw/loans/approve — push approved loan to LW (T-1.1)
// =====================================================================

func (s *Server) handleLwApprove(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeError(w, http.StatusMethodNotAllowed, "POST required")
                return
        }

        var body map[string]any
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
                return
        }

        appID, _ := body["application_id"].(float64)

        sc := scenario(r)
        switch sc {
        case "contract_failed":
                writeJSON(w, http.StatusOK, map[string]any{
                        "application_id":  int(appID),
                        "contract_status": "failed",
                        "transfer_status": "failed",
                        "lms_loan_id":     "",
                })
        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated LW approve service error")
        default:
                writeJSON(w, http.StatusOK, map[string]any{
                        "application_id":  int(appID),
                        "contract_status": "signed",
                        "transfer_status": "completed",
                        "lms_loan_id":     fmt.Sprintf("STUB-LMS-%d", int(appID)),
                })
        }
}

// =====================================================================
// /api/mygov/permission/generate — MyGov permission link (PR #64)
// =====================================================================

func (s *Server) handleMyGovGenerateLink(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeError(w, http.StatusMethodNotAllowed, "POST required")
                return
        }
        fin := r.URL.Query().Get("fin")
        if fin == "" {
                writeError(w, http.StatusBadRequest, "fin query parameter is required")
                return
        }

        token := fmt.Sprintf("STUB-MYGOV-%s-%d", fin, time.Now().Unix())
        writeJSON(w, http.StatusOK, map[string]any{
                "token":     token,
                "url":       fmt.Sprintf("https://stub-mygov.example.com/permit/%s", token),
                "expires_at": time.Now().Add(30 * time.Minute).Format(time.RFC3339),
        })
}

// =====================================================================
// /api/mygov/permission/data — MyGov authorized data (PR #64)
// =====================================================================

func (s *Server) handleMyGovFetchData(w http.ResponseWriter, r *http.Request) {
        token := r.URL.Query().Get("token")
        if token == "" {
                writeError(w, http.StatusBadRequest, "token query parameter is required")
                return
        }

        now := time.Now()
        sc := scenario(r)

        switch sc {
        case "employment_ok":
                // Current job started 8 months ago → passes 6-month tenure rule
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Employed Customer",
                        "official_income": 1500.0,
                        "employer_name":   "ABC LLC",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history": []map[string]any{
                                {
                                        "employer_name": "ABC LLC",
                                        "start_date":    now.AddDate(0, -8, 0).Format("2006-01-02"),
                                        "end_date":      nil,
                                        "position":      "Engineer",
                                },
                        },
                        "disability_group": 0,
                        "is_pensioner":     false,
                        "pension_type":     "",
                })

        case "employment_short_tenure":
                // Current job 3 months + previous job 4 months, gap 10 days → combined 7 months → PASS
                // (Tests the 29-day gap rule with previous employer)
                prevEnd := now.AddDate(0, -3, -10)
                prevStart := prevEnd.AddDate(0, -4, 0)
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Recent Hire Customer",
                        "official_income": 1200.0,
                        "employer_name":   "XYZ LLC",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history": []map[string]any{
                                {
                                        "employer_name": "XYZ LLC",
                                        "start_date":    now.AddDate(0, -3, 0).Format("2006-01-02"),
                                        "end_date":      nil,
                                        "position":      "Manager",
                                },
                                {
                                        "employer_name": "Previous Corp",
                                        "start_date":    prevStart.Format("2006-01-02"),
                                        "end_date":      prevEnd.Format("2006-01-02"),
                                        "position":      "Specialist",
                                },
                        },
                        "disability_group": 0,
                        "is_pensioner":     false,
                        "pension_type":     "",
                })

        case "employment_short_tenure_long_gap":
                // Current job 3 months + previous job 4 months, gap 60 days → combined would be 7 months
                // BUT gap > 29 days → previous job NOT counted → reject
                prevEnd := now.AddDate(0, -3, -60)
                prevStart := prevEnd.AddDate(0, -4, 0)
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Long Gap Customer",
                        "official_income": 1100.0,
                        "employer_name":   "XYZ LLC",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history": []map[string]any{
                                {
                                        "employer_name": "XYZ LLC",
                                        "start_date":    now.AddDate(0, -3, 0).Format("2006-01-02"),
                                        "end_date":      nil,
                                        "position":      "Manager",
                                },
                                {
                                        "employer_name": "Previous Corp",
                                        "start_date":    prevStart.Format("2006-01-02"),
                                        "end_date":      prevEnd.Format("2006-01-02"),
                                        "position":      "Specialist",
                                },
                        },
                        "disability_group": 0,
                        "is_pensioner":     false,
                        "pension_type":     "",
                })

        case "employment_insufficient_tenure":
                // Current job 2 months, no previous job → reject (tenure < 6 months)
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "New Employee Customer",
                        "official_income": 1000.0,
                        "employer_name":   "New Company",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history": []map[string]any{
                                {
                                        "employer_name": "New Company",
                                        "start_date":    now.AddDate(0, -2, 0).Format("2006-01-02"),
                                        "end_date":      nil,
                                        "position":      "Trainee",
                                },
                        },
                        "disability_group": 0,
                        "is_pensioner":     false,
                        "pension_type":     "",
                })

        case "pension_disability_group1":
                // Pensioner with 1st group disability → auto-reject per business rule
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Disabled Pensioner",
                        "official_income": 300.0,
                        "employer_name":   "",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history":    []map[string]any{},
                        "disability_group": 1,
                        "is_pensioner":     true,
                        "pension_type":     "disability",
                })

        case "pension_disability_group2":
                // Pensioner with 2nd group disability → NOT auto-reject (only group 1 rejects)
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Pensioner Group 2",
                        "official_income": 250.0,
                        "employer_name":   "",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history":    []map[string]any{},
                        "disability_group": 2,
                        "is_pensioner":     true,
                        "pension_type":     "disability",
                })

        case "pension_age":
                // Age pensioner, no disability → NOT auto-reject
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Age Pensioner",
                        "official_income": 400.0,
                        "employer_name":   "",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history":    []map[string]any{},
                        "disability_group": 0,
                        "is_pensioner":     true,
                        "pension_type":     "age",
                })

        case "error":
                writeError(w, http.StatusBadGateway, "stub: simulated MyGov service error")

        case "":
                // Default: employment_ok scenario
                writeJSON(w, http.StatusOK, map[string]any{
                        "fin":             "PIN1",
                        "full_name":       "Default Customer",
                        "official_income": 1000.0,
                        "employer_name":   "Default LLC",
                        "address":         "Bakı",
                        "fetched_at":      now.Format(time.RFC3339),
                        "work_history": []map[string]any{
                                {
                                        "employer_name": "Default LLC",
                                        "start_date":    now.AddDate(0, -12, 0).Format("2006-01-02"),
                                        "end_date":      nil,
                                        "position":      "Worker",
                                },
                        },
                        "disability_group": 0,
                        "is_pensioner":     false,
                        "pension_type":     "",
                })

        default:
                writeError(w, http.StatusBadRequest, "stub: unknown scenario '"+sc+"' for mygov/permission/data")
        }
}

// StartInBackground launches the stub server in a goroutine and returns
// immediately. Convenience wrapper for main.go usage.
func StartInBackground(port int) {
        New(port).Start()
}

// PortFromString parses a port string with a fallback.
func PortFromString(s string, fallback int) int {
        n, err := strconv.Atoi(s)
        if err != nil || n <= 0 || n > 65535 {
                return fallback
        }
        return n
}
