# RDC Quick Start — End-to-End Testing Guide

This guide shows you how to run the RDC system locally and test the full
flow from the customer's application (apply.html) to the expert's dashboard.

**Time needed:** ~10 minutes

---

## Step 0: Prerequisites

- Go 1.25+ installed
- SQL Server accessible (host, port, user, password)
- A `.env` file in the `source/` directory (or environment variables set in your shell)

---

## Step 1: Configure `.env`

Create or edit `source/.env` with these settings:

```bash
# --- Database (REQUIRED) ---
DB_HOST=172.17.1.24
DB_USER=rdc_test
DB_PASSWORD=your_password_here
DB_PORT=1433
DB_NAME=RDC

# --- Server ---
SERVER_ADDR=:8000

# --- Migrations ---
# true = drop and recreate tables on startup (use for clean test)
# false = keep existing data
MIGRATIONS_DROP_RECREATE=true

# --- LW Provider ---
# CRITICAL: set these for stub mode (LW router not yet available)
LW_USE_MOCK=false
LW_USE_STUB=true
LW_STUB_PORT=8090

# --- OTP Provider ---
# true = OTP codes appear in server log (no real SMS)
OTP_USE_MOCK=true

# --- Logging ---
LOG_LEVEL=info
```

### Why these settings?

| Setting | Value | Why |
|---|---|---|
| `LW_USE_MOCK=false` | Disable old mock provider | We want HTTP calls, not local DB mock |
| `LW_USE_STUB=true` | Enable in-process stub server | Stub simulates LW router responses |
| `OTP_USE_MOCK=true` | Log OTP instead of SMS | No real SMS gateway needed for testing |
| `MIGRATIONS_DROP_RECREATE=true` | Clean DB on startup | Ensures fresh state for testing |

---

## Step 2: Start the Server

```bash
cd source
go run .
```

### What you should see in the terminal:

```
INFO LW stub server starting (development only) addr=:8090
INFO using HTTP LW provider pointed at in-process stub base_url=http://localhost:8090
INFO connected to SQL Server
INFO migrations completed
INFO server listening addr=:8000
```

### Verify the stub is running:

Open another terminal and run:
```bash
curl http://localhost:8090/stub/health
```

Expected response:
```json
{"status":"ok","service":"lw-stub"}
```

If you see `connection refused`, the stub server didn't start — check your `.env` file.

---

## Step 3: Customer Applies (apply.html)

Open your browser and go to:

```
http://localhost:8000/apply.html
```

### 3.1 Fill in the form:

| Field | Value | Notes |
|---|---|---|
| FIN kodu | `TEST001` | Any 7-character code |
| Seriya nömrəsi | `AA1234567` | Any text |
| Telefon | `50 123 45 67` | 9 digits (without +994 prefix) |

### 3.2 Click "OTP Göndər"

- The form expands to show an OTP input field
- **Look at the server terminal** — you will see a log line like:
  ```
  INFO mock OTP code phone=+994501234567 code=123456
  ```
- Copy the 6-digit code from the log

### 3.3 Enter the OTP code and click "Müraciət Göndər"

- The page transitions to Step 2: "Müraciətinizə baxılır..."
- Behind the scenes, the backend fetches the credit offer from the stub
- After 1-2 seconds, Step 3 appears: credit offer with amount slider

### 3.4 Select amount and fill in card details:

| Field | Value |
|---|---|
| Amount slider | Drag to **200 AZN** (or any amount in the range) |
| Kart nömrəsi | `4169731234567890` (16 digits) |
| Checkbox | ✅ Tick "Bu kart məlumatı mənə məxsusdur" |
| Faktiki yaşayış ünvanı | `Bakı, Nizami r., Murtuza Muxtarov 12` |

### 3.5 Click "Təsdiq edirəm"

- The backend does the following:
  1. Fetches `customer_full_name` from stub PersonalInfo
  2. Fetches `akb_score` from stub AKB Score (Point=650)
  3. Fetches credit offer → determines `term_months` from amount
  4. Saves everything to DB
  5. **Runs the credit engine** (12 rejection rules)
  6. Returns the final status

### 3.6 Check the result:

- **Green screen** ("Müraciətiniz qəbul olundu!"): Engine passed all rules → status is `pending_approval`
- **Red screen** ("Müraciətiniz rədd edildi"): Engine rejected → see the reason
- **Amber screen** ("Müraciətiniz qeydə alındı"): Engine didn't finish → check server logs

### What happened behind the scenes:

The stub server returned default responses:
- AKB Score: Point=650 (normal score, no stop factor)
- PersonalInfo: "Test Müştəri", born 1991 (age ~35, under 69)
- AKB History: empty (no liabilities, clean customer)
- AZMK Blacklist: not blacklisted
- LW Loans: no loans (new customer)

→ Credit level = **new** → status = **pending_approval**

Note the **application number** shown on the success screen (e.g. "Muraciət №: 1").

---

## Step 4: View in RDC Dashboard

### 4.1 Open the application list:

```
http://localhost:8000/index.html
```

You should see your application in the list with:
- Status: `pending_approval` (yellow badge)
- Customer PIN: `TEST001`
- Amount: `200 AZN`

### 4.2 Open the application detail:

Click on the application, or go directly to:
```
http://localhost:8000/detail.html?id=1
```
(replace `1` with your application ID)

### 4.3 What you should see:

- **Customer info**: full name "Test Müştəri", PIN, phone, card, address
- **Credit offer**: approved amount 200 AZN, rate 30%, term 3 months
- **Check results**: all checks passed (blacklist, AKB score, age, etc.)
- **MyGov Təsdiqi** section (blue box with 2 cards):
  - İş Yeri Sorgusu (Employment verification)
  - Pensiya Sorgusu (Pension verification)

