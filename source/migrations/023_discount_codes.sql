-- 023_discount_codes.sql
-- PR #94: Discount / referral code infrastructure (Migration + Model + Repository layer).
--
-- Business logic (see Docs/PR93_Discount_Code_Plan.md):
--   1. Customer A's loan is approved → system generates unique code ALPUL-XXXXXX
--   2. SMS sent to A: "Kreditiniz tesdiq edildi! Endirim kodunuz: ALPUL-XXXXXX"
--   3. Customer B (different PIN) enters A's code in apply.html (optional field)
--   4. Backend validates: code exists, belongs to other customer, status='active'
--   5. On B's approval: discount applied to commission, code marked as 'used'
--   6. B receives their own code + SMS → referral chain continues
--
-- Self-use prevention: A cannot use own code (issued_to_customer_id != current customer)
-- Single-use: each code can only be redeemed once
-- External control: discount_type ('percent'|'fixed') + discount_value stored per-code

-- =========================================================
-- 1. discount_codes table
-- =========================================================
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'discount_codes')
BEGIN
    CREATE TABLE discount_codes (
        id                          INT IDENTITY(1,1) PRIMARY KEY,
        code                        VARCHAR(20)   NOT NULL UNIQUE,        -- ALPUL-AB12CD
        issued_to_customer_id       INT           NOT NULL,               -- FK customers.id (owner)
        issued_from_application_id  INT           NOT NULL,               -- FK loan_applications.id (which approval generated it)
        discount_type               VARCHAR(10)   NOT NULL DEFAULT 'percent',  -- 'percent' | 'fixed'
        discount_value              DECIMAL(10,2) NOT NULL DEFAULT 2.00,  -- percent: 2.00=2%; fixed: 5.00=5 AZN
        status                      VARCHAR(15)   NOT NULL DEFAULT 'active',  -- 'active' | 'used' | 'expired'
        used_by_application_id      INT           NULL,                   -- which application redeemed it
        used_at                     DATETIME      NULL,
        valid_until                 DATETIME      NULL,                   -- optional expiry
        created_at                  DATETIME      NOT NULL DEFAULT GETDATE(),

        CONSTRAINT FK_discount_codes_customer   FOREIGN KEY (issued_to_customer_id)      REFERENCES customers(id),
        CONSTRAINT FK_discount_codes_app_issued FOREIGN KEY (issued_from_application_id) REFERENCES loan_applications(id),
        CONSTRAINT FK_discount_codes_app_used   FOREIGN KEY (used_by_application_id)     REFERENCES loan_applications(id),

        CONSTRAINT CK_discount_type   CHECK (discount_type IN ('percent', 'fixed')),
        CONSTRAINT CK_discount_status CHECK (status IN ('active', 'used', 'expired')),
        CONSTRAINT CK_discount_value  CHECK (discount_value >= 0)
    );

    CREATE INDEX IX_discount_codes_code   ON discount_codes(code);
    CREATE INDEX IX_discount_codes_owner  ON discount_codes(issued_to_customer_id);
    CREATE INDEX IX_discount_codes_status ON discount_codes(status);
END
GO

-- =========================================================
-- 2. loan_applications: discount columns
-- =========================================================
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'discount_code')
BEGIN
    ALTER TABLE loan_applications ADD discount_code VARCHAR(20) NULL;
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'discount_amount')
BEGIN
    ALTER TABLE loan_applications ADD discount_amount DECIMAL(10,2) NULL;
END
GO
