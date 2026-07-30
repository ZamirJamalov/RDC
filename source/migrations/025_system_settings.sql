-- 025_system_settings.sql
-- PR #98: Generic key-value system settings table.
--
-- Used for feature flags and runtime configuration that can be toggled
-- without redeploying the application. Currently supports:
--   - discount_codes_enabled: 1=on, 0=off (turns off the entire discount
--     code feature with one command)
--
-- Query example:
--   SELECT value FROM system_settings WHERE key = 'discount_codes_enabled';

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'system_settings')
BEGIN
    CREATE TABLE system_settings (
        key         VARCHAR(50)  NOT NULL PRIMARY KEY,
        value       VARCHAR(255) NOT NULL,
        description NVARCHAR(500) NULL,
        updated_at  DATETIME     NOT NULL DEFAULT GETDATE()
    );
END
GO

-- Seed: discount_codes_enabled (default ON)
IF NOT EXISTS (SELECT 1 FROM system_settings WHERE key = 'discount_codes_enabled')
BEGIN
    INSERT INTO system_settings (key, value, description)
    VALUES (
        'discount_codes_enabled',
        '1',
        'Endirim kodu funksionalini yandır/söndür (1=on, 0=off). PR #98.'
    );
END
GO
