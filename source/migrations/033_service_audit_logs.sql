-- 033_service_audit_logs.sql
-- PR #163: Bütün xarici servis çağırışlarının audit log-u.
-- Hər application üçün hansı servislərə hansı URL, request və response gedib.

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'service_audit_logs')
BEGIN
    CREATE TABLE service_audit_logs (
        id              INT IDENTITY(1,1) PRIMARY KEY,
        application_id  INT NULL,
        service_name    NVARCHAR(100) NOT NULL,   -- 'AZMK_KYC', 'AZMK_KYC_VERIFY', 'AZMK_PARTNER', 'AZMK_CARD', 'AZMK_CUSTOMER_DATA', 'AZMK_MKR_SCORE', 'AZMK_OWNER_DATA'
        method          NVARCHAR(10)  NOT NULL,   -- 'POST', 'GET', 'PUT'
        url             NVARCHAR(500) NOT NULL,   -- tam URL
        request_body    NVARCHAR(MAX) NULL,       -- JSON request body
        response_body   NVARCHAR(MAX) NULL,       -- JSON response body
        status_code     INT NULL,                 -- HTTP status code
        duration_ms     INT NULL,                 -- sorğunun müddəti (ms)
        error           NVARCHAR(500) NULL,       -- xəta mesajı (varsa)
        created_at      DATETIME NOT NULL DEFAULT GETDATE(),
        created_by_user_id INT NULL               -- hansı istifadəçi (dashboard əməliyyatları üçün)
    );
END
GO

-- Index for fast application_id lookup
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_service_audit_logs_app_id' AND object_id = OBJECT_ID('service_audit_logs'))
BEGIN
    CREATE INDEX IX_service_audit_logs_app_id ON service_audit_logs(application_id);
END
GO

-- Index for fast service_name lookup
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_service_audit_logs_service' AND object_id = OBJECT_ID('service_audit_logs'))
BEGIN
    CREATE INDEX IX_service_audit_logs_service ON service_audit_logs(service_name);
END
GO
