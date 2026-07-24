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
        cutoff_type VARCHAR(10) NOT NULL DEFAULT 'auto',  -- 'auto' or 'manual'
        is_active BIT NOT NULL DEFAULT 1,
        created_at DATETIME NOT NULL DEFAULT GETDATE()
    );
END
GO

-- Seed data
IF NOT EXISTS (SELECT 1 FROM business_cutoffs)
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active) VALUES
    -- AUTO cutoffs (sistem terefinden yoxlanilir)
    ('AKB_SCORE_LOW',         'Skor bali 200-den asagi olduqda imtina olunsun',                    2, 'auto', 1),
    ('DELAY_RATIO_HIGH',      'Gecikme gunleri uzre emsal 6%-den yuksek olduqda imtina olunsun',  2, 'auto', 1),
    ('AGE_OVER_69',           'Yasi 69+ olduqda imtina olunsun',                                    0, 'auto', 1),
    ('AKB_STOP_FACTOR',       'AKB stop faktoruna dushen mushterilere imtina olunsun',             30, 'auto', 1),
    ('AZMK_BLACKLIST',        'Mushteri AZMK-da qara siyahidadirsa imtina olunsun',                0, 'auto', 1),
    ('ACTIVE_DELAY_HIGH',     'Aktiv kreditlerinde cari gun gecikmesi 5-den artiq olanlara imtina',2, 'auto', 1),
    ('DELAY_3M',              'Son 3 ayda maksimal gecikme 20+ olduqda imtina olunsun',            30, 'auto', 1),
    ('DELAY_6M',              'Son 6 ayda maksimal gecikme 30+ olduqda imtina olunsun',            30, 'auto', 1),
    ('DELAY_12M',             'Son 12 ayda maksimal gecikme 45+ olduqda imtina olunsun',           30, 'auto', 1),
    ('DELAY_18M',             'Son 18 ayda maksimal gecikme 60+ olduqda imtina olunsun',           30, 'auto', 1),
    ('COMPLIANCE',            'Komplayns yoxlamalarini kecmeyenlere imtina olunsun',               0, 'auto', 1),
    ('MONTHLY_PAYMENTS_HIGH', 'Aktiv ayliq odenislerin cemi 2000azn-den artiq olduqda imtina',     2, 'auto', 1),
    ('EMPLOYMENT_TENURE',     'Emek staji 6 aydan azdirsa imtina olunsun',                          0, 'auto', 1),
    ('DISABILITY_GROUP1',     '1ci qrup elliyi olan mushterilere imtina olunsun',                   0, 'auto', 1),
    ('ACTIVE_LOAN',           'Aktiv krediti olan mushteriye imtina olunsun',                       2, 'auto', 1),
    ('LATE_PAYMENT',          'Gecikmeli odenis tarixcesi olanlara imtina olunsun',                 2, 'auto', 1),
    ('NO_COMMISSION_FOUND',   'Uygun komissiya tapilmadi',                                          2, 'auto', 1),
    ('LW_BLACKLIST',          'LW qara siyahisinda olan mushteriye imtina olunsun',                 0, 'auto', 1),
    -- MANUAL cutoffs (ekspert terefinden dashboard-da secilir)
    ('MANUAL_FAKE_INFO',      'Yalan melumat',                                                      90, 'manual', 1),
    ('MANUAL_WRONG_NUMBER',   'Nomre yanlisdir',                                                    2, 'manual', 1),
    ('MANUAL_ROUGH_BEHAVIOR', 'Kobud reftar',                                                       30, 'manual', 1),
    ('MANUAL_DOCS_INVALID',   'Senedler uygun deyil',                                               7, 'manual', 1),
    ('MANUAL_INCOME_LOW',     'Gelir kifayet deyil',                                                30, 'manual', 1),
    ('MANUAL_CONTACTS_WRONG', 'Elaqe nomreleri yanlis',                                             2, 'manual', 1),
    ('MANUAL_MYGOV_DENIED',   'MyGov icaze verilmedi',                                               7, 'manual', 1),
    ('MANUAL_OTHER',          'Diger',                                                              7, 'manual', 1);
END
GO
