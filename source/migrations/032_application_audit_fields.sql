-- 032_application_audit_fields.sql
-- PR #148: Application audit fields — track which expert performed each operation.
-- processed_by_user_id/username (PR #142) yalnız approve/reject üçün idi.
-- Bu migration daha geniş audit saxlayır:
--   contacts_updated_by — kontakt nömrələrini kim dəyişdi
--   timer_updated_by — timer-ı kim saxladı
--   mygov_checked_by — MyGov yoxlamasını kim etdi
--   compliance_checked_by — komplyans yoxlamasını kim etdi

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contacts_updated_by_user_id')
BEGIN
    ALTER TABLE loan_applications ADD contacts_updated_by_user_id INT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contacts_updated_by_username')
BEGIN
    ALTER TABLE loan_applications ADD contacts_updated_by_username NVARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'contacts_updated_at')
BEGIN
    ALTER TABLE loan_applications ADD contacts_updated_at DATETIME NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'timer_updated_by_user_id')
BEGIN
    ALTER TABLE loan_applications ADD timer_updated_by_user_id INT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'timer_updated_by_username')
BEGIN
    ALTER TABLE loan_applications ADD timer_updated_by_username NVARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'mygov_checked_by_user_id')
BEGIN
    ALTER TABLE loan_applications ADD mygov_checked_by_user_id INT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'mygov_checked_by_username')
BEGIN
    ALTER TABLE loan_applications ADD mygov_checked_by_username NVARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'mygov_checked_at')
BEGIN
    ALTER TABLE loan_applications ADD mygov_checked_at DATETIME NULL;
END
GO
