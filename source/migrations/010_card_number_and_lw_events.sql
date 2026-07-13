-- ============================================================
-- Card Number + LW Loan Events (Phase 5+)
-- ============================================================

-- 1. Add card_number to loan_applications (required for approval)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'card_number')
BEGIN
    ALTER TABLE loan_applications ADD card_number VARCHAR(16) NOT NULL DEFAULT ''
END;
GO

-- Remove the default so it becomes truly required
IF EXISTS (SELECT * FROM sys.default_constraints WHERE name = 'DF__loan_appl__card___3C69FB99' AND parent_object_id = OBJECT_ID('loan_applications'))
BEGIN
    ALTER TABLE loan_applications DROP CONSTRAINT DF__loan_appl__card___3C69FB99
END;
GO

-- 2. LW Loan Events table (tracks contract signing + money transfer)
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'lw_loan_events')
BEGIN
    CREATE TABLE lw_loan_events (
        id INT IDENTITY(1,1) PRIMARY KEY,
        application_id INT NOT NULL,
        event_status VARCHAR(30) NOT NULL,  -- pending, contract_signed, transfer_completed, failed
        lms_loan_id VARCHAR(50),
        detail NVARCHAR(500),
        event_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_lw_loan_events_app_id' AND object_id = OBJECT_ID('lw_loan_events'))
BEGIN
    CREATE INDEX IX_lw_loan_events_app_id ON lw_loan_events (application_id);
END;
GO
