-- 031_application_processed_by.sql
-- PR #142: Track which dashboard user approved or rejected each application.
-- This ties dashboard operations (approve/reject) to the authenticated user.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'processed_by_user_id')
BEGIN
    ALTER TABLE loan_applications ADD processed_by_user_id INT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'processed_by_username')
BEGIN
    ALTER TABLE loan_applications ADD processed_by_username NVARCHAR(50) NULL;
END
GO
