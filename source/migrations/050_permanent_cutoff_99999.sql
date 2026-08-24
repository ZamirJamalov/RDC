-- 050_permanent_cutoff_99999.sql
-- PR #288: Daimi blok üçün unikal konvensiya — validity_days = 99999.
--
-- Problem: əvvəl "0" iki fərqli mənada işlənirdi:
--   - validity_days = 0  → daimi blok (köhnə konvensiya, kodda belə işləyir)
--   - "0 gün gözləmə"    → blok yoxdur (MANUAL_CALL_INTERFERENCE, is_active=0)
-- Bu çaşqınlıq yaradır. Yeni unikal konvensiya:
--   validity_days = 99999  → DAIMİ blok (təkrar müraciət mümkün deyil)
--   validity_days = N      → N gün sonra təkrar müraciət mümkündür
--   is_active = 0          → rule bloklamır (dərhal təkrar müraciət)
-- Kod (checkLastRejectionCutoff) 0 və >=99999 hər ikisini daimi kimi qəbul edir
-- (0 = köhnə rule-lar üçün backward compat).

-- 1) Bütün aktiv daimi rule-ları 0 → 99999 (MANUAL_COMPLIANCE_FAILED daxil:
--    Open Sanction və HMS yoxlama — daimi, təkrar müraciət mümkün deyil).
--    MANUAL_CALL_INTERFERENCE (is_active=0) bu UPDATE-ə düşmür.
UPDATE business_cutoffs
SET validity_days = 99999
WHERE validity_days = 0 AND is_active = 1;
GO

-- 2) Açıq şəkildə təsdiq: MANUAL_COMPLIANCE_FAILED daimidir (99999).
UPDATE business_cutoffs
SET validity_days = 99999
WHERE rule_code = 'MANUAL_COMPLIANCE_FAILED';
GO
