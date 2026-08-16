-- ============================================================
-- Update SMS sender_id: AZMK → AZMK BOKT (PR #220)
-- ============================================================
-- The registered Softline sender name is "AZMK BOKT".
-- Applies to the active softline provider row — all outgoing SMS
-- (OTP codes, approval/discount SMS, MyGov link SMS, rejection SMS)
-- use this sender via DynamicSMSProvider.
--
-- Idempotent: the migration runner re-executes all files on every
-- startup, so this UPDATE simply re-sets the same value.
-- ============================================================

UPDATE sms_providers
SET sender_id = 'AZMK BOKT'
WHERE provider_code = 'softline';
GO
