package service

import (
        "context"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
        "rdc-source/pkg/otp"
)

// InitApplicationRequest is the body for POST /api/applications/init.
// This is what the customer fills in on the public website.
type InitApplicationRequest struct {
        CustomerPIN    string `json:"customer_pin"`
        CustomerSerial string `json:"customer_serial"`
        CustomerPhone  string `json:"customer_phone"`
}

// InitApplication creates a new application with minimal info (PIN, serial, phone)
// and sends an OTP to the customer's phone. The application starts in
// pending_customer status. Cutoff checks (AKB, blacklist) will be added later.
func (s *ApplicationService) InitApplication(ctx context.Context, req *InitApplicationRequest) (*model.LoanApplication, error) {
        if req.CustomerPIN == "" {
                return nil, fmt.Errorf("customer_pin is required")
        }
        if req.CustomerPhone == "" {
                return nil, fmt.Errorf("customer_phone is required")
        }

        app := &model.LoanApplication{
                CustomerPIN:    req.CustomerPIN,
                CustomerSerial: req.CustomerSerial,
                CustomerPhone:  req.CustomerPhone,
                Status:         model.StatusPendingCustomer,
        }

        if err := s.repo.CreateApplication(ctx, app); err != nil {
                return nil, fmt.Errorf("failed to create application: %w", err)
        }

        // Send OTP
        otpResp, err := s.otpService.SendOTP(ctx, req.CustomerPhone)
        if err != nil {
                return nil, fmt.Errorf("failed to send OTP: %w", err)
        }
        if !otpResp.Sent {
                return nil, fmt.Errorf("OTP could not be sent (rate limited). Retry after %d seconds", otpResp.RetryAfterS)
        }

        slog.Info("application initialized, OTP sent",
                "application_id", app.ID,
                "customer_pin", req.CustomerPIN,
                "phone", req.CustomerPhone)

        return app, nil
}

// VerifyInitApplicationRequest is the body for POST /api/applications/init/verify.
type VerifyInitApplicationRequest struct {
        ApplicationID int    `json:"application_id"`
        Phone         string `json:"phone"`
        OTPCode       string `json:"otp_code"`
}

// VerifyInitApplication verifies the OTP code and transitions the application
// from pending_customer to pending_expert. In this version, no cutoff checks
// are performed — the customer is assumed to have passed.
func (s *ApplicationService) VerifyInitApplication(ctx context.Context, req *VerifyInitApplicationRequest) (*model.LoanApplication, error) {
        if req.ApplicationID <= 0 {
                return nil, fmt.Errorf("application_id is required")
        }
        if req.Phone == "" || req.OTPCode == "" {
                return nil, fmt.Errorf("phone and otp_code are required")
        }

        // 1. Verify OTP
        verifyResp, err := s.otpService.VerifyOTP(ctx, req.Phone, req.OTPCode)
        if err != nil {
                return nil, fmt.Errorf("OTP verification failed: %w", err)
        }
        if !verifyResp.Valid {
                return nil, fmt.Errorf("invalid OTP code, %d attempts remaining", verifyResp.Attempts)
        }

        // 2. Fetch application
        app, err := s.repo.GetApplicationByID(ctx, req.ApplicationID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }
        if app.Status != model.StatusPendingCustomer {
                return nil, fmt.Errorf("application is not in pending_customer status (current: %s)", app.Status)
        }

        // 3. Transition to pending_expert
        // NOTE: Cutoff checks (AKB, blacklist) will be added here in the next phase.
        // For now, we skip them and go straight to pending_expert.
        app.Status = model.StatusPendingExpert
        if err := s.repo.UpdateApplicationStatus(ctx, app.ID, app.Status); err != nil {
                return nil, fmt.Errorf("failed to update status: %w", err)
        }

        slog.Info("application verified, waiting for expert",
                "application_id", app.ID,
                "customer_pin", app.CustomerPIN)

        return app, nil
}

// CompleteApplicationRequest is the body for PUT /api/applications/{id}/complete.
// The expert fills in these fields after the customer verifies their phone.
type CompleteApplicationRequest struct {
        CustomerFullName string  `json:"customer_full_name"`
        Amount           float64 `json:"amount"`
        TermMonths       int     `json:"term_months"`
        LoanPurpose      string  `json:"loan_purpose"`
        AkbScore         int     `json:"akb_score"`
        CardNumber       string  `json:"card_number"`
        Contact1Phone    string  `json:"contact1_phone"`
        Contact2Phone    string  `json:"contact2_phone"`
        Contact3Phone    string  `json:"contact3_phone"`
        ActualAddress    string  `json:"actual_address"`
}

