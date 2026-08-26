-- 051_azmk_sign_columns.sql
-- PR #312: AZMK imza gözləmə axını (pending_signature → disbursed).
--
-- Ekspert approve edib AZMK /application/create uğurlu olanda müraciət
-- pending_signature statusuna keçir. Müştəri müqaviləni imzalayana qədər
-- background worker GET /application/{id}/status sorğusunu periodik göndərir.
--
--   azmk_loan_id     — status cavabından gələn AZMK kredit hesab nömrəsi
--                      (məs. "HO0030210") — audit üçün saxlanılır
--   azmk_created_at  — AZMK application-in yaradılma anı (UTC, GETUTCDATE()).
--                      Müştərinin imzalama limiti (3 saat) bundan hesablanır:
--                      vaxt bitərsə worker müraciəti avtomatik rejected edir.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'azmk_loan_id')
BEGIN
    ALTER TABLE loan_applications ADD azmk_loan_id VARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'azmk_created_at')
BEGIN
    ALTER TABLE loan_applications ADD azmk_created_at DATETIME2 NULL;
END
GO
