-- 021_rename_rate_to_commission.sql
-- PR #86: Rename 'rate' column to 'commission' and reseed with correct values.
--
-- The 'rate' column was actually the commission rate, not interest rate.
-- This migration:
--   1. Renames rate → commission
--   2. Deletes all existing seed data
--   3. Inserts correct commission values per business spec
--
-- Phase logic (PR #76):
--   Phase 1 = first application at this level
--   Phase 2 = after 1+ approved loan at this level
--
-- Annual interest rates (PR #78, migration 019):
--   NEW=55, TRUSTED=52, VALUABLE=48, ELITE=45

-- 1. Rename column
IF EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('credit_levels') AND name = 'rate')
BEGIN
    EXEC sp_rename 'credit_levels.rate', 'commission', 'COLUMN';
END
GO

-- 2. Delete old seed data
DELETE FROM credit_levels;
GO

-- 3. Insert new data with correct commission values
-- NEW level (55% annual interest)
INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, commission, unlock_phase, is_active, annual_interest_rate) VALUES
('new', 50, 300, 3, 14.00, 1, 1, 55.00),
('new', 301, 500, 3, 14.00, 2, 1, 55.00);

-- TRUSTED level (52% annual interest)
INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, commission, unlock_phase, is_active, annual_interest_rate) VALUES
('trusted', 50, 300, 3, 13.00, 1, 1, 52.00),
('trusted', 301, 500, 3, 13.00, 1, 1, 52.00),
('trusted', 501, 700, 3, 16.00, 1, 1, 52.00),
('trusted', 701, 900, 3, 15.00, 2, 1, 52.00),
('trusted', 501, 700, 6, 17.00, 1, 1, 52.00),
('trusted', 701, 900, 6, 16.00, 2, 1, 52.00);

-- VALUABLE level (48% annual interest)
INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, commission, unlock_phase, is_active, annual_interest_rate) VALUES
('valuable', 50, 300, 3, 11.00, 1, 1, 48.00),
('valuable', 301, 500, 3, 11.00, 1, 1, 48.00),
('valuable', 501, 700, 3, 14.00, 1, 1, 48.00),
('valuable', 701, 900, 3, 13.00, 1, 1, 48.00),
('valuable', 901, 1100, 3, 15.00, 1, 1, 48.00),
('valuable', 1101, 1300, 3, 14.00, 2, 1, 48.00),
('valuable', 501, 700, 6, 15.00, 1, 1, 48.00),
('valuable', 701, 900, 6, 14.00, 1, 1, 48.00),
('valuable', 901, 1100, 6, 16.00, 1, 1, 48.00),
('valuable', 1101, 1300, 6, 15.00, 2, 1, 48.00),
('valuable', 901, 1100, 9, 17.00, 1, 1, 48.00),
('valuable', 1101, 1300, 9, 16.00, 2, 1, 48.00);

-- ELITE level (45% annual interest)
INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, commission, unlock_phase, is_active, annual_interest_rate) VALUES
('elite', 50, 300, 3, 9.00, 1, 1, 45.00),
('elite', 301, 500, 3, 9.00, 1, 1, 45.00),
('elite', 501, 700, 3, 12.00, 1, 1, 45.00),
('elite', 701, 900, 3, 11.00, 1, 1, 45.00),
('elite', 901, 1100, 3, 13.00, 1, 1, 45.00),
('elite', 1101, 1300, 3, 12.00, 1, 1, 45.00),
('elite', 501, 700, 6, 13.00, 1, 1, 45.00),
('elite', 701, 900, 6, 12.00, 1, 1, 45.00),
('elite', 901, 1100, 6, 14.00, 1, 1, 45.00),
('elite', 1101, 1300, 6, 13.00, 1, 1, 45.00),
('elite', 1301, 1500, 6, 11.00, 1, 1, 45.00),
('elite', 1501, 2000, 6, 10.00, 2, 1, 45.00),
('elite', 2001, 2500, 6, 9.00, 2, 1, 45.00),
('elite', 901, 1100, 9, 15.00, 1, 1, 45.00),
('elite', 1101, 1300, 9, 14.00, 1, 1, 45.00),
('elite', 1301, 1500, 9, 12.00, 1, 1, 45.00),
('elite', 1501, 2000, 9, 11.00, 2, 1, 45.00),
('elite', 2001, 2500, 9, 10.00, 2, 1, 45.00),
('elite', 2501, 3000, 9, 9.00, 2, 1, 45.00),
('elite', 1301, 1500, 12, 13.00, 1, 1, 45.00),
('elite', 1501, 2000, 12, 12.00, 2, 1, 45.00),
('elite', 2001, 2500, 12, 11.00, 2, 1, 45.00),
('elite', 2501, 3000, 12, 10.00, 2, 1, 45.00);
GO
