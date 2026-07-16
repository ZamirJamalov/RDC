# Credit Level Testing Guide — Postman ile addim-addim

Bu guide kredit seviyyeleri arasi kecidleri Postman uzre test etmek ucun yazilib.
Hər addimi sirayla edin. Hər seviyye ucun ayri test var.

---

## Umumi Melumat

### Seviyyeler ve kecid qaydalari

| Keçid | Kredit sayi | Maks gecikme | Min müddet |
|---|---:|---:|---:|
| Yeni → Etibarli | 2 | 2 gun | 3 ay |
| Etibarli → Dəyərli | 2 | 3 gun | 3 ay |
| Dəyərli → Elit | 2 | 4 gun | 2 ay |

### Onemli qaydalar

1. Hər kredit `level_at_close` sahəsinə malik olmalidir — bu gosterir ki kredit hansi seviyyede baglanib
2. `delay_days` — gecikme gun sayi (0 = vaxtinda odenilib)
3. `term_months` — kredit muddeti ay ile
4. Keçid yalniz bir seviyye yuxari ola biler (Yeni → Etibarli → Dəyərli → Elit)
5. Gecikme limiti asilarsa → bir seviyye asagi dusur
6. Muddet qisa olarsa → yukselmir, eyni seviyyede qalir

---

## Test 1: Yeni Musteri (0 kredit)

**Meqsed:** Hech bir krediti olmayan musteri "new" seviyyesinde olmalidir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🟢 Setup: Yeni müştəri (0 kredit)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "new_customer",
  "loans": []
}
```

Send edin → Status 200 olmalidir.

### Step 2: Offer sorğula

Postman folder: **3. Offer + Application**

Request: `🟢 Offer: Yeni müştəri`

URL: `GET /api/applications/offer?customer_pin=TEST001&akb_score=400`

Send edin.

### Expected result:

```json
{
  "customer_pin": "TEST001",
  "credit_level": "new",
  "unlock_phase": 1,
  "akb_score": 400,
  "ranges": [
    {"min_amount": 100, "max_amount": 300, "term_months": 3, "rate": 30.0, "phase": 1},
    {"min_amount": 301, "max_amount": 500, "term_months": 3, "rate": 30.0, "phase": 1}
  ]
}
```

**Tesdiq:** `credit_level` = `new` ✅

---

## Test 2: Yeni → Etibarli (Promotion)

**Meqsed:** "new" seviyyesinde 2 kredit tamamlayan musteri "trusted" seviyyesine kecmelidir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🟢 Setup: Trusted (2 loans at new, 0 delay, 3mo → trusted)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "trusted",
  "loans": [
    {
      "lms_loan_id": "LMS-001",
      "loan_type": "micro",
      "amount": 300,
      "term_months": 3,
      "start_date": "2025-01-01",
      "end_date": "2025-04-01",
      "status": "completed",
      "remaining_amount": 0,
      "was_on_time": true,
      "early_completion": false,
      "delay_days": 0,
      "level_at_close": "new",
      "closed_at": "2025-04-01"
    },
    {
      "lms_loan_id": "LMS-002",
      "loan_type": "micro",
      "amount": 300,
      "term_months": 3,
      "start_date": "2025-05-01",
      "end_date": "2025-08-01",
      "status": "completed",
      "remaining_amount": 0,
      "was_on_time": true,
      "early_completion": false,
      "delay_days": 0,
      "level_at_close": "new",
      "closed_at": "2025-08-01"
    }
  ]
}
```

Send edin → Status 200 olmalidir.

### Step 2: Offer sorğula

Request: `🟢 Offer: Yeni müştəri`

Send edin.

### Expected result:

```json
{
  "credit_level": "trusted"
}
```

**Tesdiq:** `credit_level` = `trusted` ✅

### Step 3: Application yarat (optional)

Request: `🟢 Application: Happy path (yeni → pending_approval)`

Body-de `customer_pin` = `TEST001` ve `amount` = 200, `term_months` = 3 yazin.

Send edin → Status 201.

2-3 saniye gozleyin, sonra:

Request: `🟢 Get Status: Full status`

Send edin → `status` = `pending_approval` olmalidir (trusted seviyyesi manual tesdiq teleb edir).

---

## Test 3: Etibarli → Dəyərli (Promotion)

