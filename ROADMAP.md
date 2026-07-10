# RDC — Gap-Filling Roadmap

**Sənəd tarixi:** 2026-07-10
**Sənədin məqsədi:** `Docs/ALPUL Flow Texniki.png` flow diaqramı ilə `source/` qovluğundakı Go kodu arasındakı uyğunsuzluqları aşkarlamaq və hər boşluğu bağlamaq üçün konkret, addım-addım icra planı təqdim etmək.

---

## 1. İcra Qısa Xülasəsi

| Metrika | Dəyər |
|---|---|
| Flow addımlarının kodda tam implement olunma dərəcəsi | **6 / 44 ≈ 14%** |
| Flow-da təsvir olunan HTTP endpoint-lərin kodda qeydiyyatda olması | **1 / 11 ≈ 9%** |
| Xarici inteqrasiyaların işlək olması (SMS, SIMA, MyGov, ASAN Finance, AKB router) | **0 / 5** |
| LW Provider interfeysi — methodlar | 10 metod var, 7-si mock-dan kənara çıxır |
| Ümumi uyğunluq səviyyəsi | **~15–20%** |

**Qısa nəticə:** Flow diaqramı production-ready bir kredit-lending sistemini təsvir edir. Mövcud kod isə yalnız **nüvə kredit qərar məntiqinin MVP-sini** implement edir. Flow-a çatmaq üçün təxmini **70–80% əlavə iş** lazımdır.

---

## 2. Hal-hazırkı Vəziyyət vs Hədəf Vəziyyət

### Mövcud kod nə edir?
- Loan application CRUD
- Aktiv kredit + ödəniş tarixi paralel yoxlaması
- Kredit səviyyəsi təyini (`new` / `trusted` / `valuable` / `elite`)
- AKB score override (yalnız request body-dən)
- Unlock phase (phase 1 / phase 2)
- Rate lookup (39 konfiqurasiya sətri)
- Manual operator approve/reject

### Mövcud kod nə etmir (amma flow tələb edir)?
- OTP / SMS təsdiq axını
- PersonalInfo (DIN) sorğusu və form data ilə müqayisə
- AKB Score / AKB History-nin **LW-dən** sorğulanması
- Blacklist yoxlamasının credit engine-ə inteqrasiyası
- ASAN Finance gəlir yoxlaması
- SIMA KYC başlatma + asinxron callback qəbul etmə
- MyGov icazə linki + data sorğusu
- 3 əlaqə nömrəsi yoxlaması
- Faktiki ünvan double-check
- Final approve-ın **LW-ə** göndərilməsi (`ApproveLoan` metodu var, amma credit engine çağırmır)
- Bütün `/api/router/*` və `/api/lw/*` HTTP route-larının qeydiyyatı

---

## 3. Gap Analysis — Detallı Boşluq Siyahısı

### 3.1 OTP / SMS Axını (Flow addım 1–6)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-01 | Müştəri məlumat daxil edir (ad, soyad, ünvan, anket, mobil) | `model.CreateApplicationRequest` mövcuddur | ✅ Var |
| G-02 | `POST /api/applications/init` + OTP generasiya | `POST /api/applications` var, OTP yoxdur | ⚠️ Path fərqli, OTP yox |
| G-03 | SMS/OTP servisi çağırışı | Heç bir SMS paket/interface/model yoxdur | ❌ Tamamilə yox |
| G-04 | `POST /api/otp/verify` endpoint-i | Route, handler, service yoxdur | ❌ Tamamilə yox |

