-- ============================================================
-- PR #245: Qeydiyyat ünvanı (RegistrationAddress)
-- AZMK GetPersonalInfo cavabından gələn qeydiyyat ünvanı
-- loan_applications cədvəlində saxlanılır (read-only, ekspert üçün).
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'registration_address')
BEGIN
    ALTER TABLE loan_applications ADD registration_address NVARCHAR(500) NULL
END;
GO
