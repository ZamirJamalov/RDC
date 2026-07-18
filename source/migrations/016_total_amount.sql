-- ============================================================
-- Total Amount (Principal + Interest) for LW submission
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'total_amount')
BEGIN
    ALTER TABLE loan_applications ADD total_amount DECIMAL(18,2) NULL
END;
GO