### 3.2 LW Router İnteqrasiyaları (Flow addım 7–19)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-05 | `GET /api/router/personal-info` | `lw.Provider.GetPersonalInfo` interface var, mock error qaytarır, route yox | ⚠️ Stub |
| G-06 | `GET /api/router/akb-score` | `lw.Provider.GetAkbScore` interface var, mock 0 qaytarır, route yox | ⚠️ Stub |
| G-07 | `GET /api/router/akb-history` | `lw.Provider.GetAkbHistory` interface var, mock error, route yox | ⚠️ Stub |
| G-08 | `GET /api/lw/blacklist` | `lw.Provider.CheckBlacklist` interface var, mock həmişə `false`, route yox | ⚠️ Stub |
| G-09 | "Kasım nöqtə" qərarı (4 yoxlama) | Aktiv kredit + ödəniş tarixi (2 yoxlama) | ❌ 2/4 əskik |
| G-10 | Təklif limitləri göstərilir | `GetLevelRanges` repo-da var, endpoint yox | ❌ Endpoint yox |

### 3.3 SIMA KYC (Flow addım 20–27)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-11 | Müştəri məbləğ/müddət + kart seçir | Amount, TermMonths var, kart nömrəsi yox | ⚠️ Kart yox |
| G-12 | `POST /api/applications/confirm` | Yoxdur | ❌ Tamamilə yox |
| G-13 | SIMA üçün SMS göndərilir | SMS servisi yox | ❌ Tamamilə yox |
| G-14 | `POST /api/router/sima/init` | `lw.Provider.InitSimaKyc` interface var, mock error, route yox | ⚠️ Stub |
| G-15 | `POST /api/rdc/callback/sima-result` | Callback handler yox | ❌ Tamamilə yox |

### 3.4 MyGov İnteqrasiyası (Flow addım 28–32)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-16 | Kredit eksperti müraciəti açır | `PUT /api/applications/{id}/status` sadələşdirilmiş var | ⚠️ Sadələşdirilmiş |
| G-17 | MyGov linki göndər | Interface, model, handler yox | ❌ Tamamilə yox |
| G-18 | MyGov-a icazə keçidi | Yox | ❌ Tamamilə yox |

### 3.5 ASAN Finance Gəlir Yoxlaması (Flow addım 33–39)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-19 | `GET /api/router/asan-finance` | `lw.Provider.GetAsanFinance` interface var, mock error, route yox | ⚠️ Stub |
| G-20 | Gəlir əsaslı "Kasım nöqtə" | Credit engine-də gəlir yoxlaması yox | ❌ Tamamilə yox |

### 3.6 Yekun Rəsmiləşdirmə (Flow addım 40–44)

| # | Flow tələbi | Kod statusu | Fərq |
|---|---|---|---|
| G-21 | 3 əlaqə nömrəsinin yoxlanması | Model-də contact field yox | ❌ Yox |
| G-22 | Faktiki ünvan double-check | Ünvan validation yox | ❌ Yox |
| G-23 | `POST /api/lw/loans/approve` çağırışı | `lw.Provider.ApproveLoan` interface + mock var, **credit engine çağırmır** | ❌ Çağırılmır |
| G-24 | LW müqavilə imzalama + kart transfer | Mock cavab var, real inteqrasiya yox | ⚠️ Mock |

### 3.7 Arxitektura / Əlavə

| # | Mövcud problem | Təsir |
|---|---|---|
| G-25 | `main.go` hər startda `DROP TABLE IF EXISTS` icra edir | Production-da data itkisi |
| G-26 | DB credentials `config.go`-da hardcoded | Təhlükəsizlik riski |
| G-27 | Test faylları yoxdur (`*_test.go`) | Refactor zamanı regression riski |
| G-28 | Async engine error-ları discard olunur (`_ = procErr`) | Səhvlər görünmür |
| G-29 | Dead code: `pkg/lms/`, `mock_lms_repo.go`, `mock_lms_service.go`, `mock_lms.go` | Texniki borc |
| G-30 | Graceful shutdown yoxdur | Active request-lər kəsilir |
| G-31 | Structured logging yoxdur | Production debugging çətin |
| G-32 | HTTP middleware yoxdur (CORS, auth, rate-limit, request-ID) | Production tələbi |
| G-33 | Magic strings (`"approved"`, `"rejected"`, `"pending_approval"`) | Typo riski |

