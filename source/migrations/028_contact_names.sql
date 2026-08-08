-- 028_contact_names.sql
-- PR #128: Kontakt şəxslərinin ad soyadları.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact1_name')
BEGIN
    ALTER TABLE loan_applications ADD contact1_name NVARCHAR(100) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact2_name')
BEGIN
    ALTER TABLE loan_applications ADD contact2_name NVARCHAR(100) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact3_name')
BEGIN
    ALTER TABLE loan_applications ADD contact3_name NVARCHAR(100) NULL;
END
GO
