-- 016_total_amount.sql
-- Store total repayment amount (principal + interest) sent to LW.
--
-- Formula: Total = Principal + (Rate / (100 - Rate)) × 100
-- Example: 300 + (30/70)*100 = 300 + 42.86 = 342.86 AZN

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'total_amount')
BEGIN
    ALTER TABLE loan_applications ADD total_amount DECIMAL(18,2) NULL
END
GO
