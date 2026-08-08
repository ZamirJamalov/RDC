-- 027_contact_verification.sql
-- PR #124: Kontakt nömrələrinin yoxlanma statusu.
--
-- Ekspert kontakt nömrələrinə zəng edib yoxlayır:
--   NULL = yoxlanılmayıb
--   1    = təsdiq olundu (nömrə doğrudur)
--   0    = imtina (nömrə yanlışdır)

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact1_verified')
BEGIN
    ALTER TABLE loan_applications ADD contact1_verified BIT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact2_verified')
BEGIN
    ALTER TABLE loan_applications ADD contact2_verified BIT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact3_verified')
BEGIN
    ALTER TABLE loan_applications ADD contact3_verified BIT NULL;
END
GO
