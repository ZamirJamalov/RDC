-- ============================================================
-- Fix OTP Unique Constraint (Phase 3 fix)
-- ============================================================
-- The original UNIQUE (phone, status) constraint in 002_otp_codes.sql
-- was too strict — it prevented a phone from being verified more than
-- once (e.g., across multiple applications). This migration drops that
-- constraint and replaces it with a filtered unique index that only
-- applies to 'active' codes, which is what we actually want.
-- ============================================================

-- Drop the problematic constraint
IF EXISTS (SELECT * FROM sys.tables t
           JOIN sys.key_constraints k ON k.parent_object_id = t.object_id
           WHERE t.name = 'otp_codes' AND k.name = 'UQ_otp_codes_phone_active')
BEGIN
    ALTER TABLE otp_codes DROP CONSTRAINT UQ_otp_codes_phone_active;
END;
GO

-- Add a filtered unique index: only one ACTIVE code per phone at a time.
-- This allows multiple verified/expired/consumed codes per phone (history),
-- while still preventing two active codes from existing simultaneously.
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'UQ_otp_codes_phone_active' AND object_id = OBJECT_ID('otp_codes'))
BEGIN
    CREATE UNIQUE INDEX UQ_otp_codes_phone_active
    ON otp_codes (phone)
    WHERE status = 'active';
END;
GO
