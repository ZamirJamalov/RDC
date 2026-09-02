# RDC — Tam Axın (A–Z): Sequence Diagram

> PR #359 — `source/` kod bazası əsasında. Bütün xarici inteqrasiyalar və vaxt
> (poll / timeout / limit) parametrləri diaqramda qeyd olunub.

## Tam axın — sequence diagram

```mermaid
sequenceDiagram
    autonumber
    participant C as Müştəri (apply.html)
    participant E as Ekspert (dashboard)
    participant R as RDC (Go backend)
    participant SMS as Softline SMS
    participant AK as AZMK OnlineLending
    participant AD as AZMK CustomerData
    participant V as Video (kvadrat-lab)
    participant W as Sign Worker (fon)

    rect rgb(228, 238, 253)
    Note over C,R: 1. MÜŞTƏRİ MƏRHƏLƏSİ (apply)

    C->>R: POST /api/applications/init (FIN, seriya, telefon)
    R->>SMS: OTP SMS göndər
    SMS-->>R: 200 OK (message_id)
    Note over C,SMS: OTP etibarlılığı 5 dəq, limit 1 SMS/dəq

    C->>R: POST /api/applications/init/verify (OTP kodu)
    R->>AK: POST /kyc — KYC sessiyası yaradılır
    AK-->>R: kyc_id
    loop Hər 3 saniyə — maks 60 cəhd = 3 dəq
        R->>AK: GET /kyc/ID (status sorğusu)
        AK-->>R: SENT və ya VERIFIED
    end
    Note over C: Müştəri Asan Finance ilə təsdiqləyir

    R->>AK: PUT /partner (homeAddress = -, passport = seriya)
    AK-->>R: partner_id

    R->>AD: getOwnerData (qara siyahı, aktiv kredit)
    AD-->>R: nəticə
    R->>AD: getMkrScore (AKB balı)
    AD-->>R: point, response
    R->>AD: GetPersonalInfo (yaş yoxlaması)
    AD-->>R: doğum tarixi, qeydiyyat ünvanı
    R->>AD: inquireByIdCard (öhdəliklər)
    AD-->>R: kredit tarixçəsi
    alt Kəsilmə (cutoff) baş verərsə
        R-->>C: REJECTED (səbəb kodu ilə)
    end

    C->>R: POST /api/applications/offer (məbləğ, müddət)
    R->>AD: inquireByIdCard (fail-soft, AKB DB keşdən)
    R-->>C: Kredit səviyyəsi + şərtlər (illik faiz, komissiya)

    C->>R: GET /api/discount-codes/validate (endirim kodu)
    C->>R: GET /api/applications/ID/cards
    R->>AK: GET /card/partnerId (kart siyahısı)
    AK-->>R: 18 kart (maskalı)

    C->>R: POST /video-record/start
    R->>V: order yarat (app_id, telefon, məbləğ)
    V-->>R: redirect_url
    loop Frontend poll — hər 2 saniyə
        C->>R: GET /video-record/status
        R->>V: POST /api/orders/status
        V-->>R: recorded = true / false
    end

    C->>R: POST /customer-confirm (kart, ünvan, məbləğ)
    Note over R: total_amount = məbləğ + komissiya (məs. 20 + 11% = 22.47)
    alt Yeni kart daxil edilib
        R->>AK: POST /card — RegisterCard (xəta = confirm bloklanır)
    else Köhnə kart seçilib (PR 355)
        Note over R: RegisterCard çağrılmır — CardID birbaşa
    end
    R-->>C: status = pending_expert
    end

    rect rgb(253, 244, 228)
    Note over E,R: 2. EKSPERT MƏRHƏLƏSİ (dashboard)

    E->>R: GET /api/expert/queue (növbə)
    E->>R: Detail — GET /applications/public_id (+ timer autosave ~10 san)
    E->>R: PUT /contacts (3 əlaqə telefonu, verified)
    E->>R: POST /mygov-employment-request (MyGov icazə linki)
    Note over C: Müştəri MyGov app-də icazə verir
    E->>R: POST /mygov-employment-verify
    R->>AD: GetEmployeeInfoByPin (MLSA məlumatı)
    AD-->>R: iş yeri, müqavilə, staj
    Note over E: Staj yoxlaması: EMPLOYMENT_TENURE_MIN_MONTHS (default 6, prod 1)

    E->>R: PUT /api/expert/ID/approve
    R->>AK: PUT /partner — re-register (homeAddress = faktiki ünvan) PR 357
    AK-->>R: eyni partner_id
    R->>AK: POST /application/create PR 358 — homeAddress göndərilmir
    Note over R: amount = total, interestRate 48 göndərilir 0.48, fee 11 göndərilir 0.11
    AK-->>R: lw_application_id
    alt Create və ya re-register xətası
        R-->>E: REJECTED — rollback (PR 283)
    end
    R-->>E: status = pending_signature
    end

    rect rgb(228, 245, 234)
    Note over W,R: 3. FON WORKER — sign poll

    loop TICK — hər 300 saniyə (5 dəq)
        W->>R: pending_signature siyahısı + stuck disbursing sweep
        W->>AK: GET /application/ID/status
        AK-->>W: loanId, loanStatus, signed
        alt signed = true
            W->>R: atomik claim: pending_signature keçir disbursing-ə
            W->>AK: POST /application/disburse (sistemdən maksimum 1 dəfə)
            alt Uğur
                W->>R: status = disbursed
                W->>SMS: Təsdiq SMS (məbləğ + köçürülən)
                W->>R: Referal kodu ALPUL-XXXXXX generasiya (PR 319)
                W->>SMS: Referal SMS (10% endirim)
            else Xəta
                W->>R: disburse_failed (auto-retry YOXDUR, manual)
            end
        else signed = false və 3 saat bitibsə
            W->>R: REJECTED (imza vaxtı bitdi)
        end
    end
    end
```

