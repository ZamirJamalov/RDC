-- ============================================================
-- Customer Serial + Init Flow
-- ============================================================

-- 1. Add customer_serial to loan_applications
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'customer_serial')
BEGIN
    ALTER TABLE loan_applications ADD customer_serial VARCHAR(50)
END;
GO
