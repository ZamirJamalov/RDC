-- 047_manual_compliance_failed_cutoff.sql
-- PR #258: MANUAL_COMPLIANCE_FAILED cutoff rule-u business_cutoffs cədvəlinə əlavə et.
-- detail.html-də ekspert "Komplyans uğursuzluğu" səbəbi ilə imtina edə bilər,
-- amma bu rule_code business_cutoffs-da yox idi — checkLastRejectionCutoff
-- fail-soft edirdi (re-apply icazə olunurdu). İndi validity_days = 0 (permanent).

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_COMPLIANCE_FAILED')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_COMPLIANCE_FAILED', 'Komplayns yoxlamasini kecmeyenlere imtina', 0, 'manual', 1);
END
GO
