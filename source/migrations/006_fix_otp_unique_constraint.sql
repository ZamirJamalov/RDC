-- ============================================================
-- Fix OTP Unique Constraint (Phase 3 fix)
-- ============================================================
-- The original UNIQUE (phone, status) constraint in 002_otp_codes.sql
-- was too strict. This migration drops it and replaces with a filtered
-- unique index that only applies to 'active' codes.
--
-- IMPORTANT: Before creating the index, we must clean up any duplicate
-- active codes (keep only the most recent per phone). Without this,
-- CREATE UNIQUE INDEX fails with "duplicate key found".
-- ============================================================

-- Step 1: Drop the problematic constraint
IF EXISTS (SELECT * FROM sys.tables t
           JOIN sys.key_constraints k ON k.parent_object_id = t.object_id
           WHERE t.name = 'otp_codes' AND k.name = 'UQ_otp_codes_phone_active')
BEGIN
    ALTER TABLE otp_codes DROP CONSTRAINT UQ_otp_codes_phone_active;
END;
GO

-- Step 2: Clean up duplicate active codes — keep only the most recent per phone
-- (Expire all active codes except the one with the highest ID per phone)
UPDATE otp_codes
SET status = 'expired'
WHERE status = 'active'
  AND id NOT IN (
      SELECT MAX(id) FROM otp_codes
      WHERE status = 'active'
      GROUP BY phone
  );
GO

-- Step 3: Drop the index if it already exists (from a previous partial run)
IF EXISTS (SELECT * FROM sys.indexes WHERE name = 'UQ_otp_codes_phone_active' AND object_id = OBJECT_ID('otp_codes'))
BEGIN
    DROP INDEX UQ_otp_codes_phone_active ON otp_codes;
END;
GO

-- Step 4: Create the filtered unique index
CREATE UNIQUE INDEX UQ_otp_codes_phone_active
ON otp_codes (phone)
WHERE status = 'active';
GO
