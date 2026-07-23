# RDC API — Happy Path Testing Guide

This guide shows you how to test the RDC API step by step.
Follow the steps in order.
Each step tells you which Postman folder to open and which request to send.

---

## Before You Start

### Step 0: Start the Server

Open a terminal and run:

```bash
cd source
go run .
```

You should see:

```
RDC server starting on :8000
```

The server is now running.

### Step 0.1: Open Postman

1. Open Postman.
2. Click **Import**.
3. Select these two files:
   - `Docs/postman/RDC_PostmanCollection.json`
   - `Docs/postman/RDC_PostmanEnvironment.json`
4. Click **Import**.

### Step 0.2: Select Environment

1. Look at the top right corner of Postman.
2. Click the dropdown.
3. Select **RDC — Local Environment**.

### Step 0.3: Set Customer Info

1. Click the **eye icon** (top right) to see variables.
2. Click **Edit** (pencil icon).
3. Set these values:
   - `customer_pin` = `TEST001`
   - `customer_phone` = `+994501234567`
4. Click **Save**.

Now you are ready to test.

---

## The Happy Path Flow

The happy path has **10 main steps**.
Each step matches one part of the flow diagram.
Follow them in order 1 → 10.

---

## Step 1: Set Up Mock Customer Data

**Goal:** Create a customer with no loans (new customer).

**Postman folder:** `0. Setup (Mock Data)`

**Request to send:** `🟢 Setup: Yeni müştəri (0 kredit)`

**What to do:**
1. Open the request.
2. Click **Send**.

**What you should see:**
- Status: `200 OK`
- Response body has `customer_pin` and `loan_count: 0`

**Why this step:** The flow diagram starts with a customer who has no loan history.
This step puts the customer data into the mock database.

---

## Step 2: Check the Customer's Loans

**Goal:** Verify the customer has no loans.

**Postman folder:** `0. Setup (Mock Data)`

**Request to send:** `🟢 Query: Müştəri kreditlərini sorğula`

**What to do:**
1. Open the request.
2. Click **Send**.

**What you should see:**
- Status: `200 OK`
- `loan_count: 0`
- `has_existing_loans: false`

**Why this step:** The flow diagram checks the customer's loan history from LW.
This step confirms the mock data is correct.

---

## Step 3: Get the Offer (Available Loan Options)

**Goal:** See what loan amounts and terms the customer can get.

**Postman folder:** `3. Offer + Application`

**Request to send:** `🟢 Offer: Yeni müştəri`

**What to do:**
1. Open the request.
2. Click **Send**.

**What you should see:**
- Status: `200 OK`
- `credit_level: "new"`
- `ranges` shows available options (100-500 AZN, 3 months, 30% rate)

**Why this step:** The flow diagram shows the customer the loan offer before they apply.
The `credit_level` is saved automatically to the `{{credit_level}}` variable.

---

## Step 4: Send OTP (SMS Code)

**Goal:** Send a 6-digit code to the customer's phone.

**Postman folder:** `1. OTP Verification`

**Request to send:** `🟢 OTP Send: Happy path`

**What to do:**
1. Open the request.
2. Click **Send**.

**What you should see:**
- Status: `200 OK`
- `sent: true`

**IMPORTANT — Get the code:**
- Look at the **server terminal** (where `go run .` is running).
- Find a line like: `INFO mock OTP code phone=+994501234567 code=123456`
- Copy the 6-digit code (example: `123456`).

**Now update the variable:**
1. Click the **eye icon** (top right).
2. Click **Edit**.
3. Set `otp_code` = the code you copied (example: `123456`).
4. Click **Save**.

**Why this step:** The flow diagram sends an SMS to verify the customer's phone.

---

## Step 5: Verify OTP

**Goal:** Verify the SMS code and get a verification token.

**Postman folder:** `1. OTP Verification`

**Request to send:** `🟢 OTP Verify: Happy path`

**What to do:**
1. Make sure you set `otp_code` in Step 4.
2. Open the request.
3. Click **Send**.

**What you should see:**
- Status: `200 OK`
- `valid: true`
- `token` has a long string (example: `a1b2c3d4...`)

The token is saved automatically to `{{otp_token}}`.

**Why this step:** The flow diagram verifies the customer's phone number.

---

## Step 6: Check Personal Info and Credit Score

**Goal:** Get the customer's personal info, AKB score, and blacklist status from LW.

**Postman folder:** `2. LW Router Checks`

**Send these 4 requests one by one:**

