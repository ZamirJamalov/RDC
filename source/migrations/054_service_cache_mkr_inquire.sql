-- 054_service_cache_mkr_inquire.sql
-- PR #380: AZMK_GET_MKR_SCORE ve AZMK_INQUIRE_BY_ID_CARD 3 gunluk cache ile
-- isleyir. Emal axininda (early cutoffs) bu serviler onsuz da cagirilir —
-- GetOffer, PreValidate, resolveAkbHistory ve ProcessApplication eyni melumat
-- ucun fiziki cagiris etmir, cache-den oxuyur.
-- Cache HIT service_audit_logs-da method='CACHE' marker row kimi gorunur.
--
-- AZMK_GET_OWNER_DATA cachesiz QALIR (her emalda fiziki cagiris — isteyin uzre).
--
-- cache_days = 0 guardi: migrations her bashlangicta icra olundugu ucun bu
-- shert idempotentliyi temin edir ve el ile deyisdirilmish deyeri
-- restart-larda ustelemir (PR #375 pattern-i ile eyni).

UPDATE service_cache_config
SET cache_days = 3,
    description = 'AZMK getMkrScore — AKB skoru ve stop-faktor — 3 gunluk cache (PR #380)'
WHERE service_name = 'AZMK_GET_MKR_SCORE' AND cache_days = 0;

UPDATE service_cache_config
SET cache_days = 3,
    description = 'AZMK inquireByIdCard — kredit tarixchesi ve gecikmeler — 3 gunluk cache (PR #380)'
WHERE service_name = 'AZMK_INQUIRE_BY_ID_CARD' AND cache_days = 0;