---

## Step 5: MyGov Verification (Expert Flow)

### 5.1 Employment verification:

1. Click **"İş Yeri Sorgusu Göndər"**
   - SMS sent to customer (check server log for mock SMS)
   - Message: "SMS gonderildi! Müştəri MyGov app-a daxil olub icazə verməlidir."

2. Click **"Məlumatı Yoxla"**
   - Backend fetches MyGov data from stub (default: employment_ok scenario)
   - Staj check runs: 8 months at current job → **PASS**
   - Green badge: "✓ Keçdi"
   - Message: "Cari iş yerində staj 8.0 ay (≥ 6 ay) — uyğundur"

### 5.2 Pension verification (if needed):

1. Click **"Pensiya Sorgusu Göndər"**
2. Click **"Məlumatı Yoxla"**
   - Default stub: no disability (group 0) → **PASS**
   - Green badge: "✓ Keçdi"

### 5.3 If verification fails:

- Red badge: "✗ İmtina"
- Red warning box: "⚠ Müraciət Avtomatik İmtina Edildi"
- Application status changes to `rejected`
- The approve/reject buttons disappear (terminal status)

---

## Step 6: Expert Approves

After MyGov verification passes:

1. Select credit level from dropdown (e.g. "new")
2. Click **"Təsdiq Et"** (green button)
3. Application status changes to `approved`

### Or add contact phones and complete:

If you want to test the CompleteApplication flow:

1. Use Postman or curl to call:
   ```bash
   curl -X PUT http://localhost:8000/api/applications/1/complete \
     -H "Content-Type: application/json" \
     -d '{
       "contact1_phone": "+994501111111",
       "contact2_phone": "+994502222222",
       "contact3_phone": "+994503333333"
     }'
   ```
2. This triggers the credit engine again (with contacts)
3. Check status: `GET http://localhost:8000/api/applications/1/status`

---

## Step 7: Test Rejection Scenarios

To test different rejection rules, modify the stub scenario by changing
the `?scenario=` parameter in the stub URL. But since the stub is stateless,
the easiest way is to use Postman to call the stub directly:

### Test AKB stop factor:

```bash
curl "http://localhost:8090/api/router/akb-score?fin=TEST001&scenario=stop_factor"
```

Then submit a new application via apply.html — the engine will get
Point=1 from the stub and reject with "AKB stop factor: AB (score=1)".

### Test age > 69:

```bash
curl "http://localhost:8090/api/router/personal-info?fin=TEST001&scenario=old_customer"
```

Then submit — the engine will get DOB 1950 → age 76 → reject.

### Test all stub scenarios:

Browse to the Postman folder **"10. LW Stub Scenarios"** and run any
scenario request to see what the stub returns.

---

## Troubleshooting

### Problem: "connection refused" on apply.html

**Solution:** The RDC server is not running. Start it with:
```bash
cd source
go run .
```

### Problem: OTP code not found in log

**Solution:** Make sure `OTP_USE_MOCK=true` in `.env`. The OTP code appears
in the server terminal as:
```
INFO mock OTP code phone=+994501234567 code=123456
```

### Problem: "texniki xəta" on customer-confirm

**Solution:** The stub server is not running or not reachable. Check:
1. `LW_USE_STUB=true` in `.env`
2. `LW_USE_MOCK=false` in `.env`
3. Server log shows "LW stub server starting"
4. `curl http://localhost:8090/stub/health` returns OK

### Problem: Application stuck in "pending" (amber screen)

**Solution:** The credit engine failed. Check server logs for errors.
Common causes:
- Database migration issues → set `MIGRATIONS_DROP_RECREATE=true` and restart
- Stub server not responding → verify with health check

### Problem: Dashboard shows no applications

**Solution:** Make sure you completed the customer-confirm step on apply.html.
Only confirmed applications appear in the dashboard with `pending_approval`
status.

### Problem: "page not found" for detail.html

**Solution:** Make sure you include the `?id=` parameter:
```
http://localhost:8000/detail.html?id=1
```

---

## Quick Reference

| URL | Purpose |
|---|---|
| `http://localhost:8000/apply.html` | Customer application form |
| `http://localhost:8000/index.html` | RDC dashboard (application list) |
| `http://localhost:8000/detail.html?id=1` | Application detail (expert view) |
| `http://localhost:8000/landing.html` | ALPUL landing page |
| `http://localhost:8090/stub/health` | Stub server health check |

| .env setting | Test value | Production value |
|---|---|---|
| `LW_USE_MOCK` | false | false |
| `LW_USE_STUB` | true | false |
| `LW_BASE_URL` | (not used) | https://real-lw-router.example.com |
| `LW_API_KEY` | (not used) | real-api-key |
| `OTP_USE_MOCK` | true | false |
| `MIGRATIONS_DROP_RECREATE` | true (clean test) | false (keep data) |

---

## Summary

```
┌──────────────────────────────────────────────────────────────┐
│ FULL END-TO-END FLOW                                         │
│                                                              │
│  1. go run .                          → Server starts        │
│  2. http://localhost:8000/apply.html  → Customer applies     │
│  3. Fill FIN + Seriya + Phone         → OTP sent (log)       │
│  4. Enter OTP                         → Offer shown          │
│  5. Select amount + card + address    → Engine runs          │
│  6. See result (pass/reject)          → Customer done        │
│  7. http://localhost:8000/index.html  → Dashboard shows app  │
│  8. detail.html?id=1                  → Expert reviews       │
│  9. MyGov verify buttons              → Employment/pension   │
│ 10. Approve                           → Application approved │
└──────────────────────────────────────────────────────────────┘
```
