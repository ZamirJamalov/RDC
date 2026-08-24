package config

import (
        "fmt"
        "log/slog"
        "os"
        "strings"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
        // DB connection
        DBHost     string
        DBPort     string
        DBUser     string
        DBPassword string
        DBName     string

        // Server
        ServerAddr string

        // Migrations — when true, the runner will DROP and recreate tables on startup.
        // Should ONLY be true in dev/test environments. In production this must be false
        // or you will lose all data on every restart.
        MigrationsDropRecreate bool

        // Log level: "debug", "info", "warn", "error"
        LogLevel string

        // LW Provider configuration (T-2.12)
        // When UseMockLW is true, the LW provider reads from the local DB (mock_lms_loans
        // table) and returns canned responses for router endpoints. When false, the
        // HTTPProvider makes real HTTP calls to LWBaseURL with LWApiKey.
        //
        // PR #61: When UseStubLW is true, an in-process stub HTTP server is started
        // (pkg/stub) that mimics the real LW router responses. The HTTPProvider
        // points to it (LWBaseURL is overridden to http://localhost:{StubLWPort}).
        // Use this when the real LW router is not yet available but you want to
        // exercise the full HTTP provider code path (timeouts, error handling,
        // scenario-based responses via ?scenario= query param).
        //
        // Mode matrix:
        //   UseMockLW=true  UseStubLW=false → MockProvider (local DB, no HTTP)
        //   UseMockLW=false UseStubLW=true  → HTTPProvider + in-process stub server
        //   UseMockLW=false UseStubLW=false → HTTPProvider + real LW router
        LWBaseURL   string
        LWApiKey    string
        UseMockLW   bool
        LWTimeoutS  int // HTTP timeout for LW calls, in seconds
        UseStubLW   bool
        StubLWPort  int // port for the in-process stub server (default 8090)

        // OTP Provider configuration (T-3.1 to T-3.3)
        // When OTPUseMock is true, the OTP provider logs codes instead of sending SMS.
        // When false, the HTTPProvider calls a real SMS gateway at OTPBaseURL.
        OTPBaseURL  string
        OTPApiKey   string
        OTPSender   string // sender ID shown on the customer's phone
        OTPUseMock  bool
        OTPTimeoutS int // HTTP timeout for OTP calls, in seconds

        // SIMA Provider configuration (T-4.1 to T-4.2)
        SimaBaseURL  string
        SimaApiKey   string
        SimaUseMock  bool
        SimaTimeoutS int

        // PR #284: SIMA KYC biometrik link — SMS-də göndərilən URL (boşsa SMS getmir)
        SimaKycWebURL string

        // MyGov Provider configuration (T-4.8)
        MyGovBaseURL  string
        MyGovApiKey   string
        MyGovUseMock  bool
        MyGovTimeoutS int

        // MyGov Deeplink configuration
        MyGovClientID    string // UUID provided by IDDA
        MyGovRedirectURI string // Partner redirect URI after consent approval
        MyGovWebURL      string // Web URL for SMS (netlify app that triggers mygov:// deeplink)

        // Phase 5: income + contacts validation (T-5.2)
        MinOfficialIncomeAZN float64 // minimum official income required for approval

        // PR #116: AZMK Online Lending Service
        AzmkBaseURL       string  // https://web.azmk.az:7077/LW_CREDIT_HOUSE/services/OnlineLendingService
        AzmkBranchCode    string  // HO (default, dəyişilə bilər)
        AzmkProductID     string  // L07 (default, config-dən dəyişilə bilər)
        AzmkCardExpiring  string  // 2030-01-01 (həmişə statik)
        AzmkDisbursementFee float64 // 0 (həmişə 0, gələcəkdə dəyişə bilər)
        AzmkUseMock       bool    // mock mode (test üçün)
        AzmkTimeoutS      int     // HTTP timeout
        // PR #123: AZMK Basic Auth
        AzmkUsername      string  // AZMK servisinə qoşulma üçün username
        AzmkPassword      string  // AZMK servisinə qoşulma üçün password

        // PR #152: AZMK CustomerDataService (yaş yoxlaması üçün)
        AzmkCustomerDataURL      string // CustomerDataService URL (separate from OnlineLendingService)
        AzmkCustomerDataUseMock  bool   // mock mode (default: true)

        // PR #170: KYC verify toggle — true=KYC verify tələb olunur, false=skip
        AzmkKycVerifyEnabled     bool   // default: true

        // PR #171: Cutoff stop-on-first-fail toggle
        // true (default) = ilk kesim rədd edildikdə digərləri yoxlanılmır
        // false = bütün kesimlər həmişə yoxlanılır (birinci rədd səbəbi qaytarılır)
        CutoffStopOnFirstFail    bool   // default: true

        // PR #278: Cutoff checks enabled/disabled toggle
        // true (default) = cutoff-lar yoxlanılır
        // false = cutoff-lar TAMAMƏN SKIP olunur (heç bir kesim yoxlanılmır)
        CutoffChecksEnabled      bool   // default: true

        // PR #279: EMPLOYMENT_TENURE minimum staj (ay)
        EmploymentTenureMinMonths int // default: 6

        // PR #284: Referal SMS endirim faizi (disburse success SMS-indəki X% parametri)
        ReferralDiscountPercent int // default: 5

        // PR #142: Authentication
        AdminInitialPassword string // default admin password (used only on first startup when no users exist)
        AuthSessionTTLHours  int    // session token validity in hours (default: 8)

        // PR #149: Security hardening
        AllowedOrigin       string // CORS allowed origin (default: http://localhost:8000, prod: https://alpul.az)
        RateLimitPerMinute  int    // generic API rate limit per IP per minute (default: 60)
        OTPRateLimitPerMin  int    // OTP send rate limit per phone per minute (default: 1, already in service)
        OTPMaxAttempts      int    // max wrong OTP attempts before blocking application (default: 3)
        DiscountRatePerMin  int    // discount code validation rate limit per IP per minute (default: 5)

        // PR #188: Video record service (Kvadrat Lab demo)
        // Customer video identity verification before credit confirm.
        VideoRecordBaseURL    string // base URL, e.g. https://videodemo.kvadrat-lab.com
        VideoRecordUsername   string // basic auth username
        VideoRecordPassword   string // basic auth password
        VideoRecordUseMock    bool   // mock mode (no real HTTP calls)
        VideoRecordEnabled    bool   // master on/off toggle
        VideoRecordTimeoutS   int    // HTTP timeout
        VideoRecordWebhookURL string // webhook URL sent to video service (optional)
        VideoRecordRedirectURL string // redirect URL sent to video service (optional)
        VideoRecordPollIntervalS int // status polling interval (default: 2)
}

