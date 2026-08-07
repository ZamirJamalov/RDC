-- 026_azmk_online_lending.sql
-- PR #116: AZMK Online Lending Service inteqrasiyası üçün DB sütunları.
--
-- AZMK Online Lending axını:
--   1. KYC (OTP-dən sonra) → kyc_id
--   2. Partner registration (KYC-dən sonra) → partner_id
--   3. Card registration (customer-confirm-da) → card_id
--   4. Application create (ekspert approve-da) → lw_application_id
--
-- Bu sütunlar loan_applications cədvəlinə əlavə olunur və AZMK
-- servisindən qaytarılan ID-ləri saxlayır.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'kyc_id')
BEGIN
    ALTER TABLE loan_applications ADD kyc_id VARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'partner_id')
BEGIN
    ALTER TABLE loan_applications ADD partner_id VARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'card_id')
BEGIN
    ALTER TABLE loan_applications ADD card_id VARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'lw_application_id')
BEGIN
    ALTER TABLE loan_applications ADD lw_application_id VARCHAR(50) NULL;
END
GO
