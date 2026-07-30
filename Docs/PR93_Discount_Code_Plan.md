# PR #93 — Endirim Kodu (Discount / Referral Code) Funksionalı

## 1. Biznes Məntiqi (Business Logic)

### Referral flow:
```
┌──────────────────────────────────────────────────────────────────┐
│ 1. Müştəri A kredit müraciəti edir                              │
│ 2. Ekspert təsdiq edir (UpdateStatus → approved)                │
│ 3. Sistem A üçün unikal endirim kodu generasiya edir            │
│ 4. A-ya SMS göndərilir:                                         │
│    "Kreditiniz təsdiq edildi! Endirim kodunuz: ALPUL-AB12CD"    │
│    "Bu kodu növbəti kredit götürənlə paylaşın — komissiya       │
│    endirimi yararlan."                                           │
├──────────────────────────────────────────────────────────────────┤
│ 5. Müştəri B (fərqli PIN) kredit müraciəti edir                 │
│ 6. apply.html-də "Təsdiq edirəm" düyməsindən ƏVVƏL              │
│    endirim kodu xanası var (opsiyonel)                          │
│ 7. B, A-nın kodunu daxil edir → customer-confirm endpoint-inə   │
│    göndərilir                                                    │
│ 8. Sistem kodu validate edir:                                   │
│    - Mövcuddur?                                                  │
│    - Başqa müştəriyə aiddir? (A ≠ B, PIN-lə müqayisə)           │
│    - İstifadə olunmamış? (status='active')                      │
│ 9. Əgər valid → kod application-ə yapışdırılır (status hələ     │
│    dəyişmir — yalnız approve zamanı)                            │
├──────────────────────────────────────────────────────────────────┤
│ 10. Ekspert B-nin kreditini təsdiq edir                         │
│ 11. Sistem endirimi tətbiq edir:                                │
│     - commission_amount hesablanır                              │
│     - discount_amount çıxılır (percent və ya fixed)             │
│     - total_amount = principal + (commission - discount)        │
│ 12. Kod 'used' kimi qeyd olunur (used_by_application_id = B)    │
│ 13. B-yə də öz kodu generasiya olunur + SMS                     │
└──────────────────────────────────────────────────────────────────┘
```

### Qaydalar:
- ✅ A öz kodunu öz növbəti kreditində istifadə edə bilməz (self-use prevention)
- ✅ Kod yalnız bir dəfə istifadə oluna bilər (single-use)
- ✅ Endirim komissiya məbləğindən çox ola bilməz (no negative commission)
- ✅ Endirim növü və məbləği **xaricdən idarə olunur** (DB-də `discount_type` + `discount_value`)
- ✅ Kod formatı: `ALPUL-XXXXXX` (6 uppercase alphanumeric)

---

## 2. Database (Migration 023_discount_codes.sql)

### 2.1 Yeni cədvəl: `discount_codes`

```sql
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'discount_codes')
BEGIN
    CREATE TABLE discount_codes (
        id                          INT IDENTITY(1,1) PRIMARY KEY,
        code                        VARCHAR(20)  NOT NULL UNIQUE,        -- ALPUL-AB12CD
        issued_to_customer_id       INT          NOT NULL,               -- FK customers.id (sahib)
        issued_from_application_id  INT          NOT NULL,               -- FK loan_applications.id (hansı approve generasiya etdi)
        discount_type               VARCHAR(10)  NOT NULL DEFAULT 'percent', -- 'percent' | 'fixed'
        discount_value              DECIMAL(10,2) NOT NULL DEFAULT 2.00, -- percent: 2.00 = 2%; fixed: 5.00 = 5 AZN
        status                      VARCHAR(15)  NOT NULL DEFAULT 'active', -- 'active' | 'used' | 'expired'
        used_by_application_id      INT          NULL,                   -- hansı application istifadə etdi
        used_at                     DATETIME     NULL,
        valid_until                 DATETIME     NULL,                   -- opsiyonel expiry
        created_at                  DATETIME     NOT NULL DEFAULT GETDATE(),

        CONSTRAINT FK_discount_codes_customer   FOREIGN KEY (issued_to_customer_id)      REFERENCES customers(id),
        CONSTRAINT FK_discount_codes_app_issued FOREIGN KEY (issued_from_application_id) REFERENCES loan_applications(id),
        CONSTRAINT FK_discount_codes_app_used   FOREIGN KEY (used_by_application_id)     REFERENCES loan_applications(id),

        CONSTRAINT CK_discount_type  CHECK (discount_type IN ('percent', 'fixed')),
        CONSTRAINT CK_discount_status CHECK (status IN ('active', 'used', 'expired'))
    );

    CREATE INDEX IX_discount_codes_code   ON discount_codes(code);
    CREATE INDEX IX_discount_codes_owner  ON discount_codes(issued_to_customer_id);
    CREATE INDEX IX_discount_codes_status ON discount_codes(status);
END
GO
```