// CompleteApplication fills in the remaining fields and triggers the credit engine.
// Called by the expert after the customer has verified their phone.
//
// PR #58: validation relaxed. When the customer has already gone through the
// customer-confirm flow (POST /api/applications/{id}/customer-confirm), fields
// like customer_full_name, amount, term_months, card_number, actual_address,
// and akb_score are already populated. The expert's job is then to add the
// 3 contact phones (collected during the verification call) and trigger the
// engine.
//
// Validation rules (PR #58):
//   - contact1_phone is REQUIRED (expert must collect at least 1 contact)
//   - contact2_phone, contact3_phone are OPTIONAL
//   - If customer_full_name is empty in the DB AND empty in the request → error
//   - If amount is 0 in the DB AND 0 in the request → error
//   - If term_months is 0 in the DB AND 0 in the request → error
//   - If card_number is empty in the DB AND empty in the request → error
//
// In short: fields already filled by customer-confirm are NOT re-required.
// The expert can override them by providing non-zero values in the request.
func (s *ApplicationService) CompleteApplication(ctx context.Context, appID int, req *CompleteApplicationRequest) (*model.LoanApplication, error) {
        if appID <= 0 {
                return nil, fmt.Errorf("invalid application id")
        }
        if req.Contact1Phone == "" {
                return nil, fmt.Errorf("contact1_phone is required (expert must collect at least 1 contact)")
        }

        // 1. Fetch application
        app, err := s.repo.GetApplicationByID(ctx, appID)
        if err != nil {
                return nil, fmt.Errorf("application not found: %w", err)
        }
        if app.Status != model.StatusPendingExpert {
                return nil, fmt.Errorf("application is not in pending_expert status (current: %s)", app.Status)
        }

        // 2. Merge request fields into the existing application.
        // For each field: if the request provides a non-empty value, use it;
        // otherwise keep the existing DB value (which may have been set by
        // customer-confirm). After the merge, validate that all required fields
        // are populated.
        if req.CustomerFullName != "" {
                app.CustomerFullName = req.CustomerFullName
        }
        if req.Amount > 0 {
                app.Amount = req.Amount
        }
        if req.TermMonths > 0 {
                app.TermMonths = req.TermMonths
        }
        if req.CardNumber != "" {
                app.CardNumber = req.CardNumber
        }
        if req.ActualAddress != "" {
                app.ActualAddress = req.ActualAddress
        }
        if req.AkbScore > 0 {
                app.AkbScore = req.AkbScore
        }
        app.LoanPurpose = req.LoanPurpose
        app.Contact1Phone = req.Contact1Phone
        app.Contact2Phone = req.Contact2Phone
        app.Contact3Phone = req.Contact3Phone
        app.Status = model.StatusPending // will transition to "checking" by the engine

        // 3. Validate that all required fields are now populated (either from
        // customer-confirm or from the expert's request).
        if app.CustomerFullName == "" {
                return nil, fmt.Errorf("customer_full_name is required (not set by customer-confirm and not provided in request)")
        }
        if app.Amount <= 0 {
                return nil, fmt.Errorf("amount must be greater than zero (not set by customer-confirm and not provided in request)")
        }
        if app.TermMonths <= 0 {
                return nil, fmt.Errorf("term_months must be greater than zero (not set by customer-confirm and not provided in request)")
        }
        if len(app.CardNumber) != 16 {
                return nil, fmt.Errorf("card_number must be exactly 16 digits (current: %d)", len(app.CardNumber))
        }

        // 4. Save to DB
        if err := s.repo.UpdateApplicationDetails(ctx, appID, app); err != nil {
                return nil, fmt.Errorf("failed to update application: %w", err)
        }

        // 5. Trigger credit engine async
        s.triggerAsyncProcessing(app)

        slog.Info("application completed, credit engine triggered",
                "application_id", appID,
                "customer_pin", app.CustomerPIN,
                "amount", app.Amount,
                "term_months", app.TermMonths,
                "contact1_phone", app.Contact1Phone)

        // Return the updated app (status is now pending, engine will transition to checking)
        return s.repo.GetApplicationByID(ctx, appID)
}

// Ensure otp import is used
var _ = otp.Provider(nil)
var _ = time.Second
