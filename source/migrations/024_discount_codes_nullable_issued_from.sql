-- 024_discount_codes_nullable_issued_from.sql
-- PR #97: Make issued_from_application_id nullable so discount codes can be
-- created manually (by admin / seed scripts) without requiring an existing
-- loan application.
--
-- Background: PR #94 created the column as NOT NULL because every code was
-- expected to come from an approval. PR #97 adds the ability to seed test
-- codes and create admin-issued codes, which don't have a source application.

IF EXISTS (SELECT 1 FROM sys.columns
           WHERE object_id = OBJECT_ID('discount_codes')
             AND name = 'issued_from_application_id'
             AND is_nullable = 0)
BEGIN
    ALTER TABLE discount_codes ALTER COLUMN issued_from_application_id INT NULL;
END
GO