### 2.2 `loan_applications` cədvəlinə sütunlar

```sql
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'discount_code')
BEGIN
    ALTER TABLE loan_applications ADD discount_code VARCHAR(20) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'discount_amount')
BEGIN
    ALTER TABLE loan_applications ADD discount_amount DECIMAL(10,2) NULL;
END
GO
```

### 2.3 Default konfiqurasiya (external control)

Endirim dəyəri **hardcoded deyil** — `discount_codes` cədvəlində hər kod üçün ayrı yazılır. Bu, admin panelindən və ya DB-dən dəyişdirilə bilər. Default: `percent=2.00` (komissiyanın 2%-i).

Gələcəkdə `discount_code_templates` cədvəli əlavə oluna bilər (müxtəlif kampaniyalar üçün fərqli dəyərlər).

### 2.4 `migration.go::dropAllTables`-a əlavə

```go
// internal/migration/migration.go
tables := []string{
    "application_checks",
    "credit_level_history",
    // ... mövcud ...
    "business_cutoffs",
    "discount_codes",  // PR #93 — əlavə et
}
```

---

## 3. Model Layer

### 3.1 Yeni fayl: `internal/model/discount_code.go`

```go
package model

import "time"

type DiscountCode struct {
    ID                       int        `json:"id"`
    Code                     string     `json:"code"`
    IssuedToCustomerID       int        `json:"issued_to_customer_id"`
    IssuedFromApplicationID  int        `json:"issued_from_application_id"`
    DiscountType             string     `json:"discount_type"`  // 'percent' | 'fixed'
    DiscountValue            float64    `json:"discount_value"`
    Status                   string     `json:"status"`          // 'active' | 'used' | 'expired'
    UsedByApplicationID      *int       `json:"used_by_application_id,omitempty"`
    UsedAt                   *time.Time `json:"used_at,omitempty"`
    ValidUntil               *time.Time `json:"valid_until,omitempty"`
    CreatedAt                time.Time  `json:"created_at"`
}

const (
    DiscountTypePercent = "percent"
    DiscountTypeFixed   = "fixed"

    DiscountStatusActive  = "active"
    DiscountStatusUsed    = "used"
    DiscountStatusExpired = "expired"
)
```

### 3.2 `internal/model/loan_application.go`-a əlavə

```go
type LoanApplication struct {
    // ... mövcud field-lər ...
    DiscountCode   string   `json:"discount_code,omitempty"`    // PR #93: daxil edilmiş kod
    DiscountAmount *float64 `json:"discount_amount,omitempty"`  // PR #93: tətbiq olunmuş endirim məbləği
}
```

---

## 4. Repository Layer

### 4.1 Yeni fayl: `internal/repository/discount_code_repo.go`