**Meqsed:** "trusted" seviyyesinde 2 kredit tamamlayan musteri "valuable" seviyyesine kecmelidir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🟢 Setup: Valuable (2 loans at trusted, 0 delay, 3mo → valuable)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "valuable",
  "loans": [
    {
      "lms_loan_id": "LMS-003",
      "amount": 500,
      "term_months": 3,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "trusted",
      "closed_at": "2025-04-01"
    },
    {
      "lms_loan_id": "LMS-004",
      "amount": 500,
      "term_months": 3,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "trusted",
      "closed_at": "2025-08-01"
    }
  ]
}
```

Send edin → Status 200.

### Step 2: Offer sorğula

Request: `🟢 Offer: Yeni müştəri`

Send edin.

### Expected result:

```json
{
  "credit_level": "valuable"
}
```

**Tesdiq:** `credit_level` = `valuable` ✅

---

## Test 4: Dəyərli → Elit (Promotion)

**Meqsed:** "valuable" seviyyesinde 2 kredit tamamlayan musteri "elite" seviyyesine kecmelidir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🟢 Setup: Elite (2 loans at valuable, 0 delay, 2mo → elite)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "elite",
  "loans": [
    {
      "lms_loan_id": "LMS-005",
      "amount": 700,
      "term_months": 2,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "valuable",
      "closed_at": "2025-03-01"
    },
    {
      "lms_loan_id": "LMS-006",
      "amount": 700,
      "term_months": 3,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "valuable",
      "closed_at": "2025-07-01"
    }
  ]
}
```

Send edin → Status 200.

### Step 2: Offer sorğula

Request: `🟢 Offer: Yeni müştəri`

Send edin.

### Expected result:

```json
{
  "credit_level": "elite"
}
```

**Tesdiq:** `credit_level` = `elite` ✅

### Step 3: Application yarat (auto-approve test)

Request: `🟢 Application: Happy path (elite → auto-approve)`

Body-de `customer_pin` = `TEST001`, `amount` = 500, `term_months` = 6 yazin.

Send edin → Status 201.

2-3 saniye gozleyin, sonra:

Request: `🟢 Get Status: Full status`

Send edin → `status` = `approved` olmalidir (elite seviyyesi auto-approve olur).

---

## Test 5: Gecikme Limiti Asimi (Downgrade)

