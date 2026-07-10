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

## Step 7: Create the Loan Application

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
| 7 | 3. Offer + Application | Application: Happy path | 201 Created (id saved) |
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

## Done

You now know how to test the RDC API step by step.
Follow the steps in order, and you will see the full happy path from start to finish.
