-- 040_service_audit_logs_error_max.sql
-- PR #207: service_audit_logs.error sütununu NVARCHAR(500) → NVARCHAR(MAX) çevir.
-- Səbəb: AZMK SOAP XML error mesajları 500 simvoldan uzundur, truncation xətası verir.
-- Nümunə: "AZMK getMkrScore error: MKR HTTP 500: <soap:Envelope xmlns:soap=..."

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('service_audit_logs') AND name = 'error' AND system_type_id = 231 AND max_length = -1)
BEGIN
    ALTER TABLE service_audit_logs ALTER COLUMN error NVARCHAR(MAX) NULL;
END
GO
