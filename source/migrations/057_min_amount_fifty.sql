-- 057_min_amount_fifty.sql
-- PR #498: Minimum kredit məbləğini 1 AZN-dən 50 AZN-ə yüksəlt.
--
-- Background:
--   Migration 045 (PR #252) min_amount = 50 → 0 etmişdi.
--   Migration 046 (PR #253) min_amount = 0 → 1 etmişdi.
--   Business requirement yenidən dəyişdi: minimum kredit məbləği 50 AZN-dir
--   (landing kartları "50-dən ... -dək", apply slider 50-dən başlayır).
--
-- Dəyişiklik:
--   Bütün aktiv credit_levels sətirlərində min_amount = 1 → 50.
--   Komissiya/faiz dəyərləri və phase-lər dəyişmir.
--
-- Təsir:
--   - apply.html rangeMin = 50 (DB-dən gəlir; frontend-də 50 floor da var)
--   - Backend amount > 0 yoxlaması saxlanılır (50 > 0, keçir)

UPDATE credit_levels SET min_amount = 50 WHERE min_amount = 1 AND is_active = 1;
GO
