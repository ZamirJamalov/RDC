-- 042_service_cache_config_employee_pension.sql
-- PR #242: GetEmployeeInfoByPin ve GetPensionInfo servisleri cache konfiqurasiyaya elave edildi.
-- service_name service_audit_logs-da yazilan adlarla eynidir:
--   AZMK_GET_EMPLOYEE_INFO — GetEmployeeInfoByPin (EMPLOYMENT_TENURE kesim noqtesi)
--   AZMK_GET_PENSION_INFO  — GetPensionInfoByPin (DISABILITY_GROUP1 kesim noqtesi)
-- Default cache_days = 0 (cache yox — birbasa servise muraciet).

IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_GET_EMPLOYEE_INFO')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_GET_EMPLOYEE_INFO', 0, 'AZMK GetEmployeeInfoByPin — iş yeri məlumatları (EMPLOYMENT_TENURE)');
GO
IF NOT EXISTS (SELECT 1 FROM service_cache_config WHERE service_name = 'AZMK_GET_PENSION_INFO')
    INSERT INTO service_cache_config (service_name, cache_days, description) VALUES ('AZMK_GET_PENSION_INFO', 0, 'AZMK GetPensionInfoByPin — pensiya və əlillik qrupu (DISABILITY_GROUP1)');
GO
