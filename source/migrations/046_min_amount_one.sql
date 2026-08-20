-- 046_min_amount_one.sql
-- PR #253: Minimum kredit məbləğini 0 AZN-dən 1 AZN-ə yüksəlt.
--
-- Background:
--   Migration 045 (PR #252) min_amount = 50 → 0 etmişdi.
--   PR #252 review-dən sonra business requirement düzəldildi:
--   minimum 0 AZN deyil, 1 AZN olmalıdır (0 AZN kredit məbləği məntiqsizdir).
--
-- Dəyişiklik:
--   Bütün aktiv credit_levels sətirlərində min_amount = 0 → 1.
--   Komissiya/faiz dərəcələri və phase-lər dəyişmir.
--
-- Təsir:
--   - apply.html slider min dəyəri DB-dən gəlir (rangeMin computed → 1)
--   - landing.html slider min="0" → min="1" (PR #253-də düzəldilir)
--   - Backend amount > 0 yoxlaması saxlanılır (1 AZN > 0, keçir)

UPDATE credit_levels SET min_amount = 1 WHERE min_amount = 0 AND is_active = 1;
GO
