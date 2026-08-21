-- 048_contact_call_notes.sql
-- PR #266: Ekspertin hər kontakt nömrəsi ilə danışığı zamanı qeydləri saxlamaq üçün.
-- Hər kontakt üçün ayrı call_note sahəsi — ekspert zəng zamanı aldıqı məlumatları yazır.
-- Frontend blur olanda (focus itəndə) avtomatik save olunur.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact1_call_note')
BEGIN
    ALTER TABLE loan_applications ADD contact1_call_note NVARCHAR(1000) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact2_call_note')
BEGIN
    ALTER TABLE loan_applications ADD contact2_call_note NVARCHAR(1000) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contact3_call_note')
BEGIN
    ALTER TABLE loan_applications ADD contact3_call_note NVARCHAR(1000) NULL;
END
GO
