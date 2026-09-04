# RDC Dead Code Analysis — PR #197

**Tarix:** 2026-08-15
**Analiz:** 122 Go fayl + 38 SQL migration + 12 web asset
**Nəticə:** ~770 sətir ölü kod + 92 KB orphan asset

---

## 📊 Xülasə

| Kateqoriya | Sayı | Təxmini sətir |
|---|---|---|
| 🔴 DEAD FILE (tamamilə ölü fayl) | 2 | ~186 |
| 🟡 DEAD EXPORT (istifadə olunmayan eksport) | ~30 | ~500 |
| 🟢 ORPHAN ASSET (istifadə olunmayan fayl) | 4 PNG | ~92 KB |
| 🔵 SUSPICIOUS (şübhəli) | 5 | dəyişkən |
| **Cəmi** | **~41** | **~770 sətir + 92 KB** |

### Wiring yoxlaması (hamısı OK):

- ✅ Bütün mock provider-lər (`pkg/{azmk,lw,otp,sima,mygov}/mock_provider.go` + `pkg/videorecord` MockProvider) `main.go` / `main_helpers.go`-da istifadə olunur
- ✅ Bütün 3 daxili test mock-ları (`mockApplicationStore`, `mockCustomerStore`, `mockLWProvider`) 200+ referans ilə istifadə olunur
- ✅ Stub server (`pkg/stub/server.go`) `main.go:newLWProvider`-da dev mode üçün qoşulub
- ✅ Discount code feature tamamilə qoşulub (handler → service → repo)
- ✅ Video record feature (PR #188) tam qoşulub (4 endpoint `router.go`-da)
- ✅ Bütün 5 info səhifə (how-to-get, how-to-pay, faq, privacy, contact) landing.html və apply.html nav-dan linkedir
- ✅ Yalnız `web/assets/alpul-logo.png` və `calc-decoration.png` istifadə olunur

---

## 🔴 DEAD FILES (tamamilə silinə bilər)

### 1. `internal/repository/service_audit_log_repo.go` (92 sətir)

**Səbəb:** `ServiceAuditLogRepo` + `NewServiceAuditLogRepo` + `Insert` + `ListByApplication` heç bir yerdə çağrılmır. AZMK və video provider-lər audit log-ları birbaşa `db.Exec` SQL insert ilə yazırlar (`pkg/azmk/provider.go:180` və `pkg/videorecord/provider.go:157`), bu repo-nu bypass edirlər. `NewServiceAuditLogRepo` `main.go`-da heç vaxt instantiate olunmur.

**Tövsiyə:** Faylı sil. Həmçinin `model.ServiceAuditLog` struct-ını sil (yalnız burada istifadə olunur).

### 2. `internal/service/contact_check_service.go` (94 sətir)

**Səbəb:** `ContactCheckService` + `NewContactCheckService` + `Check` + `CheckAddress` heç bir yerdə instantiate olunmur. T-5.5/T-5.6 kontakt/ünvan validation üçün planlaşdırılıb, amma heç vaxt credit engine-ə qoşulmayıb (`credit_checks.go` referans etmir).

**Tövsiyə:** Faylı sil. (Əgər kontakt validation hələ lazımdırsa, qoş — amma hazırda ölü kod.)

---

## 🟡 DEAD EXPORTS (simvolu silinə bilər)

### Repository Layer

| Fayl | Simvol | Səbəb |
|---|---|---|
| `repository/discount_code_repo.go` | `GetByOwnerCustomerID` | Comment-də "Future use" deyir; heç çağrılmır |
| `repository/mygov_repo.go` | `Create` (no-deeplink variant) | `CreateWithDeeplink` ilə əvəz olunub |
| `repository/mygov_repo.go` | `UpdateStatus` | Heç çağrılmır |
| `repository/application_repo.go` | `UpdateApplicationStatusTx` | Interface-də və mock-da var, amma production kod-da heç çağrılmır |
| `repository/application_repo.go` | `UpdateApplicationDiscountTx` | Define olunub, amma heç çağrılmır (interface yalnız non-Tx versiya elan edir) |
| `repository/cutoff_result_repo.go` | `ListByApplication` | Heç çağrılmır (yalnız `Insert` istifadə olunur) |
| `repository/video_record_repo.go` | `ListByApplication` | Heç çağrılmır |
| `repository/user_repo.go` | `DeleteExpiredSessions` | Heç çağrılmır |

### Service Layer

| Fayl | Simvol | Səbəb |
|---|---|---|
| `service/sima_service.go` | `InitKyc` | PR #120 SIMA KYC-ni customer-confirm-dən silindi (AZMK KYC əvəz etdi). Yalnız `HandleCallback` hələ qoşulub. |
| `service/sima_service.go` | `GetStatus` | Service package-dan kənarda heç çağrılmır |
| `service/mygov_service.go` | `GetIncome` | Heç çağrılmır |
| `service/feature_flag_service.go` | `nilSettingsStore` tipi | Yalnız compile-time interface check üçün var; heç instantiate olunmur |
| `middleware/rate_limit.go` | `RateLimitWithKey` | Eksport olunub, amma heç istifadə olunmur (yalnız `RateLimit` router.go-da istifadə olunur) |

### pkg Layer

| Fayl | Simvol | Səbəb |
|---|---|---|
| `pkg/stub/server.go` | `PortFromString` | Yalnız öz testində istifadə olunur (`TestPortFromString`) |
| `pkg/lw/provider.go` | `GetLoanStatus` metodu | Heç bir `Provider` instance-də çağrılmır. Polling endpoint `LWLoanStatusHandler.GetStatus` istifadə edir ki, bu da LW provider-i deyil, lokal `lw_loan_events` cədvəlini sorğulayır. |
| `pkg/lw/http_provider.go` | `HTTPProvider.GetLoanStatus` | Ölü interface metodunun implementasiyası |
| `pkg/lw/mock_provider.go` | `MockProvider.GetLoanStatus` | Ölü interface metodunun implementasiyası |
| `pkg/lw/model.go` | `AkbScoreResponse.HasStopFactor()` | Yalnız `StopFactorCode()` tərəfindən çağrılır (özü də ölü). Engine birbaşa `resp.Return.Point == 1` istifadə edir. |
| `pkg/lw/model.go` | `AkbScoreResponse.StopFactorCode()` | Kənardan heç çağrılmır |
| `pkg/lw/model.go` | `AkbScoreResponse.Score()` | Kənardan heç çağrılmır |
| `pkg/mygov/provider.go` | `GeneratePermissionLink` metodu | Heç çağrılmır. `MyGovService.GenerateLink` deeplink-i birbaşa `mygov.BuildDeeplink` ilə qurur. |
| `pkg/mygov/http_provider.go` | `HTTPProvider.GeneratePermissionLink` | Ölü interface metodunun implementasiyası |
| `pkg/mygov/mock_provider.go` | `MockProvider.GeneratePermissionLink` | Ölü interface metodunun implementasiyası |
| `pkg/mygov/crypto.go` | `BuildWebURL` | Heç çağrılmır. Service SMS-də `s.webURL` config-dən göndərir, `BuildWebURL` nəticəsini yox. |
| `pkg/otp/dynamic_provider.go` | `DynamicSMSProvider.ForceRefresh` | Eksport olunub, amma heç çağrılmır |
| `pkg/otp/http_provider.go` | `softlineErrorText` (private) | Heç çağrılmır |
| `pkg/otp/http_provider.go` | `softlineErrorMessage` (private) | Yalnız ölü `softlineErrorText` tərəfindən çağrılır |
| `pkg/otp/http_provider.go` | `softlineErrorMessages` map (private) | Yalnız ölü `softlineErrorMessage` istifadə edir |
| `pkg/sima/provider.go` | `InitKyc` metodu | Yalnız ölü `SimaService.InitKyc` tərəfindən çağrılır |
| `pkg/sima/provider.go` | `GetResult` metodu | Heç çağrılmır |
| `pkg/sima/http_provider.go` + `mock_provider.go` | `InitKyc`/`GetResult` impl-ləri | Ölü interface metodları |

### Model Layer

| Fayl | Simvol | Səbəb |
|---|---|---|
| `model/sima.go` | `SimaSession` struct | `repository.SimaSession` (ayrıca tip) tərəfindən kölgədə qalır; model versiyası heç referans olunmur |
| `model/sima.go` | `SimaInitRequest` struct | Heç istifadə olunmur |
| `model/sima.go` | `SimaStatusPending/Success/Failed/Expired` konstantları | Heç istifadə olunmur (SQL-də string literal istifadə olunur) |
| `model/mygov.go` | `MyGovPermission` struct | `repository.MyGovPermission` (alias tip) tərəfindən kölgədə qalır |
| `model/mygov.go` | `MyGovPermissionRequest` struct | Heç istifadə olunmur |
| `model/mygov.go` | `MyGovStatusPending/Granted/Fetched/Expired/Denied` konstantları | Heç istifadə olunmur |
| `model/lw_loan_event.go` | `LWLoanStatusPending/ContractSigned/TransferCompleted/Failed` konstantları | Heç istifadə olunmur (SQL sorğularda string literal) |
| `model/discount_code.go` | `DiscountStatusExpired` konstantı | Heç istifadə olunmur (yalnız `Active` və `Used` yoxlanılır) |
| `model/customer.go` | `CreateOrUpdateCustomerRequest` struct | Heç istifadə olunmur |
| `model/credit_level.go` | `CreditLevel` struct | Heç istifadə olunmur (repo `LevelRange` istifadə edir) |
| `model/credit_level.go` | `CreditLevelRule` struct | Heç istifadə olunmur |
| `model/service_audit_log.go` | `ServiceAuditLog` struct | Yalnız ölü `service_audit_log_repo.go` istifadə edir |

### Config Layer (`config/config.go`)

| Sahə | Səbəb |
|---|---|
| `OTPBaseURL` | Heç oxunmur (DynamicSMSProvider DB-dən oxuyur) |
| `OTPApiKey` | Heç oxunmur |
| `OTPSender` | Heç oxunmur |
| `OTPTimeoutS` | Heç oxunmur |
| `OTPMaxAttempts` | Heç oxunmur (kod `model.OTPMaxAttempts = 5` konstant istifadə edir) |
| `MinOfficialIncomeAZN` | Env-dən oxunur, amma credit engine heç vaxt bu threshold-u yoxlamır |
| `VideoRecordPollIntervalS` | Heç oxunmur (frontend poll edir, backend yox) |

---

## 🟢 ORPHAN ASSETS (silinə bilər)

`source/` root-da 4 PNG fayl, heç bir HTML/JS/Go faylı tərəfindən referans olunmur:

| Fayl | Ölçü |
|---|---|
| `source/image.png` | 15 KB |
| `source/image-Photoroom.png` | 16 KB |
| `source/image-Photoroom-256.png` | 26 KB |
| `source/image-Photoroom-kalk-soyken.png` | 35 KB |

Yalnız `source/web/assets/alpul-logo.png` və `calc-decoration.png` istifadə olunur (HTML-dən referans).

**Tövsiyə:** 4 root-level PNG-ni sil.

---

## 🔵 SUSPICIOUS (şübhəli, araşdırılmalı)

### 1. `migrations/seed_discount_code.sql` (197 sətir)

Hər startup-da `migration.Run()` tərəfindən işlədilir (bütün `.sql` fayllarını oxuyur). Test müştəriləri (`TESTA`, `TESTB`) və test discount kodları (`ALPUL-TEST01`, `ALPUL-TEST02`) DB-yə insert edir. `IF NOT EXISTS` guard-ları duplicate önləyir, amma test data **production DB**-də qalır. Fayl adında numeric prefix yoxdur, ona görə axırda işləyir.

**Tövsiyə:** `seeds/` və ya `scripts/` qovluğuna köçür ki, `migration.Run()` avtomatik işlətməsin.

### 2. `internal/handler/otp_handler_test.go` — istifadə olunmayan mock

- `mockOTPProviderForHandler` tipi define olunub (sətir 17-27), amma heç bir test-də instantiate olunmur (test-lər `NewOTPHandler(nil)` istifadə edir).
- Sətir 30-35 və 135-də `var _ = ...` compile guard-ları var (`context.Background`, `model.OTPCodeLength`, `service.NewOTPService`, `errors.New`) — yalnız unused-import xətalarını ört-basdır etmək üçün.

**Tövsiyə:** Ölü mock və compile guard-ları sil.

### 3. `service/credit_decision.go::calculateCommissionAmount` (private)

Yalnız `discount_code_service_test.go` tərəfindən çağrılır. Production kod-da heç istifadə olunmur. Ya test-only helper-dır (o halda `_test.go` faylına köçürülməli), ya da ölü production kod.

**Tövsiyə:** Test faylına köçür və ya sil.

### 4. `model.Customer.Email` sahəsi

`Email` sahəsi `customer_repo.go` tərəfindən scan/write olunur (DB persistence), amma heç bir service kodu tərəfindən populate olunmur — müştərilər yalnız `CustomerPIN`, `FullName`, `ActualAddress` ilə yaradılır. Sütun həmişə NULL saxlayır. Tamamilə ölü deyil (DB schema dəstəkləyir), amma sahə istifadə olunmur.

**Tövsiyə:** Qoş və ya model-dən sil.

### 5. SIMA service qismən ölü

`SimaService` hələ qoşuludur (`main.go:170` → `lw_callback_handler.go`), və `HandleCallback` canlıdır (`POST /api/rdc/callback/sima-result` tərəfindən çağrılır). Amma `InitKyc` və `GetStatus` metodları ölü — PR #120 comment-i `application_service_customer_confirm.go:301`-də deyir: *"SIMA KYC silindi — AZMK KYC artıq OTP-dən sonra baş verir (PR #117)"*. Async callback yolu legacy compatibility üçün qalır.

**Tövsiyə:** Əgər SIMA KYC init birdəfəlik təxirə salınıbsa, `SimaService.InitKyc`, `SimaService.GetStatus` və `sima.Provider.InitKyc`/`GetResult` interface metodlarını sil. `HandleCallback`-i saxla (uçuq callback-lər üçün).

---

## 📈 Cəmi Ölü Kod

| Kateqoriya | Sətir |
|---|---|
| Ölü Go faylları (2) | ~186 |
| Ölü eksport funksiya/tip-lər (~30 simvol) | ~500 |
| Ölü private helper-lər (softline*, calculateCommissionAmount və s.) | ~60 |
| Ölü test scaffolding (mockOTPProviderForHandler) | ~20 |
| **Cəmi Go ölü kod** | **~770 sətir** |
| Orphan PNG asset-lər | ~92 KB |

---

## 🎯 Növbəti Addımlar (tövsiyə olunan PR sırası)

1. **Quick wins (1 commit):** `source/` root-da 4 orphan PNG-ni sil.
2. **Dead files (1 commit):** `service_audit_log_repo.go` + `contact_check_service.go` + `model.ServiceAuditLog` sil.
3. **Dead config fields (1 commit):** 7 istifadə olunmayan `Config` sahəsini + `getEnv*` çağrılarını sil. PR description-da bu env var-ların artıq oxunmadığını qeyd et: `OTP_BASE_URL`, `OTP_API_KEY`, `OTP_SENDER`, `OTP_TIMEOUT_S`, `OTP_MAX_ATTEMPTS`, `MIN_OFFICIAL_INCOME_AZN`, `VIDEO_RECORD_POLL_INTERVAL_S`.
4. **Dead model types/constants (1 commit):** `SimaSession`, `SimaInitRequest`, `SimaStatus*`, `MyGovPermission`, `MyGovPermissionRequest`, `MyGovStatus*`, `LWLoanStatus*`, `DiscountStatusExpired`, `CreateOrUpdateCustomerRequest`, `CreditLevel`, `CreditLevelRule` sil.
5. **Dead repo/service methods (1 commit):** Repository/Service cədvəlindəki ~8 ölü metodu sil.
6. **Dead pkg exports (1 commit):** `PortFromString`, ölü `GetLoanStatus`/`GeneratePermissionLink` interface metodları + impl-ləri, `BuildWebURL`, `ForceRefresh`, `softlineErrorText/Message/Messages`, ölü `AkbScoreResponse` accessor-ları sil.
7. **Suspicious cleanup (1 commit):** `seed_discount_code.sql`-i `migrations/`-dan kənarlaşdır; `otp_handler_test.go` scaffolding təmizlə; SIMA service retirement qərarı ver.
8. **Araşdır:** `MinOfficialIncomeAZN` credit engine-də yoxlanılmalıdır (yüklənir, amma heç vaxt check olunmur).

---

## ⚠️ Diqqət

- **Build verify:** Hər silmədən sonra `go build ./...` və `go vet ./...` çalıştır.
- **Test:** `go test ./...` — pre-existing failures var (internal/service), amma yeni failures olmamalıdır.
- **Migration:** `seed_discount_code.sql`-i köçürəndə `migration.Run()`-ın onu artıq işlətmədiyindən əmin ol.
- **Backward compat:** Config sahələrini siləndə `.env` fayllarında hələ də o dəyişənlər ola bilər — problem deyil (sadəcə ignore olunacaq), amma dəqiqləşdirmək lazımdır.

---

*Bu analiz PR #197 üçün hazırlanmışdır. Hər bir ölü kod kateqoriyası ayrıca commit kimi silinməlidir ki, rollback asan olsun.*
