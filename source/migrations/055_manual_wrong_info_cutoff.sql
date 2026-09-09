-- 055_manual_wrong_info_cutoff.sql
-- PR #429: MANUAL_WRONG_INFO cutoff rule-u business_cutoffs cədvəlinə əlavə et.
-- detail.html-də ekspert "Yanlış məlumat" səbəbi ilə imtina edə bilər.
-- Gözləmə müddəti: 10 gün (dashboard dropdown-da GetContact-dan bir əvvəl).
-- Pattern: 049_expert_rejection_reasons.sql (PR #258/287).

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_WRONG_INFO')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_WRONG_INFO', N'Yanlış məlumat', 10, 'manual', 1);
END
GO
