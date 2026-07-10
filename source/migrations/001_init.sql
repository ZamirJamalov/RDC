-- ============================================================
-- RDC Loan Application System — Initial Schema
-- Database: SQL Server Express
-- Idempotent: safe to run multiple times (uses IF NOT EXISTS / IF EXISTS guards)
-- ============================================================

-- 1. credit_levels (rate is stored as percentage, e.g. 30.00 for 30%)
-- unlock_phase: 1 = available from first loan at this level, 2 = available after 1+ approved loan at this level
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'credit_levels')
BEGIN
    CREATE TABLE credit_levels (
        id INT IDENTITY(1,1) PRIMARY KEY,
        level_name VARCHAR(20) NOT NULL,
        min_amount DECIMAL(18,2) NOT NULL DEFAULT 0,
        max_amount DECIMAL(18,2) NOT NULL DEFAULT 0,
        term_months INT NOT NULL,
        rate DECIMAL(5,2) NOT NULL,
        unlock_phase INT NOT NULL DEFAULT 1,
        is_active BIT NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- 2. credit_level_rules
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'credit_level_rules')
BEGIN
    CREATE TABLE credit_level_rules (
        id INT IDENTITY(1,1) PRIMARY KEY,
        from_level VARCHAR(20) NOT NULL,
        to_level VARCHAR(20) NOT NULL,
        min_completed_loans INT NOT NULL DEFAULT 0,
        all_on_time BIT NOT NULL DEFAULT 1,
        has_early_completion BIT NOT NULL DEFAULT 0,
        is_active BIT NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- 3. loan_applications
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'loan_applications')
BEGIN
    CREATE TABLE loan_applications (
        id INT IDENTITY(1,1) PRIMARY KEY,
        customer_pin VARCHAR(20) NOT NULL,
        customer_full_name NVARCHAR(100) NOT NULL,
        amount DECIMAL(18,2) NOT NULL,
        term_months INT NOT NULL,
        loan_purpose NVARCHAR(200),
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        credit_level VARCHAR(20),
        approved_amount DECIMAL(18,2),
        approved_rate DECIMAL(5,2),
        rejection_reason_id INT,
        rejection_reason NVARCHAR(500),
        akb_score INT,
        created_at DATETIME NOT NULL DEFAULT GETDATE(),
        updated_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- 4. application_checks
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'application_checks')
BEGIN
    CREATE TABLE application_checks (
        id INT IDENTITY(1,1) PRIMARY KEY,
        application_id INT NOT NULL FOREIGN KEY REFERENCES loan_applications(id),
        check_type VARCHAR(50) NOT NULL,
        status VARCHAR(20) NOT NULL DEFAULT 'pending',
        detail NVARCHAR(500),
        checked_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- 5. credit_level_history
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'credit_level_history')
BEGIN
    CREATE TABLE credit_level_history (
        id INT IDENTITY(1,1) PRIMARY KEY,
        customer_pin VARCHAR(20) NOT NULL,
        from_level VARCHAR(20),
        to_level VARCHAR(20) NOT NULL,
        reason NVARCHAR(200),
        application_id INT FOREIGN KEY REFERENCES loan_applications(id),
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- 6. rejection_reasons
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'rejection_reasons')
BEGIN
    CREATE TABLE rejection_reasons (
        id INT IDENTITY(1,1) PRIMARY KEY,
        reason_code VARCHAR(50) NOT NULL UNIQUE,
        reason_description NVARCHAR(500) NOT NULL,
        is_active BIT NOT NULL DEFAULT 1
    );
END;
GO

-- 7. check_type_config
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'check_type_config')
BEGIN
    CREATE TABLE check_type_config (
        id INT IDENTITY(1,1) PRIMARY KEY,
        check_type VARCHAR(50) NOT NULL UNIQUE,
        description NVARCHAR(200),
        is_active BIT NOT NULL DEFAULT 1,
        priority_order INT NOT NULL DEFAULT 0
    );
END;
GO

-- 8. mock_lms_loans
IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'mock_lms_loans')
BEGIN
    CREATE TABLE mock_lms_loans (
        id INT IDENTITY(1,1) PRIMARY KEY,
        customer_pin VARCHAR(20) NOT NULL,
        scenario_name VARCHAR(100),
        lms_loan_id VARCHAR(50) NOT NULL,
        loan_type VARCHAR(30),
        amount DECIMAL(18,2) NOT NULL,
        term_months INT,
        start_date DATE,
        end_date DATE,
        status VARCHAR(20) NOT NULL,
        remaining_amount DECIMAL(18,2),
        was_on_time BIT NOT NULL DEFAULT 1,
        early_completion BIT NOT NULL DEFAULT 0,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END;
GO

-- Safeguard: ensure akb_score column exists (for databases created before this column was added)
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'akb_score')
BEGIN
    ALTER TABLE loan_applications ADD akb_score INT NULL
END;
GO

-- ============================================================
-- Seed data (only inserted if tables are empty)
-- ============================================================

