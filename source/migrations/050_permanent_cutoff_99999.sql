-- 050_permanent_cutoff_99999.sql
-- PR #289: Daimi blok üçün unikal konvensiya — validity_days = 99999.
--
-- Yeni konvensiya (sistem hələ tam istifadəyə verilməyib — köhnə "0 = daimi"
-- yozuşuna backward compat lazım deyil):
--   validity_days = 99999  → DAIMİ blok (təkrar müraciət mümkün deyil)
--   validity_days = N      → N gün sonra təkrar müraciət mümkündür
--   validity_days = 0      → gözləmə yoxdur (dərhal təkrar müraciət)
--   is_active = 0          → rule oxunmur (fail-soft icazə)
--
-- 022-ci migration daimi rule-ları 0 ilə seed edir (köhnə konvensiya).
-- Yeni kodda 0 = "gözləmə yoxdur" deməkdir, ona görə bu migration onları
-- 99999-a çevirir — əks halda daimi rule-lar bloklamayan olardı.

-- Bütün aktiv daimi rule-lar 0 → 99999 (AGE_OVER_69, AZMK_BLACKLIST,
-- EMPLOYMENT_TENURE, LW_BLACKLIST, DISABILITY_GROUP1, COMPLIANCE,
-- MANUAL_COMPLIANCE_FAILED daxil).
-- MANUAL_CALL_INTERFERENCE (is_active=0) bu UPDATE-ə düşmür.
UPDATE business_cutoffs
SET validity_days = 99999
WHERE validity_days = 0 AND is_active = 1;
GO

-- Açıq şəkildə təsdiq: MANUAL_COMPLIANCE_FAILED (Open Sanction və HMS
-- yoxlama) — daimi, təkrar müraciət mümkün deyil.
UPDATE business_cutoffs
SET validity_days = 99999
WHERE rule_code = 'MANUAL_COMPLIANCE_FAILED';
GO