### 6a. Personal Info
- Request: `🟢 Personal Info: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `fin` and `full_name`.

### 6b. AKB Score
- Request: `🟢 AKB Score: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `score`.

### 6c. AKB History
- Request: `🟢 AKB History: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `report_id`.

### 6d. Blacklist Check
- Request: `🟢 Blacklist: Not blacklisted`
- Send it.
- You should see: Status `200 OK`, `is_blacklisted: false`.

**Why this step:** The flow diagram checks the customer's identity and credit history.
In mock mode, all checks return "not blacklisted" and a default score.

---

## Step 6.5: Customer Confirm (Public Website Flow) — NEW in PR #58

**Goal:** Simulate the customer submitting their credit offer confirmation on the public website.

**Postman folder:** `3. Offer + Application`

This step replaces the old "create application with all fields" approach (Step 7 below) for the customer-facing flow. In the new flow (PR #58), the customer only fills in:
- amount (selected from offer range)
- card number (16 digits)
- actual address
- card ownership checkbox

The backend fetches `customer_full_name`, `akb_score`, and `term_months` automatically from LW router.

### 6.5a. Customer Confirm (Happy Path)

- Request: `🟢 Customer Confirm: Happy path (amount + card + address + checkbox)`
- Make sure `{{application_id}}` is set (from Step 5 Init Verify).
- Click **Send**.

**What you should see:**
- Status: `200 OK`
- `customer_full_name` is populated (from PersonalInfo)
- `akb_score` is populated (from AKB)
- `term_months` is populated (from offer, matched to amount)
- `customer_confirmed_at` has a timestamp
- `card_ownership_confirmed: true`
- `status: "pending_expert"` (still waiting for expert)

**Behind the scenes:**
1. Backend calls `GetPersonalInfo` → fills `customer_full_name`
2. Backend calls `GetAkbScore` → fills `akb_score`
3. Backend calls `GetOffer` → finds the range matching `amount=200` → sets `term_months`
4. Saves everything with `customer_confirmed_at = now()`
5. Application stays in `pending_expert` — expert must still add contact phones

### 6.5b. Customer Confirm — Error Cases

Test the validation rules by sending these requests one by one:

| Request | Expected Error |
|---------|----------------|
| `🔴 Customer Confirm: amount aralıqdan kənar (9999 AZN)` | `400` — "keçərli deyil" |
| `🔴 Customer Confirm: card_number 15 rəqəm` | `400` — "16 digits" |
| `🔴 Customer Confirm: kart sahibliyi təsdiq olunmayıb` | `400` — "ownership must be confirmed" |
| `🔴 Customer Confirm: app pending_expert deyil` | `400` — "pending_expert status" |

### 6.5c. Complete (Expert Adds Contacts Only)

After the customer confirms, the expert calls the customer to collect 3 contact phone numbers, then completes the application.

- Request: `🟢 Complete (customer-confirm-dan sonra): Expert yalnız kontaktları daxil edir`
- Click **Send**.

**What you should see:**
- Status: `200 OK`
- `contact1_phone`, `contact2_phone`, `contact3_phone` populated
- `customer_full_name`, `amount`, `term_months`, `card_number` preserved from customer-confirm
- `status: "pending"` (engine starts asynchronously)

**IMPORTANT — Wait 2 seconds:**
The credit engine runs in the background. Wait 2-3 seconds before checking the final status in Step 8.

**Why this step:** PR #58 introduced the customer-facing confirmation flow. The customer fills only 4 fields on the website; the backend fetches the rest from LW router (fail-hard on error).

---

## Step 7: Create the Loan Application (Legacy / Direct API)


**Goal:** Submit the loan application.

**Postman folder:** `3. Offer + Application`

**Request to send:** `🟢 Application: Happy path (yeni → pending_approval)`

**What to do:**
1. Open the request.
2. Click **Send**.

**What you should see:**
- Status: `201 Created`
- `id` has a number (example: `1`)
- `status: "pending"`

The `id` is saved automatically to `{{application_id}}`.

**IMPORTANT — Wait 2 seconds:**
The credit engine runs in the background.
Wait 2-3 seconds for it to finish before the next step.

**Why this step:** The flow diagram creates the application and starts the credit decision pipeline.

---

## Step 8: Check the Application Status

**Goal:** See the result of the credit decision.

**Postman folder:** `7. Status & Decision`

### 8a. Get Full Status
- Request: `🟢 Get Status: Full status`
- Send it.
- You should see:
  - Status `200 OK`
  - `status: "pending_approval"` (for a new customer)
  - `checks` has 4 check results (all passed)
  - `decision` has `approved_amount` and `approved_rate`

### 8b. Get All Checks
- Request: `🟢 Get Checks: All check results`
- Send it.
- You should see: Status `200 OK`, a list of 4 checks:
  1. `lms_active_loan_check` — passed
  2. `lms_payment_history_check` — passed
  3. `credit_level_check` — passed
  4. `blacklist_check` — passed

**Why this step:** The flow diagram shows the customer the decision.
For a "new" customer, the status is `pending_approval` (waiting for expert review).

---

## Step 9: Start SIMA KYC (Identity Verification)

**Goal:** Start the SIMA KYC process for the customer.

**Postman folder:** `4. SIMA KYC`

### 9a. Start KYC
- Request: `🟢 SIMA Init: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `session_id`.
- The `session_id` is saved to `{{sima_session_id}}`.