---

## 4. Yol Xəritəsi (Phased Roadmap)

### Phase 0 — Hazırlıq (1–2 gün)

**Məqsəd:** Texniki borcları təmizləmək və davamlı development üçün baza yaratmaq.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-0.1 | `source/internal/model/status.go` (yeni) | `StatusPending`, `StatusChecking`, `StatusApproved`, `StatusRejected`, `StatusPendingApproval` typed constants |
| T-0.2 | `source/main.go` | `runMigrations`-ı `IF NOT EXISTS` pattern-inə keçir; drop əmrlərini yalnız dev modda işlət (env flag ilə) |
| T-0.3 | `source/config/config.go` | Hardcoded DB credentials-i sil; `LOG_FATAL` qoymadan env olmasa xəta ver |
| T-0.4 | `source/` | Dead code sil: `pkg/lms/`, `internal/repository/mock_lms_repo.go`, `internal/service/mock_lms_service.go`, `internal/model/mock_lms.go` |
| T-0.5 | `source/go.mod` | `log/slog` və ya `go.uber.org/zap` əlavə et |
| T-0.6 | `source/main.go` | Graceful shutdown əlavə et (`os.Signal` + `http.Server.Shutdown`) |
| T-0.7 | `source/internal/middleware/` (yeni) | `RequestID`, `Recovery`, `Logger` middleware-ləri |
| T-0.8 | `source/internal/handler/application_handler.go` | `http.HandleFunc` → `http.Handler` + middleware chain |
| T-0.9 | `source/internal/service/credit_engine_test.go` (yeni) | `determineCreditLevel`, `computeAnalytics` üçün unit test |
| T-0.10 | `source/internal/service/application_service_test.go` (yeni) | `CreateApplication`, `UpdateStatus` üçün unit test (mock repo ilə) |

### Phase 1 — Kritik Düzəlişlər (2–3 gün)

**Məqsəd:** Mövcud credit engine-də ən kritik 2 boşluğu bağlamaq.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-1.1 | `source/internal/service/credit_engine.go` | `ProcessApplication` sonunda (approve halında) `lwProvider.ApproveLoan(...)` çağır; uğursuz olarsa status-u `rejected`-a çevir və reject reason yaz |
| T-1.2 | `source/internal/service/credit_engine.go` | Async goroutine-də `_ = procErr`-ı `slog.Error` ilə əvəz et; retry mexanizmi əlavə et (max 3 cəhd, exponential backoff) |
| T-1.3 | `source/internal/service/credit_engine.go` | `ProcessApplication`-ı tam transaction içində işlət (`appRepo.WithTx`) |
| T-1.4 | `source/internal/repository/application_repo.go` | `WithTx(ctx, fn)` helper metodu əlavə et |
| T-1.5 | `source/internal/service/credit_engine.go` | `ProcessApplication`-a `CheckBlacklist` çağırışı əlavə et; blacklisted → reject |
| T-1.6 | `source/internal/service/credit_engine.go` | AKB score-u request body-dən deyil, `lwProvider.GetAkbScore`-dan al (fallback: request-dən) |

### Phase 2 — LW Router Endpoint-ləri (3–4 gün)

**Məqsəd:** Bütün `/api/router/*` və `/api/lw/*` endpoint-lərini HTTP route qeydiyyatına salmaq.

