-- 045_min_amount_zero.sql
-- PR #252: Minimum kredit məbləğini 50 AZN-dən 0 AZN-ə endir.
--
-- Background:
--   Migration 018 (PR #76) min_amount=100 → 50 etmişdi.
--   Migration 021 (PR #86) credit_levels cədvəlini yenidən seed etdi —
--   bütün aktiv range-lərdə min_amount = 50 qaldı.
--
-- PR #252 business requirement:
--   Minimum kredit məbləği 0 AZN olmalıdır (yəni slider 0-dan başlasın).
--   İstifadəçi məbləği 0 seçib "Müraciət Göndər" klikləsə, backend-də
--   CustomerConfirmApplication yoxlayır: amount > 0 (application_service_customer_confirm.go:69).
--   Yəni 0 AZN seçib təsdiqləmək backend-də bloklanır.
--
-- Dəyişiklik:
--   Bütün aktiv credit_levels sətirlərində min_amount = 50 → 0.
--   Yeni "50 AZN minimum" məhdudiyyəti qalmır.
--   Komissiya/faiz dərəcələri və phase-lər dəyişmir.
--
-- Təsir:
--   - apply.html slider min dəyəri artıq DB-dən gəlir (rangeMin computed → 0)
--   - landing.html slider min="50" hardcoded — PR #252-də min="0" edilir
--   - Backend amount > 0 yoxlaması saxlanılır (0 AZN kredit məbləği qəbul edilmir)

UPDATE credit_levels SET min_amount = 0 WHERE min_amount = 50 AND is_active = 1;
GO
