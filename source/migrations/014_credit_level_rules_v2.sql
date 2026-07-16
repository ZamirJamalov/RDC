-- ============================================================
-- Credit Level Rules V2 — delay days, level tracking, min term
-- ============================================================

-- 1. Add new columns to mock_lms_loans
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mock_lms_loans') AND name = 'delay_days')
BEGIN
    ALTER TABLE mock_lms_loans ADD delay_days INT NOT NULL DEFAULT 0
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mock_lms_loans') AND name = 'level_at_close')
BEGIN
    ALTER TABLE mock_lms_loans ADD level_at_close VARCHAR(20) NULL
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('mock_lms_loans') AND name = 'closed_at')
BEGIN
    ALTER TABLE mock_lms_loans ADD closed_at DATETIME NULL
END;
GO

-- 2. Add new columns to credit_level_rules
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('credit_level_rules') AND name = 'max_delay_days')
BEGIN
    ALTER TABLE credit_level_rules ADD max_delay_days INT NOT NULL DEFAULT 0
END;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('credit_level_rules') AND name = 'min_term_months')
BEGIN
    ALTER TABLE credit_level_rules ADD min_term_months INT NOT NULL DEFAULT 0
END;
GO

-- 3. Clear old rules and insert new ones
DELETE FROM credit_level_rules;
GO

INSERT INTO credit_level_rules (from_level, to_level, min_completed_loans, all_on_time, has_early_completion, max_delay_days, min_term_months, is_active)
VALUES
('new',      'trusted',  2, 1, 0, 2, 3, 1),
('trusted',  'valuable', 2, 1, 0, 3, 3, 1),
('valuable', 'elite',    2, 1, 0, 4, 2, 1);
GO
