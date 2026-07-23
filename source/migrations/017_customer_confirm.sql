-- 017_customer_confirm.sql
-- PR #58: Customer-side confirmation flow.
--
-- Adds two columns to track when the customer confirmed their application
-- on the public website (after selecting amount, entering card + address,
-- and ticking the card-ownership checkbox).
--
-- card_ownership_confirmed is stored for audit purposes — if a customer
-- later disputes the card used for disbursement, this flag proves they
-- explicitly confirmed ownership at submission time.
--
-- PR #69 FIX: customer_confirmed_at changed from DATETIME to NVARCHAR(50)
-- because Go's time.RFC3339 format ("2006-01-02T15:04:05Z07:00") is not
-- SQL Server DATETIME-compatible. Using NVARCHAR(50) matches the pattern
-- used for checked_at in application_checks (migration 005).

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'customer_confirmed_at')
BEGIN
    ALTER TABLE loan_applications ADD customer_confirmed_at NVARCHAR(50) NULL
END
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'card_ownership_confirmed')
BEGIN
    ALTER TABLE loan_applications ADD card_ownership_confirmed BIT DEFAULT 0
END
GO
