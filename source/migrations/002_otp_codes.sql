-- ============================================================
-- OTP Codes Table (Phase 3, T-3.5)
-- Stores OTP codes for SMS verification during loan application.
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'otp_codes')
BEGIN
    CREATE TABLE otp_codes (
        id INT IDENTITY(1,1) PRIMARY KEY,
        phone VARCHAR(20) NOT NULL,
        code_hash VARCHAR(128) NOT NULL,          -- SHA-256 hash of the code (never store plaintext)
        status VARCHAR(20) NOT NULL DEFAULT 'active',  -- active, verified, expired, consumed
        attempts INT NOT NULL DEFAULT 0,           -- number of failed verification attempts
        max_attempts INT NOT NULL DEFAULT 5,       -- max attempts before the code is locked
        expires_at DATETIME NOT NULL,              -- when the code expires
        consumed_at DATETIME NULL,                 -- when the code was successfully verified
        created_at DATETIME NOT NULL DEFAULT GETDATE(),
        CONSTRAINT UQ_otp_codes_phone_active UNIQUE (phone, status)
    );
END;
GO

-- Index for fast lookup by phone number (rate limiting + verification)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_otp_codes_phone' AND object_id = OBJECT_ID('otp_codes'))
BEGIN
    CREATE INDEX IX_otp_codes_phone ON otp_codes (phone, created_at DESC);
END;
GO

-- Index for finding expired codes (cleanup job)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_otp_codes_expires_at' AND object_id = OBJECT_ID('otp_codes'))
BEGIN
    CREATE INDEX IX_otp_codes_expires_at ON otp_codes (expires_at) WHERE status = 'active';
END;
GO
