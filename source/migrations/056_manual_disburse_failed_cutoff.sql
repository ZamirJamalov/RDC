-- 056_manual_disburse_failed_cutoff.sql
-- PR #441: MANUAL_DISBURSE_FAILED cutoff rule-u business_cutoffs cədvəlinə əlavə et.
-- detail.html-də (yalnız ADMIN) disburse_failed statuslu müraciətlər üçün imtina
-- səbəbi kimi görünür. Gözləmə müddəti 0 gün — müştəri dərhal təkrar müraciət
-- edə bilər (PR #289 üzrə validity_days=0 = gözləmə yoxdur).

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_DISBURSE_FAILED')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_DISBURSE_FAILED', N'Disburse xətası (köçürmə alınmadı)', 0, 'manual', 1);
END
GO
