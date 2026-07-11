-- ============================================================
-- Drop OTP Filtered Unique Index (Variant 2: multiple active codes)
-- ============================================================
-- The filtered unique index UQ_otp_codes_phone_active (created in
-- migration 006) only allowed one active code per phone at a time.
-- This forced SendOTP to expire the previous active code before
-- inserting a new one, which effectively bypassed the TTL — the
-- customer could get a new OTP every 60 seconds (rate limit) without
-- waiting for the 5-minute TTL to expire.
--
-- Business decision: allow multiple active codes per phone. The
-- VerifyOTP method only accepts the LATEST active code (ORDER BY
-- created_at DESC). Previous active codes expire naturally when
-- their TTL passes.
--
-- This means:
--   - Customer can request a new OTP after 60s (rate limit) if SMS
--     didn't arrive — no need to wait 5 minutes
--   - TTL is respected — old codes expire naturally after 5 minutes
--   - Only the most recent code is valid for verification (security)
-- ============================================================

IF EXISTS (SELECT * FROM sys.indexes WHERE name = 'UQ_otp_codes_phone_active' AND object_id = OBJECT_ID('otp_codes'))
BEGIN
    DROP INDEX UQ_otp_codes_phone_active ON otp_codes;
END;
GO
