-- 052_service_cache_employee_pension_3days.sql
-- PR #375: iş yeri (AZMK_GET_EMPLOYEE_INFO) və pensiya (AZMK_GET_PENSION_INFO)
-- yoxlamaları 3 günlük cache ilə işləyir. Dashboard-dakı "Yoxla" düymələri hər
-- klikdə AZMK-nı fiziki çağırmır — 3 gün ərzindəki son uğurlu response
-- service_audit_logs cədvəlindən oxunur (PR #205 mexanizmi).
--
-- cache_days = 0 guardı: migrations hər başlanğıcda icra olunduğu üçün bu
-- şərt idempotentliyi təmin edir və əl ilə dəyişdirilmiş dəyəri (məs. 1 gün)
-- restart-larda üstələmir.

UPDATE service_cache_config
SET cache_days = 3,
    description = 'AZMK GetEmployeeInfoByPin — iş yeri məlumatları (EMPLOYMENT_TENURE) — 3 günlük cache (PR #375)'
WHERE service_name = 'AZMK_GET_EMPLOYEE_INFO' AND cache_days = 0;

UPDATE service_cache_config
SET cache_days = 3,
    description = 'AZMK GetPensionInfoByPin — pensiya və əlillik qrupu (DISABILITY_GROUP1) — 3 günlük cache (PR #375)'
WHERE service_name = 'AZMK_GET_PENSION_INFO' AND cache_days = 0;
