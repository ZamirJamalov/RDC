-- 049_expert_rejection_reasons.sql
-- PR #287: RDC dashboard-da ekspert imtina səbəbləri — yalnız 7 səbəb, bu ardıcıllıqda:
--   1. Video uyğunsuzluq                — 1 gün gözləmə
--   2. Open Sanction və HMS yoxlama     — daimi (təkrar müraciət mümkün deyil)
--   3. Nömrə təqdim etmir               — 24 saat
--   4. Əlaqə nömrələrindən mənfi rəy    — 14 gün
--   5. Müştərinin kobud rəftarı         — 10 gün
--   6. Zəng zamanı kənar müdaxilə       — 0 gün (bloklama yoxdur)
--   7. GetContact — mənfi taq           — 30 gün
--
-- validity_days: 0 = daimi blok, N = N gündən sonra təkrar müraciət mümkündür.
-- Köhnə manual rule-lar (MANUAL_FAKE_INFO və s.) SİLİNMİR — keçmiş rejeksiyaların
-- validity_days bloku təbii şəkildə bitir. Dashboard-da artıq təklif olunmurlar.
--
-- MANUAL_CALL_INTERFERENCE üçün is_active=0: checkLastRejectionCutoff yalnız
-- is_active=1 rule-ları oxuyur, tapmayanda icazə verir (fail-soft) → 0 gün gözləmə
-- = dərhal təkrar müraciət mümkündür. İmtina yalnız cutoff_results-a yazılır.

-- 1) Saxlanılan rule-ların müddətləri yenilənir
UPDATE business_cutoffs
SET description = N'Open Sanction və HMS yoxlama', validity_days = 0
WHERE rule_code = 'MANUAL_COMPLIANCE_FAILED';
GO

UPDATE business_cutoffs
SET description = N'Müştərinin kobud rəftarı', validity_days = 10
WHERE rule_code = 'MANUAL_ROUGH_BEHAVIOR';
GO

-- 2) Yeni rule-lar (dashboard-dakı ardıcıllıqla)
IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_VIDEO_MISMATCH')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_VIDEO_MISMATCH', N'Video uyğunsuzluq', 1, 'manual', 1);
END
GO

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_NUMBER_NOT_PROVIDED')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_NUMBER_NOT_PROVIDED', N'Nömrə təqdim etmir', 1, 'manual', 1);
END
GO

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_CONTACTS_NEGATIVE')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_CONTACTS_NEGATIVE', N'Əlaqə nömrələrindən mənfi rəy', 14, 'manual', 1);
END
GO

-- 0 gün: is_active=0 → bloklamır (dərhal təkrar müraciət)
IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_CALL_INTERFERENCE')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_CALL_INTERFERENCE', N'Zəng zamanı kənar müdaxilə', 0, 'manual', 0);
END
GO

IF NOT EXISTS (SELECT 1 FROM business_cutoffs WHERE rule_code = 'MANUAL_GETCONTACT_NEGATIVE')
BEGIN
    INSERT INTO business_cutoffs (rule_code, description, validity_days, cutoff_type, is_active)
    VALUES ('MANUAL_GETCONTACT_NEGATIVE', N'GetContact mənfi taq', 30, 'manual', 1);
END
GO
