-- 036_video_records.sql
-- PR #188: Video record service inteqrasiyası.
-- Müştərinin video identifikasiya qeydləri (Kvadrat Lab demo service).
-- Hər müraciət üçün 1 video record order yaradılır, status poll olunur.

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'video_records')
BEGIN
    CREATE TABLE video_records (
        id                  INT IDENTITY(1,1) PRIMARY KEY,
        application_id      INT NOT NULL,
        app_id_external     NVARCHAR(100) NOT NULL,         -- video service-ə göndərilən app_id (müraciət ID string kimi)
        order_redirect_url  NVARCHAR(500) NULL,             -- video service-dən qayıdan redirect_url
        phone               NVARCHAR(20)  NULL,             -- müştərinin telefonu
        amount              DECIMAL(18,2) NULL,             -- kredit məbləği
        customer_name       NVARCHAR(200)  NULL,            -- GetPersonalInfo-dən gələn ad/soyad
        request_body        NVARCHAR(MAX)  NULL,            -- video service-ə göndərilən raw JSON
        response_body       NVARCHAR(MAX)  NULL,            -- video service-dən gələn raw JSON
        status_request_body NVARCHAR(MAX)  NULL,            -- status sorğusunun raw JSON
        status_response_body NVARCHAR(MAX) NULL,            -- status cavabının raw JSON
        recorded            BIT            NOT NULL DEFAULT 0,  -- video record tamamlanıb?
        status_checked_at   DATETIME       NULL,            -- son status poll vaxtı
        created_at          DATETIME       NOT NULL DEFAULT GETDATE(),
        updated_at          DATETIME       NOT NULL DEFAULT GETDATE()
    );
END
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_video_records_app_id' AND object_id = OBJECT_ID('video_records'))
BEGIN
    CREATE INDEX IX_video_records_app_id ON video_records(application_id);
END
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_video_records_app_ext' AND object_id = OBJECT_ID('video_records'))
BEGIN
    CREATE INDEX IX_video_records_app_ext ON video_records(app_id_external);
END
GO
