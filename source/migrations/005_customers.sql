-- ============================================================
-- Customers Table (Phase 5+ — Customer Profile)
-- Central customer registry. Previously customer info was stored
-- only inside loan_applications (duplicated per application).
-- This table gives each customer a single profile.
-- ============================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'customers')
BEGIN
    CREATE TABLE customers (
        id INT IDENTITY(1,1) PRIMARY KEY,
        customer_pin VARCHAR(20) NOT NULL UNIQUE,   -- FIN code, unique per customer
        full_name NVARCHAR(100) NOT NULL,
        phone VARCHAR(20),                          -- primary phone (verified via OTP)
        email NVARCHAR(100),
        actual_address NVARCHAR(500),
        created_at DATETIME NOT NULL DEFAULT GETDATE(),
        updated_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- Index for fast PIN lookup (already covered by UNIQUE, but explicit for clarity)
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_customers_pin' AND object_id = OBJECT_ID('customers'))
BEGIN
    CREATE INDEX IX_customers_pin ON customers (customer_pin);
END;
GO

-- Add customer_id FK to loan_applications (nullable for backward compat)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'customer_id')
BEGIN
    ALTER TABLE loan_applications ADD customer_id INT NULL;
END;
GO

-- Add FK constraint (if not exists)
IF NOT EXISTS (SELECT * FROM sys.foreign_keys WHERE name = 'FK_loan_applications_customers')
BEGIN
    ALTER TABLE loan_applications
    ADD CONSTRAINT FK_loan_applications_customers
    FOREIGN KEY (customer_id) REFERENCES customers(id);
END;
GO