**Meqsed:** "trusted" seviyyesinde 4 gun gecikme edib (limit 3 gun) → "new" seviyyesine dusmelidir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🔴 Setup: Gecikme aşımı (2 at trusted, 4 delay → downgrade to new)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "delay_downgrade",
  "loans": [
    {
      "lms_loan_id": "LMS-007",
      "amount": 500,
      "term_months": 3,
      "status": "completed",
      "was_on_time": false,
      "delay_days": 4,
      "level_at_close": "trusted",
      "closed_at": "2025-04-01"
    },
    {
      "lms_loan_id": "LMS-008",
      "amount": 500,
      "term_months": 3,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "trusted",
      "closed_at": "2025-08-01"
    }
  ]
}
```

Send edin → Status 200.

### Step 2: Offer sorğula

Request: `🟢 Offer: Yeni müştəri`

Send edin.

### Expected result:

```json
{
  "credit_level": "new"
}
```

**Tesdiq:** `credit_level` = `new` (downgrade olub) ✅

**Izah:** Musteri "trusted" seviyyesinde idie amma 4 gun gecikme edib (limit 3 gun). Ona gore "new" seviyyesine dusur. Növbəti kredit "new" seviyyesinin faiz ve limitleri daxilinde teklif olunur.

---

## Test 6: Qisa Muddet (No Promotion)

**Meqsed:** "new" seviyyesinde 2 kredit amma 2 ay muddetle (minimum 3 ay) → yukselmir, "new" qalir.

### Step 1: Setup mock data

Postman folder: **0. Setup (Mock Data)**

Request: `🔴 Setup: Qısa müddət (2 at new, 0 delay, 2mo → no promotion)`

Body:
```json
{
  "customer_pin": "TEST001",
  "scenario_name": "short_term",
  "loans": [
    {
      "lms_loan_id": "LMS-009",
      "amount": 300,
      "term_months": 2,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "new",
      "closed_at": "2025-03-01"
    },
    {
      "lms_loan_id": "LMS-010",
      "amount": 300,
      "term_months": 2,
      "status": "completed",
      "was_on_time": true,
      "delay_days": 0,
      "level_at_close": "new",
      "closed_at": "2025-06-01"
    }
  ]
}
```

Send edin → Status 200.

### Step 2: Offer sorğula

Request: `🟢 Offer: Yeni müştəri`

Send edin.

### Expected result:

```json
{
  "credit_level": "new"
}
```

**Tesdiq:** `credit_level` = `new` (yukselmeyib) ✅

**Izah:** 2 kredit tamamlayib amma hər biri yalniz 2 ay muddetle (minimum 3 ay telebi var). Ona gore "trusted" seviyyesine yukselmek huququ yoxdur.

---

## Test 7: AKB Override

**Meqsed:** AKB skoru 700+ olan musteri "valuable" seviyyesine kecmelidir (kredit tarixcesinden asili olmayaraq).

### Step 1: Setup mock data

Request: `🟢 Setup: Yeni müştəri (0 kredit)`

Send edin → 0 kredit.

### Step 2: Offer sorğula (AKB 750)

URL: `GET /api/applications/offer?customer_pin=TEST001&akb_score=750`

Send edin.

### Expected result:

```json
{
  "credit_level": "valuable",
  "akb_score": 750
}
```

**Tesdiq:** `credit_level` = `valuable` (AKB override) ✅

---

## Test 8: Aktiv Kredit (Reject)

**Meqsed:** Aktiv krediti olan musteri reject olunmalidir.

### Step 1: Setup mock data

Request: `🟢 Setup: Aktiv kreditli`

Send edin → 1 aktiv kredit.

### Step 2: Application yarat

Request: `🟢 Application: Happy path (yeni → pending_approval)`

Send edin → Status 201.

2-3 saniye gozleyin.

### Step 3: Status yoxla

Request: `🟢 Get Status: Full status`

Send edin.

### Expected result:

```json
{
  "status": "rejected",
  "decision": {
    "rejection_reason": "Customer has active loans"
  }
}
```

**Tesdiq:** `status` = `rejected` ✅

---

## Test 9: Gecikmeli Odenis (Reject)

**Meqsed:** 5 gun gecikme ile baglanmis krediti olan musteri reject olunmalidir.

### Step 1: Setup mock data

Request: `🟢 Setup: Gecikməli (1 at new, 5 delay → reject)`

Send edin → 1 kredit, 5 gun gecikme.

### Step 2: Application yarat

Request: `🟢 Application: Happy path`

Send edin → Status 201.

2-3 saniye gozleyin.

### Step 3: Status yoxla

Request: `🟢 Get Status: Full status`

Send edin.

### Expected result:

```json
{
  "status": "rejected",
  "decision": {
    "rejection_reason": "Late payments found in loan history"
  }
}
```

**Tesdiq:** `status` = `rejected` ✅

---

## Quick Reference Table

| Test | Setup | Expected Level | Expected Status |
|---|---|---|---|
| 1. Yeni musteri | 0 kredit | `new` | - |
| 2. Yeni → Etibarli | 2 at new, 0 delay, 3mo | `trusted` | pending_approval |
| 3. Etibarli → Dəyərli | 2 at trusted, 0 delay, 3mo | `valuable` | pending_approval |
| 4. Dəyərli → Elit | 2 at valuable, 0 delay, 2mo | `elite` | approved (auto) |
| 5. Gecikme asimi | 2 at trusted, 4 delay | `new` (downgrade) | - |
| 6. Qisa muddet | 2 at new, 0 delay, 2mo | `new` (no promotion) | - |
| 7. AKB override | 0 kredit, AKB 750 | `valuable` | - |
| 8. Aktiv kredit | 1 active | - | rejected |
| 9. Gecikmeli | 1 at new, 5 delay | - | rejected |

---

## Troubleshooting

### Problem: "credit_level" her zaman "new" qaytarir

**Sebəb:** `level_at_close` sahəsi bos veya sehv dəyər gonderilir.

**Hell:** Mock data-da `level_at_close` sahəsini yoxlayin. Məsələn, "trusted" seviyyesine kecmek üçün kreditler `level_at_close: "new"` olmalidir (çünki "new" seviyyesinde kredit goturub baglayib).

### Problem: "failed to scan loan row" xətasi

**Sebəb:** Mock data-da yeni sahəler (`delay_days`, `level_at_close`, `closed_at`) yoxdur.

**Hell:** Postman collection-un yenilenmiş versiyasini import edin.

### Problem: Application reject olunur amma seviyye dogru olmalidir

**Sebəb:** Aktiv kredit var və ya gecikme limiti asilib.

**Hell:** Once `GET /api/applications/offer` ile seviyyeni yoxlayin, sonra application yaradin.