// Load reads configuration from environment variables. Required fields (DB_HOST,
// DB_PASSWORD) will cause a fatal error if missing — there are NO hardcoded
// defaults for credentials, ever.
func Load() *Config {
        cfg := &Config{
                DBHost:                 requireEnv("DB_HOST"),
                DBPort:                 getEnv("DB_PORT", "1433"),
                DBUser:                 requireEnv("DB_USER"),
                DBPassword:             requireEnv("DB_PASSWORD"),
                DBName:                 getEnv("DB_NAME", "RDC"),
                ServerAddr:             getEnv("SERVER_ADDR", ":8000"),
                MigrationsDropRecreate: getEnvBool("MIGRATIONS_DROP_RECREATE", true),
                LogLevel:               getEnv("LOG_LEVEL", "info"),
                LWBaseURL:              getEnv("LW_BASE_URL", "http://localhost:8080"),
                LWApiKey:               getEnv("LW_API_KEY", ""),
                UseMockLW:              getEnvBool("LW_USE_MOCK", true),
                LWTimeoutS:             getEnvInt("LW_TIMEOUT_S", 30),
                UseStubLW:              getEnvBool("LW_USE_STUB", false),
                StubLWPort:             getEnvInt("LW_STUB_PORT", 8090),
                OTPBaseURL:             getEnv("OTP_BASE_URL", "http://localhost:8081"),
                OTPApiKey:              getEnv("OTP_API_KEY", ""),
                OTPSender:              getEnv("OTP_SENDER", "RDC"),
                OTPUseMock:             getEnvBool("OTP_USE_MOCK", true),
                OTPTimeoutS:            getEnvInt("OTP_TIMEOUT_S", 10),
                SimaBaseURL:            getEnv("SIMA_BASE_URL", "http://localhost:8082"),
                SimaApiKey:             getEnv("SIMA_API_KEY", ""),
                SimaUseMock:            getEnvBool("SIMA_USE_MOCK", true),
                SimaTimeoutS:           getEnvInt("SIMA_TIMEOUT_S", 15),
                // PR #284: SIMA KYC biometrik link SMS-i üçün (boş = SMS göndərilmir)
                SimaKycWebURL:          getEnv("SIMA_KYC_WEB_URL", ""),
                MyGovBaseURL:           getEnv("MYGOV_BASE_URL", "http://localhost:8083"),
                MyGovApiKey:            getEnv("MYGOV_API_KEY", ""),
                MyGovUseMock:           getEnvBool("MYGOV_USE_MOCK", true),
                MyGovTimeoutS:          getEnvInt("MYGOV_TIMEOUT_S", 15),
                MyGovClientID:          getEnv("MYGOV_CLIENT_ID", ""),
                MyGovRedirectURI:       getEnv("MYGOV_REDIRECT_URI", "https://webhook.site/9f74dfae-92bc-458e-a3e3-b5134a9bf8bb"),
                MyGovWebURL:            getEnv("MYGOV_WEB_URL", "https://lively-pie-17ab5c.netlify.app/"),
                MinOfficialIncomeAZN:   getEnvFloat("MIN_OFFICIAL_INCOME_AZN", 300.0),

                // PR #116: AZMK Online Lending Service
                AzmkBaseURL:         getEnv("AZMK_BASE_URL", "https://web.azmk.az:7077/LW_CREDIT_HOUSE/services/OnlineLendingService"),
                AzmkBranchCode:      getEnv("AZMK_BRANCH_CODE", "HO"),
                AzmkProductID:       getEnv("AZMK_PRODUCT_ID", "L07"),
                AzmkCardExpiring:    getEnv("AZMK_CARD_EXPIRING", "2030-01-01"),
                AzmkDisbursementFee: getEnvFloat("AZMK_DISBURSEMENT_FEE", 0.0),
                AzmkUseMock:         getEnvBool("AZMK_USE_MOCK", true),
                AzmkTimeoutS:        getEnvInt("AZMK_TIMEOUT_S", 30),
                AzmkUsername:        getEnv("AZMK_USERNAME", ""),
                AzmkPassword:        getEnv("AZMK_PASSWORD", ""),

                // PR #152: AZMK CustomerDataService
                AzmkCustomerDataURL:     getEnv("AZMK_CUSTOMER_DATA_URL", "https://web.azmk.az:7077/LW_AKP/services/CustomerDataService"),
                AzmkCustomerDataUseMock: getEnvBool("AZMK_CUSTOMER_DATA_USE_MOCK", true),

                // PR #170: KYC verify toggle
                AzmkKycVerifyEnabled: getEnvBool("AZMK_KYC_VERIFY_ENABLED", true),

                // PR #171: Cutoff stop-on-first-fail toggle
                CutoffStopOnFirstFail: getEnvBool("CUTOFF_STOP_ON_FIRST_FAIL", true),

                // PR #278: Cutoff checks enabled/disabled toggle
                CutoffChecksEnabled: getEnvBool("CUTOFF_CHECKS_ENABLED", true),

                // PR #279: EMPLOYMENT_TENURE minimum staj (ay)
                EmploymentTenureMinMonths: getEnvInt("EMPLOYMENT_TENURE_MIN_MONTHS", 6),

                // PR #284: Referal SMS endirim faizi (X% parametri)
                ReferralDiscountPercent: getEnvInt("REFERRAL_DISCOUNT_PERCENT", 5),

                // PR #142: Authentication
                AdminInitialPassword: getEnv("ADMIN_INITIAL_PASSWORD", ""),
                AuthSessionTTLHours:  getEnvInt("AUTH_SESSION_TTL_HOURS", 8),

                // PR #149: Security hardening
                AllowedOrigin:      getEnv("ALLOWED_ORIGIN", "http://localhost:8000"),
                RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 60),
                OTPRateLimitPerMin: getEnvInt("OTP_RATE_LIMIT_PER_MIN", 1),
                OTPMaxAttempts:     getEnvInt("OTP_MAX_ATTEMPTS", 3),
                DiscountRatePerMin: getEnvInt("DISCOUNT_RATE_PER_MIN", 5),

                // PR #188: Video record service (parametric)
                VideoRecordBaseURL:      getEnv("VIDEO_RECORD_BASE_URL", "https://videodemo.kvadrat-lab.com"),
                VideoRecordUsername:     getEnv("VIDEO_RECORD_USERNAME", "bokt"),
                VideoRecordPassword:     getEnv("VIDEO_RECORD_PASSWORD", "ecbb82fe097a4184791e32a16238f96aa375c599d6f92ef46cd1c196b7efe7e4"),
                VideoRecordUseMock:      getEnvBool("VIDEO_RECORD_USE_MOCK", true),
                VideoRecordEnabled:      getEnvBool("VIDEO_RECORD_ENABLED", false),
                VideoRecordTimeoutS:     getEnvInt("VIDEO_RECORD_TIMEOUT_S", 30),
                VideoRecordWebhookURL:   getEnv("VIDEO_RECORD_WEBHOOK_URL", ""),
                VideoRecordRedirectURL:  getEnv("VIDEO_RECORD_REDIRECT_URL", ""),
                VideoRecordPollIntervalS: getEnvInt("VIDEO_RECORD_POLL_INTERVAL_S", 2),
        }

        if cfg.MigrationsDropRecreate {
                slog.Warn("MIGRATIONS_DROP_RECREATE is true — tables will be dropped and recreated on startup. " +
                        "Set MIGRATIONS_DROP_RECREATE=false in production!")
        }

        if !cfg.UseMockLW && cfg.LWApiKey == "" && !cfg.UseStubLW {
                slog.Warn("LW_USE_MOCK is false and LW_USE_STUB is false but LW_API_KEY is empty — real LW calls will fail authentication")
        }

        // PR #204: LW vs AZMK qarışıqlığını aradan qaldır
        // AZMK və LW fərqli servis-lərdir:
        //   AZMK = Azərbaycan Mikro-Kredit BOKT (real server: web.azmk.az:7077)
        //   LW   = Loan Warehouse (LW router, local: localhost:8080)
        // AZMK uğurlu olsa belə, LW ayrıca konfiqurasiya tələb edir.
        if !cfg.UseMockLW {
                slog.Warn("PR #204: LW_USE_MOCK=false — LW server HTTP çağırışları ediləcək",
                        "lw_base_url", cfg.LWBaseURL,
                        "azmk_url", cfg.AzmkCustomerDataURL,
                        "note", "AZMK və LW fərqli servis-lərdir. AZMK uğurlu olsa belə, LW ayrıca qurulmalıdır. "+
                                "Əgər LW server mövcud deyilsə, .env-də LW_USE_MOCK=true qurun.")
        }

        if cfg.UseMockLW && cfg.UseStubLW {
                slog.Warn("Both LW_USE_MOCK and LW_USE_STUB are true — LW_USE_MOCK wins (stub server will not be started)")
        }

        return cfg
}