## Status keçidləri

```mermaid
stateDiagram-v2
    [*] --> pending_customer : init
    pending_customer --> rejected : early cutoff / KYC vaxtı bitdi
    pending_customer --> pending_expert : customer-confirm
    pending_expert --> rejected : ekspert reject
    pending_expert --> pending_signature : approve + AZMK create uğuru
    pending_signature --> rejected : imza 3 saat bitdi (worker)
    pending_signature --> disbursing : signed = true, atomik claim
    disbursing --> disbursed : disburse uğurlu
    disbursing --> disburse_failed : xəta / crash (manual review)
    disbursed --> [*]
```

## Xarici inteqrasiyalar

| Servis | Base URL | Əməliyyatlar | Axında yeri | Timeout |
|---|---|---|---|---|
| AZMK OnlineLending | web.azmk.az:7077/.../OnlineLendingService | POST /kyc, GET /kyc/ID, PUT /partner, GET /card/ID, POST /card, POST /application/create, GET /application/ID/status, POST /application/disburse | init/verify, kart, approve, worker | 30 s |
| AZMK CustomerData | web.azmk.az:7077/.../CustomerDataService | GetPersonalInfo, getMkrScore, getOwnerData, inquireByIdCard, GetEmployeeInfoByPin | early cutoff, offer, staj yoxlaması | 30 s |
| Softline SMS | gw.soft-line.az/sendsms | GET sendsms | OTP, təsdiq, referal SMS | — |
| Video servisi | videodemo.kvadrat-lab.com | POST /api/orders, POST /api/orders/status | apply video mərhələsi | — |
| MyGov | konfiqurasiya edilən provider | icazə linki (deeplink), məlumat çəkmə | employment / pension | — |
| LW | LW_BASE_URL (məs. localhost:8080) | personal-info, akb-score, akb-history, asan-finance, sima, blacklist (/api/router/* proxy) | ekspert alətləri, AKB override | 30 s |
| SIMA | mock (dev) | /api/router/sima/init + callback | dev/test | — |
| SQL Server | DB_HOST | əsas DB (loan_applications, cutoff_results, ...) | hər yer | — |

## Vaxt parametrləri

| Parametr | Mənbə (env / kod) | Default | Prod (loqdan) | Təsvir |
|---|---|---|---|---|
| OTP etibarlılığı | OTPCodeTTL (kod) | 300 s | 300 s | OTP kodu 5 dəq çür olur |
| OTP limiti | OTP_RATE_LIMIT_PER_MIN | 1/dəq | 1 | SMS bombaya qarşı |
| API limiti | RATE_LIMIT_PER_MINUTE | 60/dəq | 60 | ümumi API |
| Endirim limiti | DISCOUNT_RATE_PER_MIN | 5/dəq | 5 | discount validate |
| KYC poll | kod (init.go) | 3 s × 60 | — | init/verify içində, maks 3 dəq gözləmə |
| Video status poll | frontend (apply.html, PR 188) | 2 s | — | recorded = true olana qədər |
| Sign worker tick | AZMK_SIGN_POLL_INTERVAL_S | 300 s | 300 s | hər 5 dəq status yoxlanılır |
| İmza timeout | AZMK_SIGN_TIMEOUT_S | 10800 s | 10800 s | 3 saat bitəndə rejected |
| AZMK HTTP | AZMK_TIMEOUT_S | 30 s | 30 s | bütün AZMK çağırışları |
| LW HTTP | LW_TIMEOUT_S | 30 s | 30 s | |
| SMS provider keşi | kod (DB-driven) | 60 s | 1m0s | provider DB-dən 1 dəq keşlənir |
| Auth sessiya | AUTH_SESSION_TTL_HOURS | 8 saat | 8 | ekspert sessiyası |
| Minimum staj | EMPLOYMENT_TENURE_MIN_MONTHS | 6 ay | 1 ay | MLSA staj yoxlaması |
| Timer autosave | frontend (detail.html) | ~10 s | — | ekspert detail səhifəsi |

## AZMK request formuları (əsas)

- **Partner (init):** `PUT /partner` — homeAddress = `-` (müştəri hələ ünvan daxil etməyib), passport = seriya, mobile = +994 siz
- **Partner re-register (approve, PR 357):** homeAddress = dashboard-dakı faktiki ünvan; eyni PIN üçün eyni partner_id qayıdır
- **Application create (PR 358):** `POST /application/create` — homeAddress göndərilmir; amount = total (principal + komissiya); interestRate = illik faiz / 100 (48 → 0.48); disbursementFee = komissiya / 100 (11 → 0.11); cardId = seçilmiş kart
- **Disburse (PR 323):** applicationId + cardId + eyni LoanData; sistemdən maksimum 1 dəfə göndərilir (claim protokolu)
- **SMS (PR 351):** "Sizin %.2f AZN məbləğində kreditiniz təsdiq edildi, kartınıza %.2f AZN köçürüldü." — total + köçürülən principal
- **Referal (PR 319):** disburse uğurundan sonra ALPUL-XXXXXX kodu (10%) + SMS