```go
package repository

type DiscountCodeRepo struct {
    db *sql.DB
}

func NewDiscountCodeRepo(db *sql.DB) *DiscountCodeRepo { return &DiscountCodeRepo{db: db} }

// Create — yeni kod əlavə et (approval zamanı)
func (r *DiscountCodeRepo) Create(ctx context.Context, c *model.DiscountCode) error

// GetByCode — kod string ilə gətir (validation zamanı)
func (r *DiscountCodeRepo) GetByCode(ctx context.Context, code string) (*model.DiscountCode, error)

// MarkUsed — kodu istifadə olunmuş kimi qeyd et (approval transaction içində)
func (r *DiscountCodeRepo) MarkUsed(ctx context.Context, codeID, applicationID int) error

// GetByOwnerCustomerID — müştərinin kodlarını gətir (future: "my codes" dashboard)
func (r *DiscountCodeRepo) GetByOwnerCustomerID(ctx context.Context, customerID int) ([]*model.DiscountCode, error)

// ExistsByCode — collision check (generasiya zamanı)
func (r *DiscountCodeRepo) ExistsByCode(ctx context.Context, code string) (bool, error)
```

### 4.2 `internal/repository/application_repo.go`-a əlavə

- `UpdateApplicationDecision`-a `discount_code`, `discount_amount` parameter-ləri əlavə et
- `UpdateCustomerConfirm`-a `discount_code` parameter əlavə et
- Bütün SELECT sorğularına `discount_code`, `discount_amount` sütunları əlavə et (`application_details_repo.go`, `application_list_repo.go`)

### 4.3 `internal/service/interfaces.go`-a əlavə

```go
type DiscountCodeStore interface {
    Create(ctx context.Context, c *model.DiscountCode) error
    GetByCode(ctx context.Context, code string) (*model.DiscountCode, error)
    MarkUsed(ctx context.Context, codeID, applicationID int) error
    ExistsByCode(ctx context.Context, code string) (bool, error)
}
```

---

## 5. Service Layer

### 5.1 Yeni: `internal/service/discount_code_service.go`

```go
package service

type DiscountCodeService struct {
    repo       DiscountCodeStore
    customerRepo CustomerStore
}

// GenerateForApplication — approval zamanı çağrılır
// Format: ALPUL-XXXXXX (6 uppercase alphanumeric)
// Collision yoxlanılır, lazımsa regenerate
func (s *DiscountCodeService) GenerateForApplication(ctx context.Context, appID, customerID int) (*model.DiscountCode, error)

// ValidateForCustomer — customer-confirm zamanı çağrılır
// Yoxlayır:
//   1. Kod mövcuddur
//   2. status = 'active'
//   3. issued_to_customer_id ≠ currentCustomerID (self-use prevention)
//   4. (opsiyonel) valid_until keçməyib
func (s *DiscountCodeService) ValidateForCustomer(ctx context.Context, code string, currentCustomerID int) (*model.DiscountCode, error)

// CalculateDiscount — approval zamanı komissiya məbləğindən endirim hesablayır
// percent: discount_amount = commission_amount × (value / 100)
// fixed:   discount_amount = min(value, commission_amount)
func (s *DiscountCodeService) CalculateDiscount(code *model.DiscountCode, commissionAmount float64) float64
```

**Kod generasiya alqoritmi:**
```go
const codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 0, O, I, 1 çıxarıldı (vizual qarışıqlıq yox)

func generateCode() string {
    b := make([]byte, 6)
    for i := range b {
        b[i] = codeCharset[rand.Intn(len(codeCharset))]
    }
    return "ALPUL-" + string(b)
}
```

### 5.2 Modify: `internal/service/application_service_customer_confirm.go`

```go
type CustomerConfirmRequest struct {
    Amount                  float64 `json:"amount"`
    CardNumber              string  `json:"card_number"`
    ActualAddress           string  `json:"actual_address"`
    CardOwnershipConfirmed  bool    `json:"card_ownership_confirmed"`
    RulesConfirmed          bool    `json:"rules_confirmed"`           // PR #92
    DiscountCode            string  `json:"discount_code,omitempty"`  // PR #93 — NEW
}
```