// DSN returns the SQL Server connection string in go-mssqldb format.
// Uses explicit port format (avoids SQLEXPRESS browser lookup) and disables
// encryption (acceptable for local dev; in production use encrypt=true with a
// proper certificate).
func (c *Config) DSN() string {
        return fmt.Sprintf("server=%s;port=%s;user id=%s;password=%s;database=%s;encrypt=disable",
                c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName)
}

// requireEnv reads an environment variable and fatals if it is empty. Use this
// for any setting that has no safe default (credentials, hostnames, etc.).
func requireEnv(key string) string {
        value, ok := os.LookupEnv(key)
        if !ok || strings.TrimSpace(value) == "" {
                slog.Error("required environment variable is not set", "key", key)
                os.Exit(1)
        }
        return value
}

func getEnv(key, fallback string) string {
        if value, ok := os.LookupEnv(key); ok {
                return value
        }
        return fallback
}

func getEnvBool(key string, fallback bool) bool {
        if value, ok := os.LookupEnv(key); ok {
                switch strings.ToLower(value) {
                case "true", "1", "yes":
                        return true
                case "false", "0", "no":
                        return false
                }
        }
        return fallback
}

func getEnvInt(key string, fallback int) int {
        if value, ok := os.LookupEnv(key); ok {
                if n, err := parseInt(value); err == nil {
                        return n
                }
                slog.Warn("invalid integer env var, using fallback", "key", key, "value", value, "fallback", fallback)
        }
        return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
        if value, ok := os.LookupEnv(key); ok {
                if f, err := parseFloat(value); err == nil {
                        return f
                }
                slog.Warn("invalid float env var, using fallback", "key", key, "value", value, "fallback", fallback)
        }
        return fallback
}

func parseInt(s string) (int, error) {
        var n int
        _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
        return n, err
}

func parseFloat(s string) (float64, error) {
        var f float64
        _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
        return f, err
}
