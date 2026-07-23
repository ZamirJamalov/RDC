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

	sc := scenario(r)
	switch sc {
	case "stop_factor":
		// Point=1 → stop factor present, Response=2-letter code
		writeJSON(w, http.StatusOK, map[string]any{
			"fin": fin,
			"return": map[string]any{
				"response": "AB",
				"point":    1,
			},
		})
	case "low_score":
		// Point=150 → below 200 threshold (rule 1)
		writeJSON(w, http.StatusOK, map[string]any{
			"fin": fin,
			"return": map[string]any{
				"response": "",
				"point":    150,
			},
		})
	case "high_score":
		// Point=750 → triggers valuable override (AKB 700+)
		writeJSON(w, http.StatusOK, map[string]any{
			"fin": fin,
			"return": map[string]any{
				"response": "",
				"point":    750,
			},
		})
	case "no_data":
		// Point=0 → AKB returned no usable data
		writeJSON(w, http.StatusOK, map[string]any{
			"fin": fin,
			"return": map[string]any{
				"response": "",
				"point":    0,
			},
		})
	case "error":
		writeError(w, http.StatusBadGateway, "stub: simulated AKB service error")
	case "":
		// Default: normal score 650
		writeJSON(w, http.StatusOK, map[string]any{
			"fin": fin,
			"return": map[string]any{
				"response": "",
				"point":    650,
			},
		})
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

	sc := scenario(r)
	switch sc {
	case "delay_ratio_high":
		// 12 months with 7 days overdue each → ratio = 84/12 = 7.0 > 6 (rule 2)
		history := []map[string]any{}
		for i := 0; i < 12; i++ {
			history = append(history, map[string]any{
				"reporting_period": formatPeriod(i),
				"overdue_days":     7,
				"credit_status":    "active",
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-DELAY-RATIO",
			"reporting_date": "2026-07-01",
			"borrower": map[string]any{
				"fin":    fin,
				"name":   "Delay Ratio Customer",
				"status": "active",
			},
			"liabilities": []map[string]any{
				{
					"id":                     "L-DELAY",
					"credit_status":          "closed",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 0,
					"history":                history,
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

	case "active_delay_high":
		// Active liability with 10 days overdue → rule 6 (>5 days)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-ACTIVE-DELAY",
			"reporting_date": "2026-07-01",
			"borrower": map[string]any{
				"fin":    fin,
				"name":   "Active Delay Customer",
				"status": "active",
			},
			"liabilities": []map[string]any{
				{
					"id":                     "L-ACTIVE",
					"credit_status":          "active",
					"days_main_sum_overdue":  10,
					"monthly_payment_amount": 200,
					"history":                []map[string]any{},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         200,
		})

	case "delay_3m":
		// 25 days overdue 1 month ago → rule 7 (>=20 in 3 months)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-DELAY-3M",
			"reporting_date": "2026-07-01",
			"borrower":       map[string]any{"fin": fin, "name": "Delay 3M Customer", "status": "active"},
			"liabilities": []map[string]any{
				{
					"id":                     "L-3M",
					"credit_status":          "closed",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 0,
					"history": []map[string]any{
						{"reporting_period": formatPeriod(1), "overdue_days": 25, "credit_status": "active"},
					},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

	case "delay_6m":
		// 35 days overdue 4 months ago → rule 8 (>=30 in 6 months)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-DELAY-6M",
			"reporting_date": "2026-07-01",
			"borrower":       map[string]any{"fin": fin, "name": "Delay 6M Customer", "status": "active"},
			"liabilities": []map[string]any{
				{
					"id":                     "L-6M",
					"credit_status":          "closed",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 0,
					"history": []map[string]any{
						{"reporting_period": formatPeriod(4), "overdue_days": 35, "credit_status": "active"},
					},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

	case "delay_12m":
		// 50 days overdue 8 months ago → rule 9 (>=45 in 12 months)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-DELAY-12M",
			"reporting_date": "2026-07-01",
			"borrower":       map[string]any{"fin": fin, "name": "Delay 12M Customer", "status": "active"},
			"liabilities": []map[string]any{
				{
					"id":                     "L-12M",
					"credit_status":          "closed",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 0,
					"history": []map[string]any{
						{"reporting_period": formatPeriod(8), "overdue_days": 50, "credit_status": "active"},
					},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

	case "delay_18m":
		// 65 days overdue 14 months ago → rule 10 (>=60 in 18 months)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-DELAY-18M",
			"reporting_date": "2026-07-01",
			"borrower":       map[string]any{"fin": fin, "name": "Delay 18M Customer", "status": "active"},
			"liabilities": []map[string]any{
				{
					"id":                     "L-18M",
					"credit_status":          "closed",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 0,
					"history": []map[string]any{
						{"reporting_period": formatPeriod(14), "overdue_days": 65, "credit_status": "active"},
					},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

	case "high_monthly_payments":
		// Two active liabilities: 1200 + 900 = 2100 > 2000 → rule 12
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-HIGH-PAYMENTS",
			"reporting_date": "2026-07-01",
			"borrower":       map[string]any{"fin": fin, "name": "High Payments Customer", "status": "active"},
			"liabilities": []map[string]any{
				{
					"id":                     "L-PAY1",
					"credit_status":          "active",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 1200,
					"history":                []map[string]any{},
				},
				{
					"id":                     "L-PAY2",
					"credit_status":          "active",
					"days_main_sum_overdue":  0,
					"monthly_payment_amount": 900,
					"history":                []map[string]any{},
				},
			},
			"inquiry_history": []map[string]any{},
			"balance":         2100,
		})

	case "error":
		writeError(w, http.StatusBadGateway, "stub: simulated AKB history service error")

	case "", "empty":
		// Default: no liabilities (clean customer)
		writeJSON(w, http.StatusOK, map[string]any{
			"report_id":      "MOCK-CLEAN",
			"reporting_date": "2026-07-01",
			"borrower": map[string]any{
				"fin":    fin,
				"name":   "Clean Customer",
				"status": "active",
			},
			"liabilities":     []map[string]any{},
			"inquiry_history": []map[string]any{},
			"balance":         0,
		})

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
