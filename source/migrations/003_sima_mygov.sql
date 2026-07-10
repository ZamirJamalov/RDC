-- ============================================================
-- SIMA KYC Sessions Table (Phase 4, T-4.3)
-- Stores SIMA KYC sessions for identity verification.
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'sima_sessions')
BEGIN
    CREATE TABLE sima_sessions (
        id INT IDENTITY(1,1) PRIMARY KEY,
        application_id INT NOT NULL,
        session_id VARCHAR(100) NOT NULL,
        fin VARCHAR(20) NOT NULL,
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        detail NVARCHAR(500),
        url NVARCHAR(500),
        started_at DATETIME NOT NULL DEFAULT GETDATE(),
        completed_at DATETIME NULL,
        expires_at DATETIME NOT NULL,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- Index for fast lookup by session_id (callback processing)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_sima_sessions_session_id' AND object_id = OBJECT_ID('sima_sessions'))
BEGIN
    CREATE UNIQUE INDEX IX_sima_sessions_session_id ON sima_sessions (session_id);
END;
GO

-- Index for lookup by application_id (pipeline checks)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_sima_sessions_application_id' AND object_id = OBJECT_ID('sima_sessions'))
BEGIN
    CREATE INDEX IX_sima_sessions_application_id ON sima_sessions (application_id);
END;
GO

-- ============================================================
-- MyGov Permissions Table (Phase 4, T-4.9)
-- Stores MyGov data access permission tokens for customers.
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'mygov_permissions')
BEGIN
    CREATE TABLE mygov_permissions (
        id INT IDENTITY(1,1) PRIMARY KEY,
        application_id INT NOT NULL,
        customer_pin VARCHAR(20) NOT NULL,
        permission_token VARCHAR(256) NOT NULL,
        link_url NVARCHAR(500),
        link_expires_at DATETIME NOT NULL,
        data_fetched_at DATETIME NULL,
        data_json NVARCHAR(MAX),
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- Index for lookup by application_id
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_mygov_permissions_application_id' AND object_id = OBJECT_ID('mygov_permissions'))
BEGIN
    CREATE INDEX IX_mygov_permissions_application_id ON mygov_permissions (application_id);
END;
GO

-- Index for lookup by permission_token (callback)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_mygov_permissions_token' AND object_id = OBJECT_ID('mygov_permissions'))
BEGIN
    CREATE INDEX IX_mygov_permissions_token ON mygov_permissions (permission_token);
END;
GO
