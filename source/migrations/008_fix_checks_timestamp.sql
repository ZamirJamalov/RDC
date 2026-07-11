-- ============================================================
-- Fix application_checks.checked_at column type
-- ============================================================
-- The checked_at column was DATETIME, but the Go code writes
-- time.Now().Format(time.RFC3339) which produces ISO 8601 format
-- (e.g. "2026-07-11T13:43:18.047Z"). SQL Server's DATETIME cannot
-- parse the "Z" suffix, causing:
--   "Conversion failed when converting date and/or time from character string"
--
-- Fix: change the column to NVARCHAR(50) so it stores the formatted
-- string as-is. This avoids all timezone/format issues.
-- ============================================================

-- Drop the default constraint first (it references GETDATE() which is DATETIME)
IF EXISTS (SELECT * FROM sys.default_constraints WHERE name = 'DF_application_checks_checked_at')
BEGIN
    ALTER TABLE application_checks DROP CONSTRAINT DF_application_checks_checked_at;
END;
GO

-- Change column type from DATETIME to NVARCHAR(50)
IF EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('application_checks') AND name = 'checked_at')
BEGIN
    ALTER TABLE application_checks ALTER COLUMN checked_at NVARCHAR(50);
END;
GO
