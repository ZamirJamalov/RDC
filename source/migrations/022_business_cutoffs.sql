-- 022_business_cutoffs.sql
-- PR #89: Business cutoff rules with validity periods.
--
-- Each cutoff rule has a validity_days field:
--   0 = permanent (customer can never re-apply after this rejection)
--   N = customer can re-apply after N days
--
-- When a customer applies, the system checks their previous rejections.
-- If a previous rejection's rule has validity_days > 0 and the rejection
-- happened within that period, the new application is blocked.
-- If validity_days == 0, the rejection is permanent.
--
-- Rejection reasons in loan_applications now store rule_code (not free text)
-- so the system can match them to this table.

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'business_cutoffs')
BEGIN
    CREATE TABLE business_cutoffs (
        id INT IDENTITY(1,1) PRIMARY KEY,
        rule_code VARCHAR(50) NOT NULL UNIQUE,
        description NVARCHAR(500) NOT NULL,
        validity_days INT NOT NULL DEFAULT 0,  -- 0=permanent, N=can re-apply after N days
        is_active BIT NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END
GO

-- Seed data
IF NOT EXISTS (SELECT 1 FROM business_cutoffs)
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, is_active) VALUES
    ('AKB_SCORE_LOW',         'Skor bali 200-den asagi olduqda imtina olunsun',                    2, 1),  -- 48 saat = 2 gun
    ('DELAY_RATIO_HIGH',      'Gecikme gunleri uzre emsal 6%-den yuksek olduqda imtina olunsun',  2, 1),  -- 48 saat
    ('AGE_OVER_69',           'Yasi 69+ olduqda imtina olunsun',                                    0, 1),  -- daimi
    ('AKB_STOP_FACTOR',       'AKB stop faktoruna dushen mushterilere imtina olunsun',             30, 1), -- 1 ay
    ('AZMK_BLACKLIST',        'Mushteri AZMK-da qara siyahidadirsa imtina olunsun',                0, 1),  -- daimi
    ('ACTIVE_DELAY_HIGH',     'Aktiv kreditlerinde cari gun gecikmesi 5-den artiq olanlara imtina',2, 1), -- 48 saat
    ('DELAY_3M',              'Son 3 ayda maksimal gecikme 20+ olduqda imtina olunsun',            30, 1), -- 1 ay
    ('DELAY_6M',              'Son 6 ayda maksimal gecikme 30+ olduqda imtina olunsun',            30, 1), -- 1 ay
    ('DELAY_12M',             'Son 12 ayda maksimal gecikme 45+ olduqda imtina olunsun',           30, 1), -- 1 ay
    ('DELAY_18M',             'Son 18 ayda maksimal gecikme 60+ olduqda imtina olunsun',           30, 1), -- 1 ay
    ('COMPLIANCE',            'Komplayns yoxlamalarini kecmeyenlere imtina olunsun',               0, 1),  -- daimi
    ('MONTHLY_PAYMENTS_HIGH', 'Aktiv ayliq odenislerin cemi 2000azn-den artiq olduqda imtina',     2, 1),  -- 48 saat
    ('EMPLOYMENT_TENURE',     'Emek staji 6 aydan azdirsa imtina olunsun',                          0, 1),  -- daimi
    ('DISABILITY_GROUP1',     '1ci qrup elliyi olan mushterilere imtina olunsun',                   0, 1),  -- daimi
    ('ACTIVE_LOAN',           'Aktiv krediti olan mushteriye imtina olunsun',                       2, 1),  -- 48 saat
    ('LATE_PAYMENT',          'Gecikmeli odenis tarixcesi olanlara imtina olunsun',                 2, 1),  -- 48 saat
    ('NO_COMMISSION_FOUND',   'Uygun komissiya tapilmadi',                                          2, 1),  -- 48 saat
    ('LW_BLACKLIST',          'LW qara siyahisinda olan mushteriye imtina olunsun',                 0, 1);  -- daimi
END
GO
