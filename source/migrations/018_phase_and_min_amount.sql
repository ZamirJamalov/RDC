-- 018_phase_and_min_amount.sql
-- PR #76: Two changes to credit_levels seed data:
--
-- 1. Change min_amount from 100 to 50 for all level ranges that start at 100.
--    Business requirement: minimum loan amount is 50 AZN, not 100 AZN.
--    This affects the first range of every level (NEW, TRUSTED, VALUABLE, ELITE).
--
-- 2. Change NEW level's second range (301-500) from unlock_phase=1 to unlock_phase=2.
--    Business requirement: at NEW level, first application should only offer 50-300 AZN.
--    Second application (after 1 approved loan at NEW) unlocks 301-500 AZN.
--    This matches the "addım" (step) logic described by business:
--      Step 1: 50-300 AZN (phase 1)
--      Step 2: 301-500 AZN (phase 2, requires 1+ approved loan at this level)

-- 1. Update min_amount from 100 to 50 for all ranges starting at 100
UPDATE credit_levels SET min_amount = 50 WHERE min_amount = 100 AND is_active = 1;
GO

-- 2. Update NEW level's 301-500 range to unlock_phase = 2
UPDATE credit_levels SET unlock_phase = 2 WHERE level_name = 'new' AND min_amount = 301 AND max_amount = 500;
GO