`CustomerConfirmApplication`-da əlavə:
```go
if req.DiscountCode != "" {
    code := strings.ToUpper(strings.TrimSpace(req.DiscountCode))
    // 1. Müştərini tap (PIN ilə)
    customer, err := s.customerRepo.GetByPIN(ctx, app.CustomerPIN)
    // 2. Kodu validate et
    discountCode, err := s.discountSvc.ValidateForCustomer(ctx, code, customer.ID)
    if err != nil {
        return fmt.Errorf("yanlis endirim kodu: %w", err)
    }
    // 3. Application-a yaz (status hələ dəyişmir)
    app.DiscountCode = code
}
```

### 5.3 Modify: `internal/service/application_service_status.go::UpdateStatus`

`StatusApproved` branch-ında, mövcud `calculateTotalAmount` çağırışını əvəz et:

```go
if req.Status == model.StatusApproved {
    // 1. Kodu yenidən validate et (race condition qoruma)
    var discountAmount float64
    if app.DiscountCode != "" {
        customer, _ := s.customerRepo.GetByPIN(ctx, app.CustomerPIN)
        code, err := s.discountSvc.ValidateForCustomer(ctx, app.DiscountCode, customer.ID)
        if err == nil {
            // 2. Komissiya məbləğini hesabla
            commissionAmount := calculateCommissionAmount(app.Amount, app.ApprovedRate)
            // 3. Endirimi hesabla
            discountAmount = s.discountSvc.CalculateDiscount(code, commissionAmount)
            // 4. Kodu 'used' kimi qeyd et (transaction içində)
            //    Bu, MarkUsed + UpdateApplicationDecision atomik olmalıdır
        }
    }
    // 5. Total amount-u endirimlə hesabla
    totalAmount = calculateTotalAmountWithDiscount(app.Amount, app.ApprovedRate, discountAmount)

    // 6. A üçün yeni endirim kodu generasiya et
    customer, _ := s.customerRepo.GetByPIN(ctx, app.CustomerPIN)
    newCode, _ := s.discountSvc.GenerateForApplication(ctx, app.ID, customer.ID)

    // 7. SMS göndər
    if newCode != nil {
        smsMsg := fmt.Sprintf("Hormetli musteri, sizin kreditiniz tesdiq edildi! Endirim kodunuz: %s. Bu kodu novbeti kredit goturenle paylasin — komisiya endirimi yararlan.", newCode.Code)
        s.smsProvider.Send(ctx, app.CustomerPhone, smsMsg)
    }
}
```

### 5.4 Modify: `internal/service/credit_decision.go`

```go
// calculateTotalAmountWithDiscount — endirim nəzərə alaraq total hesabla
func calculateTotalAmountWithDiscount(principal, commission, discountAmount float64) float64 {
    if commission <= 0 || commission >= 100 {
        return principal
    }
    commissionAmount := (commission / (100 - commission)) * 100
    discounted := commissionAmount - discountAmount
    if discounted < 0 {
        discounted = 0 // endirim komissiyadan çox ola bilməz
    }
    total := principal + discounted
    return math.Round(total*100) / 100
}

// calculateCommissionAmount — yalnız komissiya hissəsi (endirim hesablamaq üçün)
func calculateCommissionAmount(principal, commission float64) float64 {
    if commission <= 0 || commission >= 100 {
        return 0
    }
    return (commission / (100 - commission)) * 100
}
```

---

## 6. Transaction Management (Atomiklik)

Approve zamanı aşağıdakı əməliyyatlar **bir transaction içində** olmalıdır:

```go
tx, _ := s.db.BeginTx(ctx, nil)
defer tx.Rollback()

// 1. UpdateApplicationDecision (status, total_amount, discount_amount)
// 2. MarkUsed (discount_codes.status = 'used', used_by_application_id)
// 3. Create (yeni discount_code A üçün)
// 4. SaveCreditLevelHistory (mövcud)

tx.Commit()
```

