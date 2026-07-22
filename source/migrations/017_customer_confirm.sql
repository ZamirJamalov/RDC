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

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'customer_confirmed_at')
BEGIN
    ALTER TABLE loan_applications ADD customer_confirmed_at DATETIME NULL
END
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'card_ownership_confirmed')
BEGIN
    ALTER TABLE loan_applications ADD card_ownership_confirmed BIT DEFAULT 0
END
GO
