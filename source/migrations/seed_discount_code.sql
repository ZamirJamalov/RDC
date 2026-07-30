-- ============================================================
-- seed_discount_code.sql
-- PR #97 / PR #98 / PR #100: Test endirim kodları və müştərilər əlavə edir.
-- ============================================================
--
-- Bu script endirim kodu funksionalını sadə yolla sınaqdan
-- keçirmək üçün DB-yə test datası əlavə edir.
--
-- PR #100: DBeaver / SQL Editor uyumluluğu üçün yenidən yazıldı.
--   - Variable (@customer_a_id, @flag_value) YOXDUR — bunlar batch-scoped-dır
--     və DBeaver hər statement-ı ayrı batch kimi işlədir.
--   - Əvəzinə subquery və EXISTS istifadə olunur.
--   - Hər IF blok ayrı batch-dir (GO ilə ayrılır) — sqlcmd və DBeaver-da işləyir.
--
-- İSTİFADƏ:
--   sqlcmd -S DB_HOST -U DB_USER -P DB_PASSWORD -d RDC -i seed_discount_code.sql
--   Və ya DBeaver SQL Editor-da bütün mətni seçib Execute SQL (Alt+X)
--
-- TƏMİZLƏMƏK (reseed üçün):
--   DELETE FROM discount_codes WHERE code IN ('ALPUL-TEST01','ALPUL-TEST02');
--   DELETE FROM loan_applications WHERE customer_pin IN ('TESTA','TESTB');
--   DELETE FROM customers WHERE customer_pin IN ('TESTA','TESTB');
--   -- sonra yenidən bu script-i run et
--
-- ENDİRİM KODUNU ON/OFF ETMƏK (PR #98):
--   OFF:  UPDATE system_settings SET [value]='0' WHERE [key]='discount_codes_enabled';
--   ON:   UPDATE system_settings SET [value]='1' WHERE [key]='discount_codes_enabled';
--   Və ya HTTP ilə:
--     curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled \
--       -H "Content-Type: application/json" -d '{"enabled": false}'
--
-- Tələb: Migration 023, 024, 025 artıq run olunmalıdır.
-- ============================================================


-- ============================================================
-- 1. Müştəri A (endirim kodu sahibi)
-- ============================================================
IF NOT EXISTS (SELECT 1 FROM customers WHERE customer_pin = 'TESTA')
BEGIN
    INSERT INTO customers (customer_pin, full_name, phone, actual_address)
    VALUES ('TESTA', 'Test Mushteri A', '+994501111111', 'Baki, Nizami r.');
    PRINT '✓ Müştəri A yaradıldı: TESTA (PIN)';
END
ELSE
    PRINT '  Müştəri A artıq mövcuddur: TESTA';
GO


-- ============================================================
-- 2. Müştəri B (koddan istifadə edəcək)
-- ============================================================
IF NOT EXISTS (SELECT 1 FROM customers WHERE customer_pin = 'TESTB')
BEGIN
    INSERT INTO customers (customer_pin, full_name, phone, actual_address)
    VALUES ('TESTB', 'Test Mushteri B', '+994552222222', 'Baki, Sebail r.');
    PRINT '✓ Müştəri B yaradıldı: TESTB (PIN)';
END
ELSE
    PRINT '  Müştəri B artıq mövcuddur: TESTB';
GO


-- ============================================================
-- 3. Endirim kodu 1: ALPUL-TEST01 (10% percent)
-- PR #100: @customer_a_id variable əvəzinə subquery istifadə olunur.
-- Bu, DBeaver-də "Must declare the scalar variable" xətasını həll edir.
-- ============================================================
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST01')
BEGIN
    INSERT INTO discount_codes (
        code,
        issued_to_customer_id,
        issued_from_application_id,
        discount_type,
        discount_value,
        status
    )
    SELECT
        'ALPUL-TEST01',
        c.id,                       -- subquery ilə customer.id (variable yox)
        NULL,                       -- manually created (PR #97)
        'percent',
        10.00,
        'active'
    FROM customers c
    WHERE c.customer_pin = 'TESTA';

    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST01 (10% percent, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST01';
GO


-- ============================================================
-- 4. Endirim kodu 2: ALPUL-TEST02 (5 AZN fixed)
-- ============================================================
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST02')
BEGIN
    INSERT INTO discount_codes (
        code,
        issued_to_customer_id,
        issued_from_application_id,
        discount_type,
        discount_value,
        status
    )
    SELECT
        'ALPUL-TEST02',
        c.id,
        NULL,
        'fixed',
        5.00,
        'active'
    FROM customers c
    WHERE c.customer_pin = 'TESTA';

    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST02 (5 AZN fixed, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST02';
GO


-- ============================================================
-- 5. Feature flag yoxlanışı (PR #98)
-- PR #100: @flag_value variable əvəzinə EXISTS istifadə olunur.
-- ============================================================
IF EXISTS (
    SELECT 1 FROM system_settings
    WHERE [key] = 'discount_codes_enabled' AND [value] = '0'
)
BEGIN
    PRINT '';
    PRINT '⚠ ENDİRİM KODU FUNKİSIONALI HAL-HAZIRDA SÖNDÜRÜLÜB!';
    PRINT '  Yandırmaq üçün:';
    PRINT '  UPDATE system_settings SET [value]=''1'' WHERE [key]=''discount_codes_enabled'';';
    PRINT '  Və ya:';
    PRINT '  curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled -H "Content-Type: application/json" -d "{\"enabled\": true}"';
END
GO


-- ============================================================
-- 6. Nəticəni göstər — Cari endirim kodları
-- ============================================================
PRINT '';
PRINT '=== Cari endirim kodları ===';
SELECT
    dc.code,
    dc.discount_type,
    dc.discount_value,
    dc.status,
    c.customer_pin AS owner_pin,
    c.full_name    AS owner_name
FROM discount_codes dc
JOIN customers c ON c.id = dc.issued_to_customer_id
WHERE dc.code IN ('ALPUL-TEST01', 'ALPUL-TEST02');
GO


-- ============================================================
-- 7. Nəticəni göstər — Feature flag statusu
-- ============================================================
PRINT '';
PRINT '=== Feature flag statusu ===';
SELECT [key], [value], description
FROM system_settings
WHERE [key] = 'discount_codes_enabled';
GO


-- ============================================================
-- 8. Tamamlandı mesajı
-- ============================================================
PRINT '';
PRINT '============================================================';
PRINT '✓ Seed tamamlandı!';
PRINT '';
PRINT 'Endirim kodları:';
PRINT '  ALPUL-TEST01  →  10% percent  (sahib: TESTA)';
PRINT '  ALPUL-TEST02  →  5 AZN fixed  (sahib: TESTA)';
PRINT '';
PRINT 'İstifadə:';
PRINT '  1. Server-i run et: cd source && go run .';
PRINT '  2. apply.html aç və TESTB ilə müraciət et';
PRINT '  3. Endirim kodu xanasına ALPUL-TEST01 daxil et';
PRINT '  4. Real-time ✓ görməlisən';
PRINT '  5. Təsdiq et → ekspert approve → endirim tətbiq olunur';
PRINT '';
PRINT 'On/off (PR #98):';
PRINT '  curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled \';
PRINT '    -H "Content-Type: application/json" -d "{\"enabled\": false}"';
PRINT '============================================================';
GO