**Səbəb:** Əgər approval uğursuz olarsa amma kod 'used' kimi qeyd olunarsa — müştəri itirir. Əgər approval uğurlu amma kod 'used' deyilsə — növbəti müştəri eyni kodu istifadə edə bilər (double-dip).

Mövcud `repository/tx.go` transaction helper-i istifadə olunsun.

---

## 7. Frontend (apply.html)

### 7.1 Endirim kodu xanası (Təsdiq düyməsindən əVVƏL)

```html
<!-- PR #93: Endirim kodu (opsiyonel) -->
<div>
    <label class="text-sm text-gray-400 mb-2 block">
        Endirim kodu <span class="text-gray-500">(opsiyonel)</span>
    </label>
    <div class="relative">
        <input type="text"
               x-model="form.discountCode"
               @input="form.discountCode = form.discountCode.toUpperCase()"
               placeholder="ALPUL-XXXXXX"
               maxlength="13"
               class="input-field w-full px-4 py-3.5 rounded-xl uppercase tracking-wider font-mono">
        <!-- Real-time validation indicator -->
        <div x-show="form.discountCode.length >= 13" class="absolute right-3 top-1/2 -translate-y-1/2">
            <svg x-show="discountValid === 'valid'" class="w-5 h-5 text-emerald-400" .../>
            <svg x-show="discountValid === 'invalid'" class="w-5 h-5 text-rose-400" .../>
            <svg x-show="discountValid === 'checking'" class="w-5 h-5 animate-spin text-gray-400" .../>
        </div>
    </div>
    <p class="text-xs text-gray-500 mt-1">
        Endirim kodunuz varsa daxil edin. Komissiya endirimi tətbiq olunacaq.
    </p>
    <p x-show="discountValid === 'invalid'" class="text-xs text-rose-400 mt-1">
        Yanlış və ya istifadə olunmuş kod.
    </p>
</div>
```

### 7.2 Form state

```js
form: {
    // ... existing ...
    discountCode: '',     // PR #93
    discountValid: '',    // 'valid' | 'invalid' | 'checking' | ''
}
```

### 7.3 Payload (confirmApplication)

```js
body: JSON.stringify({
    amount: this.form.amount,
    card_number: this.form.cardNumber.replace(/\s/g, ''),
    actual_address: this.form.address,
    card_ownership_confirmed: this.form.cardConfirmed,
    rules_confirmed: this.form.rulesConfirmed,
    discount_code: this.form.discountCode.trim().toUpperCase(), // PR #93
}),
```

### 7.4 Real-time validation (opsiyonel, debounced)

```js
validateDiscountCode: _.debounce(async function() {
    if (this.form.discountCode.length < 13) {
        this.discountValid = '';
        return;
    }
    this.discountValid = 'checking';
    try {
        const resp = await fetch('/api/discount-codes/validate?code=' + encodeURIComponent(this.form.discountCode));
        this.discountValid = resp.ok ? 'valid' : 'invalid';
    } catch {
        this.discountValid = '';
    }
}, 500)
```

**Qeyd:** Real-time validation **self-use check etmir** (çünki frontend müştərinin ID-sini bilmir). Yalnız format + mövcudluq yoxlanır. Self-use check backend-də customer-confirm zamanı edilir.

---

## 8. API Endpoints (opsiyonel)

### 8.1 Real-time validation

```
GET /api/discount-codes/validate?code=ALPUL-XXXXXX
```

Response (200 OK):
```json
{
    "valid": true,
    "discount_type": "percent",
    "discount_value": 2.00,
    "preview_text": "Komissiyadan 2% endirim"
}
```

Response (404):
```json
{
    "valid": false,
    "reason": "not_found" | "already_used" | "expired"
}
```

### 8.2 Admin (future)

```
GET  /api/expert/discount-codes          — bütün kodlar
POST /api/expert/discount-codes          — manual kod yaradılması
PUT  /api/expert/discount-codes/{id}     — dəyəri dəyiş
```

---

