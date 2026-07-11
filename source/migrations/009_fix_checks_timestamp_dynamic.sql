-- ============================================================
-- Fix migration 008: dynamically drop constraint before ALTER COLUMN
-- ============================================================
-- PR #18 merged the first version of migration 008 which used a
-- hardcoded constraint name 'DF_application_checks_checked_at'.
-- But migration 001 used 'DEFAULT GETDATE()' without naming the
-- constraint — SQL Server auto-generates the name. The hardcoded
-- name didn't match, so the constraint wasn't dropped, and
-- ALTER COLUMN failed:
--   'ALTER TABLE ALTER COLUMN checked_at failed because one or more
--    objects access this column.'
--
-- This migration replaces the broken approach with dynamic SQL that:
-- 1. Finds the auto-generated constraint name via sys.default_constraints
-- 2. Drops it using EXEC
-- 3. Drops any indexes that include the column (cursor loop)
-- 4. Alters the column type to NVARCHAR(50)
--
-- This is a REPLACEMENT for the broken batch 2 in migration 008.
-- Batch 1 (drop constraint) silently failed because the name didn't
-- match, batch 2 (ALTER COLUMN) failed. This migration does both
-- correctly.
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