| Tapşırıq | Endpoint | Handler / Service | Təsvir |
|---|---|---|---|
| T-2.1 | `GET /api/router/personal-info` | `handler.LWRouterHandler.PersonalInfo` | LW-dən DIN məlumatını çəkib qaytar |
| T-2.2 | `GET /api/router/akb-score` | `handler.LWRouterHandler.AkbScore` | LW-dən AKB score çək |
| T-2.3 | `GET /api/router/akb-history` | `handler.LWRouterHandler.AkbHistory` | LW-dən AKB history çək |
| T-2.4 | `GET /api/lw/blacklist` | `handler.LWRouterHandler.Blacklist` | LW-dən blacklist status çək |
| T-2.5 | `GET /api/router/asan-finance` | `handler.LWRouterHandler.AsanFinance` | LW-dən rəsmi gəlir çək |
| T-2.6 | `POST /api/lw/loans/approve` | `handler.LWRouterHandler.ApproveLoan` | LW-ə approve paketi göndər |
| T-2.7 | `POST /api/router/sima/init` | `handler.LWRouterHandler.SimaInit` | SIMA KYC prosesi başlat |
| T-2.8 | `POST /api/rdc/callback/sima-result` | `handler.LWCallbackHandler.SimaResult` | SIMA-dan gələn callback qəbul et |
| T-2.9 | `source/internal/handler/lw_router_handler.go` (yeni) | Yuxarıdakı handler-ləri birləşdirən fayl |
| T-2.10 | `source/internal/handler/lw_callback_handler.go` (yeni) | Callback-lər üçün ayrı handler |
| T-2.11 | `source/pkg/lw/http_provider.go` (yeni) | `MockProvider` ilə paralel real HTTP implementation |
| T-2.12 | `source/config/config.go` | `LWBaseURL`, `LWApiKey`, `UseMockLW` env-lərini əlavə et |
| T-2.13 | `source/main.go` | `lw.NewMockProvider` → `lw.NewHTTPProvider` (config-a bağlı) |

### Phase 3 — OTP / SMS Axını (3–4 gün)

**Məqsəd:** Müştəri təsdiq və SIMA üçün OTP göndərmək.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-3.1 | `source/pkg/otp/provider.go` (yeni) | `OTPProvider` interface: `Send(phone) (string, error)`, `Verify(phone, code) (bool, error)` |
| T-3.2 | `source/pkg/otp/mock_provider.go` (yeni) | Dev üçün kodu log-a yazan mock |
| T-3.3 | `source/pkg/otp/http_provider.go` (yeni) | Real SMS gateway (örn. Clickatell, Twilio, lokal AZ provider) |
| T-3.4 | `source/internal/model/otp.go` (yeni) | `OTPRequest`, `OTPVerifyRequest`, `OTPResponse` modelləri |
| T-3.5 | `source/migrations/002_otp_codes.sql` (yeni) | `otp_codes` cədvəli: `phone`, `code_hash`, `expires_at`, `consumed_at`, `attempts` |
| T-3.6 | `source/internal/repository/otp_repo.go` (yeni) | Create, Consume, IncrementAttempts |
| T-3.7 | `source/internal/service/otp_service.go` (yeni) | `SendOTP`, `VerifyOTP` (rate-limit: 1 SMS/dəq, 5 cəhd) |
| T-3.8 | `source/internal/handler/otp_handler.go` (yeni) | `POST /api/otp/send`, `POST /api/otp/verify` |
| T-3.9 | `source/main.go` | Route-ları qeydiyyatdan keçir |
| T-3.10 | `source/internal/service/application_service.go` | `CreateApplication`-a OTP verify tələbini əlavə et (verified OTP token olmadan application create olunmasın) |

### Phase 4 — SIMA KYC + MyGov (4–5 gün)

