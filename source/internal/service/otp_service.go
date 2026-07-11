package service

import (
        "context"
        "crypto/subtle"
        "fmt"
        "log/slog"
        "time"

        "rdc-source/internal/model"
        "rdc-source/internal/repository"
        "rdc-source/pkg/otp"
)

// OTPService handles OTP code generation, delivery, and verification (T-3.7).
//
// Flow:
//  1. SendOTP: generate 6-digit code → hash → store → send via SMS provider
//  2. VerifyOTP: lookup active code → compare hash → mark verified or increment attempts
//
// Security measures:
//   - Codes are stored as SHA-256 hashes (never plaintext)
//   - Rate limiting: max 1 SMS per minute per phone number
//   - Max 5 verification attempts per code, then the code is locked
//   - Codes expire after 5 minutes
//   - Verification token is a random hex string tied to the verified phone
type OTPService struct {
        provider otp.Provider
        repo     *repository.OTPRepo
}

// NewOTPService creates a new OTPService.
func NewOTPService(provider otp.Provider, repo *repository.OTPRepo) *OTPService {
        return &OTPService{
                provider: provider,
                repo:     repo,
        }
}

// SendOTP generates a new 6-digit code, stores its hash, and sends the code
// to the customer via SMS. Returns an error if:
//   - The phone number is empty
//   - Rate limit exceeded (more than 1 SMS per minute)
//   - The SMS provider fails
func (s *OTPService) SendOTP(ctx context.Context, phone string) (*model.OTPSendResponse, error) {
        if phone == "" {
                return nil, fmt.Errorf("phone is required")
        }

        // Rate limit: max 1 SMS per minute per phone
        // Uses SQL Server's GETDATE() for the time comparison to avoid timezone
        // mismatch between Go (UTC) and SQL Server (local time).
        recentCount, err := s.repo.CountRecentCodes(ctx, phone, model.OTPRateLimitWindow)
        if err != nil {
                return nil, fmt.Errorf("failed to check rate limit: %w", err)
        }
        if recentCount >= model.OTPRateLimitPerMin {
                slog.Warn("OTP rate limit exceeded", "phone", phone, "recent_count", recentCount)
                return &model.OTPSendResponse{
                        Phone:       phone,
                        Sent:        false,
                        RetryAfterS: model.OTPRateLimitWindow,
                }, nil
        }

        // Generate 6-digit code
        code, err := generateOTPCode(model.OTPCodeLength)
        if err != nil {
                return nil, fmt.Errorf("failed to generate OTP code: %w", err)
        }

        // Hash the code for storage (never store plaintext)
        codeHash := hashCode(code)

        // Store in DB
        expiresAt := time.Now().Add(time.Duration(model.OTPCodeTTL) * time.Second)
        if err := s.repo.Create(ctx, phone, codeHash, expiresAt); err != nil {
                return nil, fmt.Errorf("failed to store OTP code: %w", err)
        }

        // Send via SMS provider
        if err := s.provider.Send(ctx, phone, code); err != nil {
                slog.Error("failed to send OTP SMS",
                        "phone", phone,
                        "provider", s.provider.Name(),
                        "error", err)
                return nil, fmt.Errorf("failed to send OTP: %w", err)
        }

        slog.Info("OTP sent",
                "phone", phone,
                "provider", s.provider.Name(),
                "expires_in_s", model.OTPCodeTTL)

        return &model.OTPSendResponse{
                Phone:      phone,
                Sent:       true,
                ExpiresInS: model.OTPCodeTTL,
        }, nil
}

// VerifyOTP checks the code against the stored hash. If valid, marks the code
// as verified and returns a verification token. If invalid, increments the
// attempt counter and returns the remaining attempts.
//
// The verification token is a random hex string that the client must include
// when creating a loan application (to prove the phone was verified).
func (s *OTPService) VerifyOTP(ctx context.Context, phone, code string) (*model.OTPVerifyResponse, error) {
        if phone == "" || code == "" {
                return nil, fmt.Errorf("phone and code are required")
        }

        // Expire old codes before lookup (keeps the table clean)
        if _, err := s.repo.ExpireOldCodes(ctx); err != nil {
                slog.Warn("failed to expire old OTP codes", "error", err)
        }

        // Lookup active code
        stored, err := s.repo.GetActiveByPhone(ctx, phone)
        if err != nil {
                return &model.OTPVerifyResponse{
                        Phone:    phone,
                        Valid:    false,
                        Attempts: model.OTPMaxAttempts,
                }, nil
        }

        // Check if code is expired
        if time.Now().After(stored.ExpiresAt) {
                return &model.OTPVerifyResponse{
                        Phone:    phone,
                        Valid:    false,
                        Attempts: 0,
                }, nil
        }

        // Compare hash (constant-time to prevent timing attacks)
        codeHash := hashCode(code)
        if subtle.ConstantTimeCompare([]byte(codeHash), []byte(stored.CodeHash)) != 1 {
                // Wrong code — increment attempts
                if err := s.repo.IncrementAttempts(ctx, stored.ID); err != nil {
                        slog.Error("failed to increment OTP attempts", "error", err)
                }
                remaining := stored.MaxAttempts - stored.Attempts - 1
                if remaining < 0 {
                        remaining = 0
                }
                slog.Warn("OTP verification failed (wrong code)",
                        "phone", phone,
                        "attempts_remaining", remaining)
                return &model.OTPVerifyResponse{
                        Phone:    phone,
                        Valid:    false,
                        Attempts: remaining,
                }, nil
        }

        // Correct code — mark as verified
        if err := s.repo.MarkVerified(ctx, stored.ID); err != nil {
                return nil, fmt.Errorf("failed to mark OTP as verified: %w", err)
        }

        // Generate verification token (random hex string)
        token, err := generateToken()
        if err != nil {
                return nil, fmt.Errorf("failed to generate verification token: %w", err)
        }

        slog.Info("OTP verified", "phone", phone)

        return &model.OTPVerifyResponse{
                Phone: phone,
                Valid: true,
                Token: token,
        }, nil
}
