-- 038_sms_provider_parametric.sql
-- PR #196: Softline SMS provayderini parametrik et.
-- sms_providers cədvəlinə yeni sütunlar əlavə edir ki, hər hansı SMS provayder
-- (Softline, Twilio, MessageBird və s.) eyni kodla dəstəklənsin.
--
-- Yeni sütunlar:
--   http_method        — GET/POST (default: GET)
--   param_user         — URL param adı (default: user)
--   param_password     — URL param adı (default: password)
--   param_phone        — URL param adı (default: gsm)
--   param_sender       — URL param adı (default: from)
--   param_text         — URL param adı (default: text)
--   success_field      — response-də success yoxlanılan sahə (default: errno)
--   success_value      — success dəyəri (default: 100)
--   error_field        — response-də error mətni sahəsi (default: errtext)

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'http_method')
BEGIN
    ALTER TABLE sms_providers ADD http_method NVARCHAR(10) NOT NULL DEFAULT 'GET';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'param_user')
BEGIN
    ALTER TABLE sms_providers ADD param_user NVARCHAR(50) NOT NULL DEFAULT 'user';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'param_password')
BEGIN
    ALTER TABLE sms_providers ADD param_password NVARCHAR(50) NOT NULL DEFAULT 'password';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'param_phone')
BEGIN
    ALTER TABLE sms_providers ADD param_phone NVARCHAR(50) NOT NULL DEFAULT 'gsm';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'param_sender')
BEGIN
    ALTER TABLE sms_providers ADD param_sender NVARCHAR(50) NOT NULL DEFAULT 'from';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'param_text')
BEGIN
    ALTER TABLE sms_providers ADD param_text NVARCHAR(50) NOT NULL DEFAULT 'text';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'success_field')
BEGIN
    ALTER TABLE sms_providers ADD success_field NVARCHAR(50) NOT NULL DEFAULT 'errno';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'success_value')
BEGIN
    ALTER TABLE sms_providers ADD success_value NVARCHAR(20) NOT NULL DEFAULT '100';
END
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('sms_providers') AND name = 'error_field')
BEGIN
    ALTER TABLE sms_providers ADD error_field NVARCHAR(50) NOT NULL DEFAULT 'errtext';
END
GO

-- PR #196: migration.go dropAllTables-də sms_providers artıq var (011-dən).
-- Yeni sütunlar DROP TABLE ilə birlikdə silinir, ona görə ayrıca drop lazım deyil.
