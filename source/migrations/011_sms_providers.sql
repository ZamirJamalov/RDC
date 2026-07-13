-- ============================================================
-- SMS Providers Table (Dynamic provider configuration)
-- ============================================================
-- Stores SMS gateway credentials in the database so they can be
-- switched at runtime without restarting the server.
-- The OTPService reads the active provider (is_active=1) and caches
-- it for 1 minute.
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'sms_providers')
BEGIN
    CREATE TABLE sms_providers (
        id INT IDENTITY(1,1) PRIMARY KEY,
        provider_code NVARCHAR(50) NOT NULL UNIQUE,
        base_url NVARCHAR(255) NOT NULL,
        app_key NVARCHAR(255) NOT NULL,         -- API key (used as password)
        username NVARCHAR(100) NOT NULL,
        password NVARCHAR(255) NOT NULL,         -- same as app_key for some providers
        sender_id NVARCHAR(50) NOT NULL,
        timeout_seconds INT DEFAULT 10,
        is_active BIT DEFAULT 0,
        created_at DATETIME DEFAULT GETDATE()
    );
END;
GO

-- Seed data: Softline provider (active by default for production)
IF NOT EXISTS (SELECT 1 FROM sms_providers WHERE provider_code = 'softline')
BEGIN
    INSERT INTO sms_providers (provider_code, base_url, app_key, username, password, sender_id, timeout_seconds, is_active)
    VALUES (
        'softline',
        'http://gw.soft-line.az/sendsms',
        'ZXe5Gk1G11',
        'softlinetestapi',
        'ZXe5Gk1G11',
        'AZMK',
        10,
        1
    );
END;
GO

-- Seed data: Mock provider (for dev/test, inactive by default)
IF NOT EXISTS (SELECT 1 FROM sms_providers WHERE provider_code = 'mock')
BEGIN
    INSERT INTO sms_providers (provider_code, base_url, app_key, username, password, sender_id, timeout_seconds, is_active)
    VALUES (
        'mock',
        'http://localhost:9999/sendsms',
        'mock-key',
        'mock-user',
        'mock-pass',
        'RDC',
        5,
        0
    );
END;
GO
