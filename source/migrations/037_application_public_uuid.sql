-- 037_application_public_uuid.sql
-- PR #191: loan_applications cədvəlinə public_id UNIQUEIDENTIFIER sütunu əlavə edir.
-- INT id daxili DB handle kimi qalır (FK-lər, joins), public_id isə xarici/UI istifadə üçündür.
-- UUID formatında olduğu üçün təxmin edilə bilməz və ardıcıl deyil.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'public_id')
BEGIN
    ALTER TABLE loan_applications ADD public_id UNIQUEIDENTIFIER NULL;
END
GO

-- Backfill existing rows with new UUIDs
UPDATE loan_applications SET public_id = NEWID() WHERE public_id IS NULL;
GO

-- Make it NOT NULL after backfill
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'public_id' AND is_nullable = 0)
BEGIN
    ALTER TABLE loan_applications ALTER COLUMN public_id UNIQUEIDENTIFIER NOT NULL;
END
GO

-- Default for new rows
IF NOT EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = 'DF_loan_applications_public_id')
BEGIN
    ALTER TABLE loan_applications ADD CONSTRAINT DF_loan_applications_public_id DEFAULT (NEWID()) FOR public_id;
END
GO

-- Unique index for lookups by public_id
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_loan_applications_public_id' AND object_id = OBJECT_ID('loan_applications'))
BEGIN
    CREATE UNIQUE INDEX IX_loan_applications_public_id ON loan_applications(public_id);
END
GO