### 9b. Simulate KYC Success (Callback)
- Request: `🟢 SIMA Callback: Success status`
- Send it.
- You should see: Status `200 OK`, `received: true`.

**Why this step:** The flow diagram starts SIMA KYC and waits for the result.
In real life, the customer verifies their face on their phone.
Here, we simulate the success callback.

---

## Step 10: Get MyGov Data (Official Income)

**Goal:** Get the customer's official income from MyGov.

**Postman folder:** `5. MyGov`

### 10a. Generate Permission Link
- Request: `🟢 MyGov Permission Link: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `url`.

### 10b. Fetch Data
- Request: `🟢 MyGov Fetch Data: Happy path`
- Send it.
- You should see: Status `200 OK`, `status: "fetched"`.

**Why this step:** The flow diagram asks the customer for permission to access their official data.
In real life, the customer opens the URL and grants permission.
Here, we simulate the data fetch.

---

## Step 11: Get ASAN Finance Data

**Goal:** Get the customer's official income from ASAN Finance.

**Postman folder:** `6. ASAN Finance`

- Request: `🟢 ASAN Finance: Happy path`
- Send it.
- You should see: Status `200 OK`, response has `official_income`.

**Why this step:** The flow diagram checks the customer's official income.

---

## Step 12: Expert Approves the Application

**Goal:** The operator (expert) reviews and approves the application.

**Postman folder:** `8. Expert Panel`

### 12a. Check the Queue
- Request: `🟢 Expert Queue: List pending`
- Send it.
- You should see: Status `200 OK`.

### 12b. Get Application Detail
- Request: `🟢 Expert Get: Application detail`
- Send it.
- You should see: Status `200 OK`, response has `id` and `status: "pending_approval"`.

### 12c. Approve the Application
- Request: `🟢 Expert Approve: Happy path`
- Send it.
- You should see: Status `200 OK`, `status: "approved"`.

**Why this step:** The flow diagram has the expert review and approve the application.
After this step, the application is approved.

---

## Step 13: Verify the Final Status

**Goal:** Confirm the application is approved.

**Postman folder:** `7. Status & Decision`

- Request: `🟢 Get Status: Full status`
- Send it.
- You should see:
  - `status: "approved"`
  - `decision` has `approved_amount` and `approved_rate`
  - No `rejection_reason`

**DONE!** You completed the happy path. 🎉

---

## Quick Summary Table

| Step | Folder | Request | Result |
|---:|---|---|---|
| 1 | 0. Setup | Setup: Yeni müştəri | Customer created |
| 2 | 0. Setup | Query: Müştəri kreditlərini sorğula | 0 loans confirmed |
| 3 | 3. Offer + Application | Offer: Yeni müştəri | credit_level = new |
| 4 | 1. OTP Verification | OTP Send: Happy path | SMS sent (code in log) |
| 5 | 1. OTP Verification | OTP Verify: Happy path | valid = true |
| 6a | 2. LW Router Checks | Personal Info | 200 OK |
| 6b | 2. LW Router Checks | AKB Score | 200 OK |
| 6c | 2. LW Router Checks | AKB History | 200 OK |
| 6d | 2. LW Router Checks | Blacklist | not blacklisted |
| **6.5a** | **3. Offer + Application** | **Customer Confirm: Happy path** | **200 OK (full_name + akb + term auto-filled)** |
| **6.5b** | **3. Offer + Application** | **Customer Confirm: Error cases (4)** | **400 errors** |
| **6.5c** | **3. Offer + Application** | **Complete (after customer-confirm): contacts only** | **200 OK (engine triggered)** |
| 7 | 3. Offer + Application | Application: Happy path (legacy) | 201 Created (id saved) |
| 8a | 7. Status & Decision | Get Status | pending_approval |
| 8b | 7. Status & Decision | Get Checks | 4 checks passed |
| 9a | 4. SIMA KYC | SIMA Init | session_id saved |
| 9b | 4. SIMA KYC | SIMA Callback | received = true |
| 10a | 5. MyGov | Permission Link | url returned |
| 10b | 5. MyGov | Fetch Data | fetched |
| 11 | 6. ASAN Finance | ASAN Finance | official_income |
| 12a | 8. Expert Panel | Expert Queue | 200 OK |
| 12b | 8. Expert Panel | Expert Get | pending_approval |
| 12c | 8. Expert Panel | Expert Approve | approved |
| 13 | 7. Status & Decision | Get Status | **approved** ✅ |

**New flow (PR #58 + PR #63 Variant B, recommended for production):** Steps 1 → 2 → 3 → 4 → 5 → 6.5a → 6.5c → 8a → MyGov verify
**Legacy flow (still works for direct API testing):** Steps 1 → 2 → 3 → 4 → 5 → 7 → 8a

---

## Important Notes

### OTP Code
- The OTP code is in the **server terminal**.
- After Step 4, look for: `INFO mock OTP code phone=... code=123456`
- Copy the code to the `otp_code` variable before Step 5.

### Wait Between Steps
- After Step 7 (Create Application), **wait 2-3 seconds**.
- The credit engine runs in the background.
- If you check status too fast, it may still show "pending" or "checking".

### Auto-Saved Variables
- `application_id` — saved in Step 7
- `otp_token` — saved in Step 5
- `credit_level` — saved in Step 3
- `sima_session_id` — saved in Step 9a

### If Something Goes Wrong
- Check the **server terminal** for error messages.
- Check that the **environment** is selected (top right).
- Check that **variables** have values (eye icon).

---

## Other Scenarios

### Scenario A: Elite Customer (Auto-Approve)

If you want to test auto-approve (no expert needed):

1. **Step 1:** Send `🟢 Setup: Elite (2 completed, 1 early)` but change `customer_pin` to `ELITE001`.
2. **Step 7:** Send `🟢 Application: Happy path (elite → auto-approve)` but change `customer_pin` to `ELITE001`.
3. **Step 8:** Check status — it should be `approved` (not `pending_approval`).
4. Skip Steps 9-12 (no expert review needed).

### Scenario B: Active Loan (Reject)

If you want to test rejection:

1. **Step 1:** Send `🟢 Setup: Aktiv kreditli`.
2. **Step 7:** Send `🟢 Application: Happy path`.
3. **Wait 3 seconds.**
4. **Step 8:** Check status — it should be `rejected` with reason "Customer has active loans".

### Scenario C: Late Payment (Reject)

If you want to test late payment rejection:

1. **Step 1:** Send `🟢 Setup: Gecikməli`.
2. **Step 7:** Send `🟢 Application: Happy path`.
3. **Wait 3 seconds.**
4. **Step 8:** Check status — it should be `rejected` with reason "Late payments found in loan history".

---

## Troubleshooting

### Problem: Status is still "pending" or "checking"

**Solution:** Wait more time. The credit engine takes 1-3 seconds.

### Problem: OTP verify returns `valid: false`

**Solution:** Check the `otp_code` variable. It must match the code in the server log.

### Problem: 400 Bad Request

**Solution:** Check the response `error` field. It tells you what is wrong.

### Problem: 404 Not Found

**Solution:** Check the `application_id` variable. It must be a valid ID from Step 7.

### Problem: Connection refused

**Solution:** The server is not running. Start it with `go run .` in the `source` folder.

---

## LW Stub Scenarios (PR #61) — NEW

When the real LW router is not yet available, you can start the RDC server in
**stub mode** — an in-process stub HTTP server mimics the real LW router
responses, so you can exercise the full HTTP provider code path.

### Setting up stub mode

1. In `.env` (or your shell environment), set:
   ```
   LW_USE_MOCK=false
   LW_USE_STUB=true
   LW_STUB_PORT=8090
   ```
2. Restart the RDC server: `go run .`
3. You should see in the log:
   ```
   INFO LW stub server starting (development only) addr=:8090
   INFO using HTTP LW provider pointed at in-process stub base_url=http://localhost:8090
   ```

### Verifying the stub is up

- **Postman folder:** `10. LW Stub Scenarios (PR #61)`
- **Request:** `🟢 Stub Health Check`
- **URL:** `{{stub_url}}/stub/health` → resolves to `http://localhost:8090/stub/health`
- **Expected:** `{"status":"ok","service":"lw-stub"}`

If you see `ECONNREFUSED`, the stub server is not running — check `.env` and
restart the RDC server.

### Available scenarios

Every stub endpoint accepts a `?scenario=xxx` query parameter that controls
which canned response is returned. The Postman folder has one request per
scenario so you can run them individually:

| Group | Scenarios |
|---|---|
| AKB Score | default (Point=650), stop_factor (Point=1, AB), low_score (150), high_score (750), no_data (0), error (502) |
| AKB History | empty, delay_ratio_high (rule 2), active_delay_high (rule 6), delay_3m (rule 7), delay_6m (rule 8), delay_12m (rule 9), delay_18m (rule 10), high_monthly_payments (rule 12), error |
| PersonalInfo | default (35yo), old_customer (rule 3), young_customer (24yo), error |
| AZMK Blacklist | not blacklisted, blacklisted (rule 5), error |
| LW Blacklist | not blacklisted, blacklisted (T-1.5), error |
| ASAN Finance | default (500), low_income (200), high_income (1500), error |
| LW Loans | default (no loans), trusted (2 completed), active_loan, late_payment, error |
| LW Approve | default (signed+completed), contract_failed, error |

### Why this matters

The stub responses match the **exact format** that the real LW router will
return — including the SOAP-derived JSON for AKB Score
(`{return: {response, point}}`, PR #55). This means:

- The HTTPProvider's JSON parsing is exercised against realistic payloads
- Timeouts and error handling are tested (502 responses)
- When the real LW router is ready, switching is just config:
  ```
  LW_USE_STUB=false
  LW_BASE_URL=https://real-lw-router.example.com
  LW_API_KEY=real-api-key
  ```
- **No code changes needed** — the HTTPProvider already parses the same
  format the stub was serving.

### Testing rejection rules end-to-end

You can combine stub scenarios with the application flow to test each
rejection rule. Example for AKB stop factor (rule 4):

1. Start server in stub mode
2. Run a request to set the stub scenario:
   - `GET http://localhost:8090/api/router/akb-score?fin=PIN1&scenario=stop_factor`
   - (The stub is stateless — it returns the scenario response every time)
3. Submit an application through the RDC API
4. The credit engine will call the stub via HTTPProvider, get `Point=1, Response=AB`
5. Application will be rejected with reason: `AKB stop factor: AB (score=1)`

**Note:** Because the stub is stateless, all customers will get the same
scenario response. To test different customers with different scenarios,
you'd need to modify the stub code or run multiple stub instances on
different ports.

---

## MyGov Employment + Pension Verification (PR #65-#66) — NEW

When an application passes the credit engine (status = `pending_approval`), the
expert must verify the customer's employment or pension status via MyGov before
final approval.

### Prerequisites

- Application must be in `pending_approval` status (engine passed all 12 rules)
- Server must be in stub mode (`LW_USE_STUB=true`) for testing without real MyGov
- The customer's phone must be OTP-verified (so SMS can be sent)

### Step 1: Send Employment or Pension Request

The expert calls the customer, asks about their employment/pension status, then
triggers the appropriate MyGov permission request.

**Postman folder:** `5. MyGov`

**Request:** `🟢 Employment Request: MyGov link + SMS (PR #65)`

```
POST /api/applications/{{application_id}}/mygov-employment-request
```

**What happens:**
1. Backend generates a MyGov permission link
2. SMS is sent to the customer's phone with the link
3. Customer opens the link and grants permission in MyGov app

**Expected response:**
```json
{
  "application_id": 1,
  "url": "https://mygov.example.com/permit/...",
  "expires_at": "2026-07-23T10:05:00Z"
}
```

For pension verification, use `🟢 Pension Request: MyGov link + SMS (PR #65)` instead.

### Step 2: Verify the Data

After the customer grants permission in MyGov, the expert clicks "Verify".

**Request:** `🟢 Employment Verify: staj check + auto-reject (PR #65)`

```
POST /api/applications/{{application_id}}/mygov-employment-verify
```

**What happens:**
1. Backend fetches MyGov data (employment history)
2. Backend runs the 6-month tenure rule:
   - Current job tenure ≥ 6 months → PASS
   - Else if previous job + gap < 29 days → combined tenure ≥ 6 months → PASS
   - Else → FAIL → **auto-reject**
3. If FAIL, application status changes to `rejected` with a descriptive reason

**Expected response (pass):**
```json
{
  "application_id": 1,
  "verified": true,
  "status": "passed",
  "reason": "Cari iş yerində staj 8.0 ay (≥ 6 ay) — uyğundur",
  "check_type": "employment"
}
```

**Expected response (fail):**
```json
{
  "application_id": 1,
  "verified": false,
  "status": "rejected",
  "reason": "Cari staj 2.0 ay (< 6 ay) və əvvəlki iş yeri yoxdur — imtina",
  "check_type": "employment"
}
```

When `status = "rejected"`, the application is automatically rejected. Check with:
```
GET /api/applications/{{application_id}}/status
```
→ `status: "rejected"`, `rejection_reason: "Cari staj 2.0 ay..."`

### Step 3: Pension Verification (if applicable)

If the customer is a pensioner, use the pension verification flow instead:

**Request:** `🟢 Pension Verify: disability check + auto-reject (PR #65)`

```
POST /api/applications/{{application_id}}/mygov-pension-verify
```

**What happens:**
1. Backend fetches MyGov data (pension/disability info)
2. Backend checks `DisabilityGroup`:
   - `DisabilityGroup == 1` → **auto-reject** (1st group disability)
   - `DisabilityGroup == 0, 2, or 3` → PASS

**Expected response (pass):**
```json
{
  "application_id": 1,
  "verified": true,
  "status": "passed",
  "reason": "No 1st-group disability found",
  "check_type": "pension"
}
```

**Expected response (fail — 1st group disability):**
```json
{
  "application_id": 1,
  "verified": false,
  "status": "rejected",
  "reason": "1ci qrup əlilliyi aşkarlandı — pensiya sorgusu əsasında avtomatik imtina",
  "check_type": "pension"
}
```

### Testing with Stub Scenarios

The stub server (PR #61) provides 8 MyGov scenarios for testing different
verification outcomes. These are in the `10. LW Stub Scenarios` folder.

| Stub Scenario | Description | Expected Verify Result |
|---|---|---|
| `employment_ok` | 8 months at current job | passed |
| `employment_short_tenure` | 3mo current + 4mo previous, gap 10d | passed (combined 7mo) |
| `employment_short_tenure_long_gap` | 3mo + 4mo, gap 60d | rejected (gap ≥ 29d) |
| `employment_insufficient_tenure` | 2mo, no previous | rejected |
| `pension_disability_group1` | 1st group disability | rejected (auto-reject) |
| `pension_disability_group2` | 2nd group disability | passed |
| `pension_age` | Age pensioner, no disability | passed |
| `error` | Service error | 502 |

To test a specific scenario, call the stub directly:
```
GET http://localhost:8090/api/mygov/permission/data?token=T1&scenario=employment_insufficient_tenure
```

Then call the verify endpoint — the backend will fetch this data from the stub
and run the appropriate check.

### Dashboard UI

The RDC dashboard (`detail.html`) now shows a "MyGov Təsdiqi" section when the
application is in `pending_approval` status. The section contains:

- **İş Yeri Sorgusu** card with 2 buttons:
  - "İş Yeri Sorgusu Göndər" → triggers employment-request
  - "Məlumatı Yoxla" → triggers employment-verify
  - Result badge: green "✓ Keçdi" or red "✗ İmtina"

- **Pensiya Sorgusu** card with 2 buttons:
  - "Pensiya Sorgusu Göndər" → triggers pension-request
  - "Məlumatı Yoxla" → triggers pension-verify
  - Result badge + reason message

- **Auto-reject warning**: red box with "⚠ Müraciət Avtomatik İmtina Edildi"
  + rejection reason, shown when verification fails

### Important Notes

- The expert should call the customer FIRST and ask about their employment
  status before triggering the MyGov request. This avoids unnecessary MyGov
  permission requests for customers who don't have official income.
- Only ONE verification type is needed: employment OR pension, not both.
  The expert decides which based on the customer's situation.
- If employment verification passes, the expert can proceed to collect contact
  phones and approve the application via `PUT /api/applications/{id}/complete`.
- If either verification fails (auto-reject), the application cannot be
  recovered — the customer must file a new application.

---

## Done

You now know how to test the RDC API step by step.
Follow the steps in order, and you will see the full happy path from start to finish.
