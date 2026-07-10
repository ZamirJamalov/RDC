-- ============================================================
-- Application Extra Fields (Phase 5, T-5.4)
-- Adds official_income, contact phones, actual_address columns
-- to loan_applications for ASAN Finance + contacts + address checks.
-- ============================================================

-- Official income from ASAN Finance (T-5.1)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'official_income')
BEGIN
    ALTER TABLE loan_applications ADD official_income DECIMAL(18,2) NULL
END;
GO

-- 3 contact phone numbers (T-5.5)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact1_phone')
BEGIN
    ALTER TABLE loan_applications ADD contact1_phone VARCHAR(20) NULL
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact2_phone')
BEGIN
    ALTER TABLE loan_applications ADD contact2_phone VARCHAR(20) NULL
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact3_phone')
BEGIN
    ALTER TABLE loan_applications ADD contact3_phone VARCHAR(20) NULL
END;
GO

-- Actual (factual) address (T-5.6)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'actual_address')
BEGIN
    ALTER TABLE loan_applications ADD actual_address NVARCHAR(500) NULL
END;
GO
