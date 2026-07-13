-- ============================================================
-- Fix Softline provider URL and sender_id
-- ============================================================
-- Migration 011 had the wrong URL (gw.softline.az → gw.soft-line.az)
-- and wrong sender_id (SOFTLINE → AZMK).
-- ============================================================

UPDATE sms_providers
SET base_url = 'http://gw.soft-line.az/sendsms',
    sender_id = 'AZMK'
WHERE provider_code = 'softline';
GO
