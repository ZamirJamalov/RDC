-- 039_service_cache_config.sql
-- PR #205: Xarici servis response-larının cache konfiqurasiyası.
-- Hər servis üçün cache_days təyin etmək imkanı.
-- cache_days = 0 → birbaşa servise muraciet (cache yox)
-- cache_days > 0 → service_audit_logs-dan son uğurlu response oxu, cache_days içindədirsə istifadə et

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'service_cache_config')
BEGIN
    CREATE TABLE service_cache_config (
        id           INT IDENTITY(1,1) PRIMARY KEY,
        service_name NVARCHAR(100) NOT NULL UNIQUE,  -- 'AZMK_GET_OWNER_DATA', 'AZMK_GET_MKR_SCORE', və s.
        cache_days   INT NOT NULL DEFAULT 0,          -- 0 = cache yox, >0 = bu gün sayı ərzində cache
        description  NVARCHAR(200) NULL,
        updated_at   DATETIME NOT NULL DEFAULT GETDATE()
    );
END
GO

-- Seed: default cache_days = 0 (cache yox — birbaşa servise muraciet)
IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_GET_OWNER_DATA')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_GET_OWNER_DATA', 0, 'AZMK getOwnerData — qara siyahı və aktiv kredit yoxlaması');
GO
IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_GET_MKR_SCORE')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_GET_MKR_SCORE', 0, 'AZMK getMkrScore — AKB skoru və stop-faktor');
GO
IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_GET_PERSONAL_INFO')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_GET_PERSONAL_INFO', 0, 'AZMK GetPersonalInfo — yaş yoxlaması');
GO
IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_INQUIRE_BY_ID_CARD')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_INQUIRE_BY_ID_CARD', 0, 'AZMK inquireByIdCard — kredit tarixçəsi və gecikmə');
GO

-- Index for fast service_name lookup
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_service_cache_config_service' AND object_id = OBJECT_ID('service_cache_config'))
BEGIN
    CREATE UNIQUE INDEX IX_service_cache_config_service ON service_cache_config(service_name);
END
GO