**Məqsəd:** SIMA KYC axınını tam başlatmaq və callback qəbul etmək; MyGov icazə axınını qurmaq.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-4.1 | `source/pkg/sima/provider.go` (yeni) | `SimaProvider`: `InitKyc(appID, fin) (*SimaSession, error)`, `GetResult(sessionID) (*SimaResult, error)` |
| T-4.2 | `source/pkg/sima/http_provider.go` (yeni) | SIMA-nın real API-sinə HTTP çağırışı |
| T-4.3 | `source/migrations/003_sima_sessions.sql` (yeni) | `sima_sessions` cədvəli: `application_id`, `session_id`, `status`, `started_at`, `completed_at`, `result_json` |
| T-4.4 | `source/internal/repository/sima_repo.go` (yeni) | CRUD for sima_sessions |
| T-4.5 | `source/internal/service/sima_service.go` (yeni) | `InitKyc`, `HandleCallback`, `PollResult` (optional) |
| T-4.6 | `source/internal/handler/lw_callback_handler.go` | `SimaResult` handler-i: payload pars et, repo-ya yaz, application status-u `kyc_completed`-a update et |
| T-4.7 | `source/internal/service/credit_engine.go` | Pipeline-a `sima_check` əlavə et; SIMA status `success` deyilsə reject |
| T-4.8 | `source/pkg/mygov/provider.go` (yeni) | `MyGovProvider`: `GeneratePermissionLink(fin)`, `FetchAuthorizedData(token)` |
| T-4.9 | `source/migrations/004_mygov_permissions.sql` (yeni) | `mygov_permissions` cədvəli: `application_id`, `permission_token`, `link_expires_at`, `data_fetched_at` |
| T-4.10 | `source/internal/service/mygov_service.go` (yeni) | `GenerateLink`, `FetchData` (callback və ya poll) |
| T-4.11 | `source/internal/handler/mygov_handler.go` (yeni) | `POST /api/mygov/permission-link`, `GET /api/mygov/callback` |
| T-4.12 | `source/main.go` | Route-ları qeydiyyatdan keçir |

### Phase 5 — ASAN Finance Gəlir + Kredit Eksperti Workflow (3–4 gün)

**Məqsəd:** Gəlir əsaslı qərar məntiqi və operator üçün ekspert paneli.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-5.1 | `source/internal/service/credit_engine.go` | `ProcessApplication`-a `asan_finance_check` əlavə et; rəsmi gəlir tələb olunan minimumdan azdırsa reject |
| T-5.2 | `source/config/config.go` | `MinOfficialIncomeAZN` config-i əlavə et |
| T-5.3 | `source/internal/model/loan_application.go` | `OfficialIncome`, `Contact1Phone`, `Contact2Phone`, `Contact3Phone`, `ActualAddress` sahələrini əlavə et |
| T-5.4 | `source/migrations/005_application_extra_fields.sql` (yeni) | Loan_applications cədvəlinə yeni sütunlar əlavə et (ALTER TABLE) |
| T-5.5 | `source/internal/service/contact_check_service.go` (yeni) | 3 əlaqə nömrəsinin bir-birindən fərqli olmasını və PIN sahibi nömrəsi olmamasını yoxla |
| T-5.6 | `source/internal/service/credit_engine.go` | Pipeline-a `contacts_check`, `address_check` əlavə et |
| T-5.7 | `source/internal/handler/expert_handler.go` (yeni) | Ekspert üçün: `GET /api/expert/queue` (pending_approval applications), `POST /api/expert/{id}/approve-with-mygov` |
| T-5.8 | `source/main.go` | Route-ları qeydiyyatdan keçir |

### Phase 6 — Tam Pipeline İnteqrasiyası və Test (3–4 gün)

**Məqsəd:** Bütün axını uçtan-uca test etmək və production-a hazır vəziyyətə gətirmək.

