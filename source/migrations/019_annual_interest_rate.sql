-- 019_annual_interest_rate.sql
-- PR #78: Add annual_interest_rate column to credit_levels.
--
-- The existing 'rate' column is actually the COMMISSION rate, not the interest rate.
-- This migration adds the real annual interest rate per level:
--   NEW:      55%
--   TRUSTED:  52%
--   VALUABLE: 48%
--   ELITE:    45%
--
-- Interest amount = principal × annual_interest_rate × (term_months / 12)
-- Commission amount = principal × (rate / (100 - rate)) × 100

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('credit_levels') AND name = 'annual_interest_rate')
BEGIN
    ALTER TABLE credit_levels ADD annual_interest_rate DECIMAL(5,2) NULL
END
GO

-- Seed: set annual interest rate per level
UPDATE credit_levels SET annual_interest_rate = 55.00 WHERE level_name = 'new' AND annual_interest_rate IS NULL;
UPDATE credit_levels SET annual_interest_rate = 52.00 WHERE level_name = 'trusted' AND annual_interest_rate IS NULL;
UPDATE credit_levels SET annual_interest_rate = 48.00 WHERE level_name = 'valuable' AND annual_interest_rate IS NULL;
UPDATE credit_levels SET annual_interest_rate = 45.00 WHERE level_name = 'elite' AND annual_interest_rate IS NULL;
GO
