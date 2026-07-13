-- ============================================================
-- MyGov Deeplink + customer_phone
-- ============================================================

-- 1. Add customer_phone to loan_applications (OTP-verified phone)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'customer_phone')
BEGIN
    ALTER TABLE loan_applications ADD customer_phone VARCHAR(20)
END;
GO

-- 2. Add deeplink fields to mygov_permissions
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mygov_permissions') AND name = 'nonce')
BEGIN
    ALTER TABLE mygov_permissions ADD nonce NVARCHAR(100)
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mygov_permissions') AND name = 'state')
BEGIN
    ALTER TABLE mygov_permissions ADD state NVARCHAR(100)
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mygov_permissions') AND name = 'deeplink')
BEGIN
    ALTER TABLE mygov_permissions ADD deeplink NVARCHAR(1000)
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mygov_permissions') AND name = 'expires_at')
BEGIN
    ALTER TABLE mygov_permissions ADD expires_at DATETIME
END;
GO