| Tapşırıq | Fayl | Təsvir |
|---|---|---|
| T-6.1 | `source/internal/service/credit_engine.go` | Pipeline-ı yenidən təşkil et: OTP → PersonalInfo → AKB Score → AKB History → Blacklist → SIMA → MyGov → ASAN Finance → Contacts → Address → Decision → LW Approve |
| T-6.2 | `source/internal/service/credit_engine.go` | Paralelləşdirmə: PersonalInfo + AKB Score + AKB History + Blacklist eyni goroutine-də (errgroup ilə) |
| T-6.3 | `source/internal/service/credit_engine.go` | `go.uber.org/errgroup` paketini istifadə et |
| T-6.4 | `source/internal/handler/application_handler.go` | `POST /api/applications/init` (OTP + initial fields), `POST /api/applications/confirm` (amount + term + card) ayır |
| T-6.5 | `source/internal/handler/application_handler.go` | `GET /api/applications/{id}/offer` endpoint-i: müştəriyə təklif olunan məbləğ/müddət aralığını qaytar (`GetLevelRanges` əsasında) |
| T-6.6 | `source/test/integration/` (yeni) | Uçtan-uca integration test: OTP → init → confirm → SIMA mock → approve |
| T-6.7 | `source/internal/service/credit_engine_test.go` | Hər qərar qolunu əhatə edən test case-lər (table-driven) |
| T-6.8 | `.github/workflows/ci.yml` (yeni) | GitHub Actions: `go vet`, `go test`, `golangci-lint` |
| T-6.9 | `Dockerfile` (yeni) | Multi-stage build |
| T-6.10 | `docker-compose.yml` (yeni) | app + SQL Server + (optional) SMS mock |
| T-6.11 | `.env.example` (yeni) | Bütün env variable-ların nümunəsi |
| T-6.12 | `README.md` | Setup, env, run, test, deploy təlimatları |

---

## 5. Fayl Düzəlişləri Cədvəli

### Silinəcək fayllar (Phase 0)
- `source/pkg/lms/provider.go`
- `source/internal/repository/mock_lms_repo.go`
- `source/internal/service/mock_lms_service.go`
- `source/internal/model/mock_lms.go`

### Dəyişdiriləcək fayllar
- `source/main.go` — routing, middleware, graceful shutdown, provider seçimi
- `source/config/config.go` — yeni env-lər, hardcoded credential silinməsi
- `source/internal/service/credit_engine.go` — pipeline genişləndirilməsi, LW ApproveLoan çağırışı, paralelləşdirmə
- `source/internal/service/application_service.go` — OTP tələbi, init/confirm ayrılması
- `source/internal/handler/application_handler.go` — yeni endpoint-lər
- `source/internal/model/loan_application.go` — yeni sahələr
- `source/internal/repository/application_repo.go` — `WithTx` helper, yeni sahələr
- `source/migrations/001_init.sql` — drop-only-dev moduna keçid
- `source/pkg/lw/mock_provider.go` — bütün metodlar dolu mock qaytarsın (error yox)
- `README.md` — tam setup təlimatı

### Yaradılacaq yeni fayllar (ümumi: ~30 fayl)

**Modellər:**
- `source/internal/model/status.go`
- `source/internal/model/otp.go`
- `source/internal/model/sima.go`
- `source/internal/model/mygov.go`

**Repository-lər:**
- `source/internal/repository/otp_repo.go`
- `source/internal/repository/sima_repo.go`
- `source/internal/repository/mygov_repo.go`

**Servislər:**
- `source/internal/service/otp_service.go`
- `source/internal/service/sima_service.go`
- `source/internal/service/mygov_service.go`
- `source/internal/service/contact_check_service.go`

**Handler-lər:**
- `source/internal/handler/otp_handler.go`
- `source/internal/handler/lw_router_handler.go`
- `source/internal/handler/lw_callback_handler.go`
- `source/internal/handler/mygov_handler.go`
- `source/internal/handler/expert_handler.go`

**Paketlər (external providers):**
- `source/pkg/otp/provider.go`
- `source/pkg/otp/mock_provider.go`
- `source/pkg/otp/http_provider.go`
- `source/pkg/sima/provider.go`
- `source/pkg/sima/http_provider.go`
- `source/pkg/mygov/provider.go`
- `source/pkg/lw/http_provider.go`

**Middleware:**
- `source/internal/middleware/request_id.go`
- `source/internal/middleware/logger.go`
- `source/internal/middleware/recovery.go`

**Migrations:**
- `source/migrations/002_otp_codes.sql`
- `source/migrations/003_sima_sessions.sql`
- `source/migrations/004_mygov_permissions.sql`
- `source/migrations/005_application_extra_fields.sql`

