-- 044_actual_address_audit.sql
-- PR #249: Faktiki ünvan (actual_address) dəyişikliklərinin audit izi.
-- PR #245 ekspert redaktəsi üçün UpdateAddress endpoint əlavə etdi, amma
-- kimin dəyişdirdiyi DB-də qeyd olunmurdu (yalnız slog.Info log var idi).
-- PR #148-dəki contacts_updated_by_* pattern tətbiq olunur:
--   actual_address_updated_by_user_id  — dashboard user ID
--   actual_address_updated_by_username — dashboard username
--   actual_address_updated_at          — son dəyişiklik tarixi

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'actual_address_updated_by_user_id')
BEGIN
    ALTER TABLE loan_applications ADD actual_address_updated_by_user_id INT NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'actual_address_updated_by_username')
BEGIN
    ALTER TABLE loan_applications ADD actual_address_updated_by_username NVARCHAR(50) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'actual_address_updated_at')
BEGIN
    ALTER TABLE loan_applications ADD actual_address_updated_at DATETIME NULL;
END
GO
