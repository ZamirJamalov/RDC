-- 029_application_timer.sql
-- PR #134: Muraciət üzərində işləmə vaxtını saxlamaq üçün timer.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('loan_applications') AND name = 'timer_seconds')
BEGIN
    ALTER TABLE loan_applications ADD timer_seconds INT NOT NULL DEFAULT 0;
END
GO
