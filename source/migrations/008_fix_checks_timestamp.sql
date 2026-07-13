-- ============================================================
-- Fix application_checks.checked_at column type (DYNAMIC SQL)
-- ============================================================
-- The checked_at column was DATETIME, but the Go code writes
-- time.Now().Format(time.RFC3339) which produces ISO 8601 format
-- (e.g. "2026-07-11T13:43:18.047Z"). SQL Server's DATETIME cannot
-- parse the "Z" suffix, causing:
--   "Conversion failed when converting date and/or time from character string"
--
-- Fix: change the column to NVARCHAR(50) so it stores the formatted
-- string as-is. This avoids all timezone/format issues.
--
-- IMPORTANT: Migration 001 created the column with `DEFAULT GETDATE()`
-- which auto-generates a constraint name (NOT DF_application_checks_checked_at).
-- We use dynamic SQL to find and drop any such constraint before ALTER COLUMN.
-- ============================================================

-- Step 1: Drop any default constraint on checked_at (name is auto-generated)
DECLARE @constraint_name NVARCHAR(256)
SELECT @constraint_name = name
FROM sys.default_constraints
WHERE parent_object_id = OBJECT_ID('application_checks')
  AND parent_column_id = (
      SELECT column_id FROM sys.columns
      WHERE object_id = OBJECT_ID('application_checks') AND name = 'checked_at'
  )

IF @constraint_name IS NOT NULL
BEGIN
    EXEC('ALTER TABLE application_checks DROP CONSTRAINT ' + @constraint_name)
END
GO

-- Step 2: Drop any indexes that include checked_at (if any)
IF EXISTS (SELECT 1 FROM sys.indexes i
           JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
           JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
           WHERE i.object_id = OBJECT_ID('application_checks') AND c.name = 'checked_at'
           AND i.is_primary_key = 0 AND i.is_unique_constraint = 0)
BEGIN
    DECLARE @index_name NVARCHAR(256)
    DECLARE drop_index_cursor CURSOR FOR
        SELECT i.name
        FROM sys.indexes i
        JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
        JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
        WHERE i.object_id = OBJECT_ID('application_checks') AND c.name = 'checked_at'
        AND i.is_primary_key = 0 AND i.is_unique_constraint = 0

    OPEN drop_index_cursor
    FETCH NEXT FROM drop_index_cursor INTO @index_name
    WHILE @@FETCH_STATUS = 0
    BEGIN
        EXEC('DROP INDEX ' + @index_name + ' ON application_checks')
        FETCH NEXT FROM drop_index_cursor INTO @index_name
    END
    CLOSE drop_index_cursor
    DEALLOCATE drop_index_cursor
END
GO

-- Step 3: Now alter the column type (idempotent — skip if already NVARCHAR)
IF EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID('application_checks')
      AND name = 'checked_at'
      AND system_type_id <> TYPE_ID('nvarchar')
)
BEGIN
    ALTER TABLE application_checks ALTER COLUMN checked_at NVARCHAR(50);
END;
GO
