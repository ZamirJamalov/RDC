-- 030_auth_users.sql
-- PR #142: Authentication system — users and sessions tables for the RDC dashboard.
-- Access to index.html, detail.html, and all /api/expert/*, /api/admin/* endpoints
-- requires a valid session token (cookie or Bearer header).

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'users')
BEGIN
    CREATE TABLE users (
        id                    INT IDENTITY(1,1) PRIMARY KEY,
        username              NVARCHAR(50)  NOT NULL UNIQUE,
        password_hash         NVARCHAR(500) NOT NULL,           -- bcrypt hash
        role                  VARCHAR(20)   NOT NULL DEFAULT 'expert',  -- 'admin' or 'expert'
        is_active             BIT           NOT NULL DEFAULT 1,  -- soft disable (admin can deactivate)
        is_locked             BIT           NOT NULL DEFAULT 0,  -- auto-locked after too many failed attempts
        locked_until          DATETIME      NULL,
        failed_login_attempts INT           NOT NULL DEFAULT 0,
        last_login_at         DATETIME      NULL,
        created_at            DATETIME      NOT NULL DEFAULT GETDATE(),
        updated_at            DATETIME      NOT NULL DEFAULT GETDATE()
    );
END
GO

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'sessions')
BEGIN
    CREATE TABLE sessions (
        id          INT IDENTITY(1,1) PRIMARY KEY,
        user_id     INT NOT NULL FOREIGN KEY REFERENCES users(id),
        token       VARCHAR(64) NOT NULL UNIQUE,              -- uuid hex string
        expires_at  DATETIME    NOT NULL,
        ip_address  VARCHAR(45) NULL,
        user_agent  NVARCHAR(500) NULL,
        created_at  DATETIME    NOT NULL DEFAULT GETDATE()
    );
END
GO

-- Index for fast token lookup (validated on every authenticated request)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_sessions_token' AND object_id = OBJECT_ID('sessions'))
BEGIN
    CREATE UNIQUE INDEX IX_sessions_token ON sessions(token);
END
GO

-- Index for fast user_id lookup (list active sessions per user)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_sessions_user_id' AND object_id = OBJECT_ID('sessions'))
BEGIN
    CREATE INDEX IX_sessions_user_id ON sessions(user_id);
END
GO
