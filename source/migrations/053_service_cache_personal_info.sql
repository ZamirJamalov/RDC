-- 053_service_cache_personal_info.sql
-- PR #379: AZMK_GET_PERSONAL_INFO 3 günlük cache ilə işləyir.
-- Emal axınında GetPersonalInfo iki dəfə çağırılırdı (early cutoffs +
-- CreditEngine.resolveCustomerAge) — indi ikincisi cache-dən oxunur.
-- Cache HIT service_audit_logs-da method='CACHE' marker row kimi görünür.
--
-- cache_days = 0 guardı: migrations hər başlanğıcda icra olunduğu üçün bu
-- şərt idempotentliyi təmin edir və əl ilə dəyişdirilmiş dəyəri restart-larda
-- üstələmir (PR #375 pattern-i ilə eyni).

UPDATE service_cache_config
SET cache_days = 3,
    description = 'AZMK GetPersonalInfo — yaş/ad məlumatları — 3 günlük cache (PR #379)'
WHERE service_name = 'AZMK_GET_PERSONAL_INFO' AND cache_days = 0;