-- Seed: rejection reasons
IF NOT EXISTS (SELECT 1 FROM rejection_reasons)
BEGIN
    INSERT INTO rejection_reasons (reason_code, reason_description) VALUES
    ('ACTIVE_LOAN', 'Müştərinin aktiv krediti mövcuddur'),
    ('LATE_PAYMENT', 'Keçmiş kreditlərdə gecikmə var'),
    ('INCOME_INSUFFICIENT', 'Gəlir səviyyəsi kifayət deyil'),
    ('BLACKLIST', 'Müştəri qara siyahıdadır'),
    ('DOCUMENT_INVALID', 'Sənədlər etibarsızdır'),
    ('DUPLICATE_APPLICATION', 'Eyni müştəri təkrar müraciət edib'),
    ('SYSTEM_ERROR', 'Sistem xətası');
END;
GO

-- Seed: check type configurations
IF NOT EXISTS (SELECT 1 FROM check_type_config)
BEGIN
    INSERT INTO check_type_config (check_type, description, priority_order) VALUES
    ('lms_active_loan_check', 'LMS aktiv kredit yoxlaması', 1),
    ('lms_payment_history_check', 'LMS ödəniş tarixi yoxlaması', 2),
    ('credit_level_check', 'Kredit səviyyəsi yoxlaması', 3);
END;
GO

-- ============================================================
-- Seed data: credit levels (39 rate rows from business rules document)
-- Rates are percentages for the full loan term
-- ============================================================

IF NOT EXISTS (SELECT 1 FROM credit_levels)
BEGIN
    -- New Level: 2 ranges, only 3-month term (all phase 1 — starting level)
    INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, rate, unlock_phase, is_active) VALUES
    ('new', 100, 300, 3, 30.00, 1, 1),
    ('new', 301, 500, 3, 30.00, 1, 1);

    -- Trusted Level: 4 ranges, 3 and 6-month terms
    -- Phase 1 (1st loan): 100-700 AZN | Phase 2 (after 1 approved loan): + 701-900 AZN
    INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, rate, unlock_phase, is_active) VALUES
    ('trusted', 100, 300, 3, 29.00, 1, 1),
    ('trusted', 301, 500, 3, 28.00, 1, 1),
    ('trusted', 501, 700, 3, 27.00, 1, 1),
    ('trusted', 501, 700, 6, 29.00, 1, 1),
    ('trusted', 701, 900, 3, 27.00, 2, 1),
    ('trusted', 701, 900, 6, 29.00, 2, 1);

    -- Valuable Level: 6 ranges, 3, 6, 9-month terms
    -- Phase 1 (1st loan): 100-1100 AZN | Phase 2 (after 1 approved loan): + 1101-1300 AZN
    INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, rate, unlock_phase, is_active) VALUES
    ('valuable', 100, 300, 3, 28.00, 1, 1),
    ('valuable', 301, 500, 3, 27.00, 1, 1),
    ('valuable', 501, 700, 3, 26.00, 1, 1),
    ('valuable', 501, 700, 6, 28.00, 1, 1),
    ('valuable', 701, 900, 3, 26.00, 1, 1),
    ('valuable', 701, 900, 6, 28.00, 1, 1),
    ('valuable', 901, 1100, 6, 26.00, 1, 1),
    ('valuable', 901, 1100, 9, 28.00, 1, 1),
    ('valuable', 1101, 1300, 6, 25.00, 2, 1),
    ('valuable', 1101, 1300, 9, 27.00, 2, 1);

    -- Elite Level: 10 ranges, 3, 6, 9, 12-month terms
    -- Phase 1 (1st loan): 100-1500 AZN | Phase 2 (after 1 approved loan): + 1501-3000 AZN
    INSERT INTO credit_levels (level_name, min_amount, max_amount, term_months, rate, unlock_phase, is_active) VALUES
    ('elite', 100, 300, 3, 27.00, 1, 1),
    ('elite', 301, 500, 3, 26.00, 1, 1),
    ('elite', 501, 700, 3, 25.00, 1, 1),
    ('elite', 501, 700, 6, 27.00, 1, 1),
    ('elite', 701, 900, 3, 25.00, 1, 1),
    ('elite', 701, 900, 6, 27.00, 1, 1),
    ('elite', 901, 1100, 6, 25.00, 1, 1),
    ('elite', 901, 1100, 9, 27.00, 1, 1),
    ('elite', 1101, 1300, 6, 24.00, 1, 1),
    ('elite', 1101, 1300, 9, 26.00, 1, 1),
    ('elite', 1301, 1500, 6, 22.00, 1, 1),
    ('elite', 1301, 1500, 9, 24.00, 1, 1),
    ('elite', 1301, 1500, 12, 26.00, 1, 1),
    ('elite', 1501, 2000, 6, 21.00, 2, 1),
    ('elite', 1501, 2000, 9, 23.00, 2, 1),
    ('elite', 1501, 2000, 12, 25.00, 2, 1),
    ('elite', 2001, 2500, 6, 20.00, 2, 1),
    ('elite', 2001, 2500, 9, 22.00, 2, 1),
    ('elite', 2001, 2500, 12, 24.00, 2, 1),
    ('elite', 2501, 3000, 9, 22.00, 2, 1),
    ('elite', 2501, 3000, 12, 21.00, 2, 1);
END;
GO