**Test/DevOps:**
- `source/internal/service/credit_engine_test.go`
- `source/internal/service/application_service_test.go`
- `source/test/integration/e2e_test.go`
- `.github/workflows/ci.yml`
- `Dockerfile`
- `docker-compose.yml`
- `.env.example`

---

## 6. Prioritet Matrisi

| Gap ID | Təsir (1-5) | Səyl (1-5) | Risk | Prioritet |
|---|---|---|---|---|
| G-23 (LW ApproveLoan çağırılmır) | 5 | 1 | Yüksək | 🔴 P0 — Dərhal |
| G-25 (hər startda DROP) | 5 | 1 | Yüksək | 🔴 P0 — Dərhal |
| G-26 (hardcoded credentials) | 5 | 1 | Yüksək | 🔴 P0 — Dərhal |
| G-28 (async error discard) | 4 | 1 | Orta | 🔴 P0 — Dərhal |
| G-05,06,07,08 (LW Router stubs) | 5 | 3 | Orta | 🟠 P1 — Phase 2 |
| G-09 (Kasım nöqtə 4 yoxlama) | 5 | 3 | Orta | 🟠 P1 — Phase 2 |
| G-03,04 (OTP axını) | 4 | 3 | Orta | 🟠 P1 — Phase 3 |
| G-14,15 (SIMA KYC) | 4 | 4 | Yüksək | 🟡 P2 — Phase 4 |
| G-17,18 (MyGov) | 3 | 4 | Yüksək | 🟡 P2 — Phase 4 |
| G-19,20 (ASAN Finance) | 4 | 2 | Aşağı | 🟡 P2 — Phase 5 |
| G-21,22 (3 əlaqə nömrəsi, ünvan) | 3 | 2 | Aşağı | 🟢 P3 — Phase 5 |
| G-27 (test yoxdur) | 4 | 3 | Orta | 🟢 P3 — Davamlı |
| G-29 (dead code) | 2 | 1 | Aşağı | 🟢 P3 — Phase 0 |
| G-30,31,32 (prod readiness) | 3 | 2 | Orta | 🟢 P3 — Phase 0/6 |

---

## 7. Təxmini İş Yükü

| Phase | Təxmini gün | Təxmini saat (1 gün = 6 saat) |
|---|---|---|
| Phase 0 — Hazırlıq | 1–2 gün | 6–12 saat |
| Phase 1 — Kritik düzəlişlər | 2–3 gün | 12–18 saat |
| Phase 2 — LW Router endpoint-ləri | 3–4 gün | 18–24 saat |
| Phase 3 — OTP/SMS axını | 3–4 gün | 18–24 saat |
| Phase 4 — SIMA + MyGov | 4–5 gün | 24–30 saat |
| Phase 5 — ASAN + ekspert workflow | 3–4 gün | 18–24 saat |
| Phase 6 — İnteqrasiya + test + DevOps | 3–4 gün | 18–24 saat |
| **Cəmi** | **19–26 iş günü** | **~114–156 saat** |

Təxmini **4–6 həftə** (1 developer, full-time) — bu, yalnız kod yazma vaxtıdır. LW/SIMA/MyGov/ASAN inteqrasiya sənədlərinin əldə edilməsi və test edilməsi üçün əlavə 1–2 həfə nəzərə alınmalıdır.

---

## 8. Uyğunluq Artım Proqnozu

| Mərhələ bitdikdən sonra | Proqnozlaşdırılan uyğunluq |
|---|---|
| Phase 0 bitikdən sonra | ~20% → texniki baza təmizlənib |
| Phase 1 bitikdən sonra | ~30% → kritik approve axını bağlanıb |
| Phase 2 bitikdən sonra | ~50% → LW router endpoint-ləri açılıb |
| Phase 3 bitikdən sonra | ~60% → OTP axını əlavə olunub |
| Phase 4 bitikdən sonra | ~80% → SIMA + MyGov tamamlanıb |
| Phase 5 bitikdən sonra | ~90% → ASAN + contacts + address əlavə olunub |
| Phase 6 bitikdən sonra | ~95% → integration test + DevOps tamamlanıb |