## 9. Edge Cases və Validation Rules

| # | Ssenari | Davranış |
|---|---------|----------|
| 1 | A öz kodunu öz növbəti kreditində daxil edir | Backend `customer-confirm`-da reject: "Siz oz endirim kodunuzdan istifade ede bilmezsiniz" |
| 2 | Eyni kod iki dəfə istifadə olunur | İkinci `customer-confirm` reject: "Bu kod artiq istifade olunub" |
| 3 | Mövcud olmayan kod | Reject: "Yanlis endirim kodu" |
| 4 | Kod formatı yanlış (məs. `alpul-abc`) | Frontend uppercase çevirir; backend case-insensitive axtarış |
| 5 | Endirim komissiyadan çoxdur | `discounted = max(0, commission - discount)` — komissiya 0-a enir, mənfi olmur |
| 6 | Müştəri kod daxil edib amma sonra approve olunmur | Kod application-da qalır amma status='active' olaraq qalır — başqa müştəri istifadə edə bilər |
| 7 | Eyni application iki dəfı customer-confirm çağırır | İkinci dəfə eyni kod yenidən validate olunur — OK (idempotent) |
| 8 | Approve zamanı kod artıq başqası tərəfindən istifadə olunub | Race condition: approve transaction-ında re-validate et, reject: "Kod artiq istifade olunub" |
| 9 | SMS provider xəta verir | Approval uğurlu sayılır, kod DB-də qalır; log warning. Manual retry oluna bilər. |
| 10 | `discount_value = 0` | Endirim 0 — kod informational məqsədli (debug/test) |

---

## 10. SMS Template

```
Hormetli musteri, sizin kreditiniz tesdiq edildi!
Məbləğ: {amount} AZN, Müddət: {term} ay.
Endirim kodunuz: {code}
Bu kodu novbeti kredit goturenle paylasin — komisiya endirimi yararlan.
```

**Qeyd:** SMS uzunluğu ~160 simvol. Azərbaycan dilində, latın hərfləri.

---

## 11. Implementation Order (Subtask-lar)

| # | Task | Fayl | Prioritet |
|---|------|------|-----------|
| 1 | Migration 023 | `migrations/023_discount_codes.sql` | Yüksək |
| 2 | Model: DiscountCode | `internal/model/discount_code.go` | Yüksək |
| 3 | Model: LoanApplication genişləndir | `internal/model/loan_application.go` | Yüksək |
| 4 | Repository: DiscountCodeRepo | `internal/repository/discount_code_repo.go` | Yüksək |
| 5 | Repository: ApplicationRepo UPDATE/SELECT | `internal/repository/application_repo.go` | Yüksək |
| 6 | Service: DiscountCodeService | `internal/service/discount_code_service.go` | Yüksək |
| 7 | Service: CustomerConfirm validation | `internal/service/application_service_customer_confirm.go` | Yüksək |
| 8 | Service: UpdateStatus approval + SMS | `internal/service/application_service_status.go` | Yüksək |
| 9 | Service: discount calculation | `internal/service/credit_decision.go` | Yüksək |
| 10 | Migration: dropAllTables əlavə | `internal/migration/migration.go` | Orta |
| 11 | Frontend: input field | `web/apply.html` | Yüksək |
| 12 | Frontend: payload | `web/apply.html` | Yüksək |
| 13 | (Opsiyonel) API: validate endpoint | `internal/handler/`, `router.go` | Aşağı |
| 14 | (Opsiyonel) Real-time validation | `web/apply.html` | Aşağı |
| 15 | Unit tests | `*_test.go` | Yüksək |
| 16 | Integration test (approve flow) | `internal/service/` | Orta |

---

## 12. Faylların Siyahısı

### Yaradılacaq (Created):
- `migrations/023_discount_codes.sql`
- `internal/model/discount_code.go`
- `internal/repository/discount_code_repo.go`
- `internal/service/discount_code_service.go`
- `internal/service/discount_code_service_test.go`

