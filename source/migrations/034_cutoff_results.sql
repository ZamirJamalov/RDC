-- 034_cutoff_results.sql
-- PR #168: Hər müraciət üzrə kesim nöqtələrinin plan/fakt nəticələri.
-- Hansı kesim yoxlanılıb, nəticə nə olub, hansı dəyər tapılıb.

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'cutoff_results')
BEGIN
    CREATE TABLE cutoff_results (
        id              INT IDENTITY(1,1) PRIMARY KEY,
        application_id  INT NOT NULL,
        cutoff_code     NVARCHAR(50)  NOT NULL,   -- AKB_SCORE_LOW, AZMK_BLACKLIST, AGE_OVER_69, DELAY_RATIO_HIGH və s.
        cutoff_name     NVARCHAR(200) NOT NULL,   -- İnsan oxunaqlı adı
        service_name    NVARCHAR(100) NULL,       -- Hansı servis çağrıldı (AZMK_GET_MKR_SCORE və s.)
        checked         BIT           NOT NULL DEFAULT 0,  -- Yoxlanıldımı?
        passed          BIT           NOT NULL DEFAULT 0,  -- Keçdimi? (1=keçdi, 0=imtina)
        actual_value    NVARCHAR(200) NULL,       -- Tapılan faktiki dəyər (məs: "point=150", "ratio=7.5")
        threshold       NVARCHAR(100) NULL,       -- Gözlənilən hüdud (məs: "< 200", "> 6", ">= 20")
        details         NVARCHAR(MAX) NULL,       -- Əlavə detallar
        created_at      DATETIME      NOT NULL DEFAULT GETDATE()
    );
END
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_cutoff_results_app_id' AND object_id = OBJECT_ID('cutoff_results'))
BEGIN
    CREATE INDEX IX_cutoff_results_app_id ON cutoff_results(application_id);
END
GO