Qalan 5% — production deployment, monitoring, alerting, load-testing kimi operasional işlərdir.

---

## 9. Tələb Olunan Xarici Sənədlər

Aşağıdakı inteqrasiyalar üçün rəsmi API sənədləri tələb olunur. Bunlar olmadan Phase 2, 4 və 5 başlaya bilməz:

1. **LW Sistemi API sənədləri** — bütün `/api/router/*` və `/api/lw/*` endpoint-ləri üçün request/response format, auth scheme
2. **SMS/OTP provider API sənədləri** — lokal AZ provider (örn. Azercell, Nar, Bakcell) və ya beynəlxalq (Clickatell, Twilio)
3. **SIMA KYC API sənədləri** — init, callback, result format
4. **MyGov İcazə API sənədləri** — permission link generation, authorized data fetch
5. **ASAN Finance API sənədləri** — gəlir məlumatı sorğusu formatı
6. **AKB (Azərbaycan Kredit Bürosu) API sənədləri** — score və history sorğu formatı

---

## 10. Risklər və Mitiqasiya

| Risk | Ehtimal | Təsir | Mitiqasiya |
|---|---|---|---|
| Xarici inteqrasiya sənədləri gecikər | Yüksək | Yüksək | Mock-ları saxla, hər provider-i interface arxasında tut |
| LW real API mock-dan fərqli behavior göstərər | Orta | Yüksək | İnteqrasiya testlərini vaxtında yaz, staging mühitində LW sandbox ilə yoxla |
| Migration-lar production data-nı pozar | Orta | Çox yüksək | Migration-ları idempotent yaz, hər migration üçün rollback SQL saxla |
| Paralel goroutine-lərdə race condition | Orta | Orta | `errgroup` + mutex + test `-race` flag ilə yoxla |
| SIMA callback-ləri gecikər / itər | Yüksək | Orta | Polling fallback: callback gəlməsə 30 saniyə sonra `GetResult` çağır |
| DB connection pool yetərincə deyil | Aşağı | Orta | `sql.SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime` konfiqurasiya et |

---

## 11. Uğur Mezonları (Definition of Done)

Hər phase aşağıdakı şərtləri qarşıladıqda "bitmiş" sayılır:

- [ ] Bütün yeni kod unit testlərlə əhatə olunub (min 70% coverage)
- [ ] `go vet ./...` təmiz keçir
- [ ] `golangci-lint run` heç bir error vermir
- [ ] Yeni endpoint-lər üçün integration test yazılıb
- [ ] Mock və real provider-lər eyni interface-i implement edir
- [ ] Migration idempotentdir (iki dəfə işlənsə belə xəta vermir)
- [ ] README-də yeni funksionallıq sənədləşdirilib
- [ ] `.env.example` yenilənib
- [ ] CI pipeline yaşıl keçir

---

## 12. Növbəti Addımlar

1. **Bu sənədi** team ilə review et
2. **Phase 0**-ı dərhal başlat — texniki borcların təmizlənməsi heç bir xarici asılılıq tələb etmir
3. **LW API sənədlərini** əldə etmək üçün LW sahibi ilə əlaqə saxla (Phase 2 bloklanır)
4. **SIMA, MyGov, ASAN Finance** inteqrasiya sənədləri üçün müvafiq qurumlarla razılıq al
5. Hər phase bitdikdən sonra demo sessiyası keçir və flow diaqramı ilə yenidən müqayisə et

---

*Bu sənəd `Docs/ALPUL Flow Texniki.png` diaqramı əsasında hazırlanmışdır. Hər hansı flow dəyişikliyi zamanı bu sənəd yenilənməlidir.*