### Modifikasiya olunacaq (Modified):
- `internal/model/loan_application.go` — `DiscountCode`, `DiscountAmount` field-ləri
- `internal/repository/application_repo.go` — UPDATE/SELECT sorğuları
- `internal/repository/application_details_repo.go` — SELECT
- `internal/repository/application_list_repo.go` — SELECT
- `internal/service/interfaces.go` — `DiscountCodeStore` interface
- `internal/service/application_service_customer_confirm.go` — validation
- `internal/service/application_service_status.go` — approval + SMS + transaction
- `internal/service/credit_decision.go` — discount-aware calculation
- `internal/migration/migration.go` — dropAllTables-ə əlavə
- `internal/handler/router.go` — (opsiyonel) validate endpoint
- `web/apply.html` — input field + payload

---

## 13. Test Planı

### Unit testlər:
- [ ] `GenerateForApplication` — unikal kod generasiya edir, collision yoxlanır
- [ ] `ValidateForCustomer` — self-use reject edir
- [ ] `ValidateForCustomer` — istifadə olunmuş kodu reject edir
- [ ] `ValidateForCustomer` — mövcud olmayan kodu reject edir
- [ ] `CalculateDiscount` — percent düzgün hesablanır
- [ ] `CalculateDiscount` — fixed düzgün hesablanır
- [ ] `CalculateDiscount` — endirim komissiyadan çox olanda 0-a clamp olunur
- [ ] `calculateTotalAmountWithDiscount` — düzgün total

### Integration testlər:
- [ ] Tam flow: A approve → SMS + kod generasiya → B kod daxil edir → B approve → endirim tətbiq → kod 'used'
- [ ] Self-use: A kod daxil edir → reject
- [ ] Double-use: B kod daxil edir → B reject → C eyni kod daxil edir → approve (kod hələ active)

### Manual test (apply.html):
- [ ] Endirim kodu xanası görünür (opsiyonel)
- [ ] Uppercase avtomatik çevrilir
- [ ] Valid kod → yeşil check
- [ ] Yanlış kod → qırmızı X
- [ ] Boş saxlanıla bilər
- [ ] Təsdiq düyməsi aktiv (kod boş olsa belə)

---

## 14. Risklər və Mitigasiya

| Risk | Ehtimal | Təsir | Mitigasiya |
|------|---------|-------|------------|
| SMS provider xəta | Orta | Aşağı | Approval uğurlu sayılır, kod DB-də qalır; log + manual retry |
| Race condition (iki müştəri eyni kod) | Aşağı | Yüksək | Transaction + re-validate approve zamanı |
| Kod bruteforce | Aşağı | Orta | 6 chars × 32 charset = 1B kombinasiya; rate-limit əlavə oluna bilər |
| `discount_value` 0 və ya mənfi | Orta | Aşağı | DB CHECK constraint; service-də `if value <= 0 return 0` |
| Migration mövcud application-ları pozar | Aşağı | Yüksək | Yeni sütunlar NULLABLE; köhnə data təsirsiz |
| `customers` cədvəli dropAllTables-da yoxdur | — | — | Bu PR-da düzəltmək opsiyonel — ayrı issue |

---

## 15. Gələcək Genişlənmələr (Out of Scope for PR #93)

- 🔮 **Campaigns:** Müxtəlif kampaniyalar üçün fərqli discount_value (Bayram, Yeni İl)
- 🔮 **Multi-use codes:** Eyni kod N dəfə istifadə oluna bilsin
- 🔮 **Time-limited codes:** `valid_until` field-i aktivləşdirilsin
- 🔮 **Admin dashboard:** Expert panel-də kodları görüb idarə etmək
- 🔮 **Customer dashboard:** Müştəri öz kodunu və istifadə statistikasını görə bilsin
- 🔮 **Tiered rewards:** A kodu B-yə verir, B kredit götürəndə A-ya da bonus (loyalty)
- 🔮 **Analytics:** Referral conversion rate, top referrers
