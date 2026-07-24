-- 020_contact_relations.sql
-- PR #85: Add contact relation columns to loan_applications.
-- Stores the relationship type for each of the 3 contact phone numbers
-- (Ata, Ana, Qardaş, Bacı, Dost, Ər/Arvad, Oğul, Qız, Digər).

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact1_relation')
BEGIN
    ALTER TABLE loan_applications ADD contact1_relation NVARCHAR(20) NULL
END
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact2_relation')
BEGIN
    ALTER TABLE loan_applications ADD contact2_relation NVARCHAR(20) NULL
END
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact3_relation')
BEGIN
    ALTER TABLE loan_applications ADD contact3_relation NVARCHAR(20) NULL
END
GO
