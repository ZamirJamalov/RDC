-- ============================================================
-- seed_discount_code.sql
-- PR #97 / PR #98: Test endirim kodları və müştərilər əlavə edir.
-- ============================================================
--
-- Bu script endirim kodu funksionalını sadə yolla sınaqdan
-- keçirmək üçün DB-yə test datası əlavə edir.
--
-- İSTİFADƏ:
--   sqlcmd -S DB_HOST -U DB_USER -P DB_PASSWORD -d RDC -i seed_discount_code.sql
--
-- TƏMİZLƏMƏK (PR #98):
--   sqlcmd -S DB_HOST -U DB_USER -P DB_PASSWORD -d RDC -Q "
--     DELETE FROM discount_codes WHERE code IN ('ALPUL-TEST01','ALPUL-TEST02');
--     DELETE FROM loan_applications WHERE customer_pin IN ('TESTA','TESTB');
--     DELETE FROM customers WHERE customer_pin IN ('TESTA','TESTB');"
--
-- ENDİRİM KODUNU ON/OFF ETMƏK (PR #98):
--   OFF:  sqlcmd -Q "UPDATE system_settings SET value='0' WHERE key='discount_codes_enabled'"
--   ON:   sqlcmd -Q "UPDATE system_settings SET value='1' WHERE key='discount_codes_enabled'"
--   Və ya HTTP ilə:
--     curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled \
--       -H "Content-Type: application/json" -d '{"enabled": false}'
--
-- Tələb: Migration 023, 024, 025 artıq run olunmalıdır.
-- ============================================================

BEGIN TRANSACTION;

-- ============================================================
-- 1. Test müştəriləri
-- ============================================================

-- Müştəri A (endirim kodu sahibi)
IF NOT EXISTS (SELECT 1 FROM customers WHERE customer_pin = 'TESTA')
BEGIN
    INSERT INTO customers (customer_pin, full_name, phone, actual_address)
    VALUES ('TESTA', 'Test Mushteri A', '+994501111111', 'Baki, Nizami r.');
    PRINT '✓ Müştəri A yaradıldı: TESTA (PIN)';
END
ELSE
    PRINT '  Müştəri A artıq mövcuddur: TESTA';

-- Müştəri B (koddan istifadə edəcək)
IF NOT EXISTS (SELECT 1 FROM customers WHERE customer_pin = 'TESTB')
BEGIN
    INSERT INTO customers (customer_pin, full_name, phone, actual_address)
    VALUES ('TESTB', 'Test Mushteri B', '+994552222222', 'Baki, Sebail r.');
    PRINT '✓ Müştəri B yaradıldı: TESTB (PIN)';
END
ELSE
    PRINT '  Müştəri B artıq mövcuddur: TESTB';

-- ============================================================
-- 2. Endirim kodları
-- ============================================================

DECLARE @customer_a_id INT;
SELECT @customer_a_id = id FROM customers WHERE customer_pin = 'TESTA';

-- Kod 1: 10% percent endirim
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST01')
BEGIN
    INSERT INTO discount_codes (
        code, issued_to_customer_id, issued_from_application_id,
        discount_type, discount_value, status
    ) VALUES (
        'ALPUL-TEST01', @customer_a_id, NULL,
        'percent', 10.00, 'active'
    );
    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST01 (10% percent, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST01';

-- Kod 2: 5 AZN fixed endirim
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST02')
BEGIN
    INSERT INTO discount_codes (
        code, issued_to_customer_id, issued_from_application_id,
        discount_type, discount_value, status
    ) VALUES (
        'ALPUL-TEST02', @customer_a_id, NULL,
        'fixed', 5.00, 'active'
    );
    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST02 (5 AZN fixed, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST02';

-- ============================================================
-- 3. Feature flag yoxlanışı (PR #98)
-- ============================================================
DECLARE @flag_value VARCHAR(10);
SELECT @flag_value = value FROM system_settings WHERE key = 'discount_codes_enabled';

IF @flag_value = '0'
    PRINT '';
    PRINT '⚠ ENDİRİM KODU FUNKİSIONALI HAL-HAZIRDA SÖNDÜRÜLÜB!';
    PRINT '  Yandırmaq üçün:';
    PRINT '  sqlcmd -Q "UPDATE system_settings SET value=''1'' WHERE key=''discount_codes_enabled''"';
    PRINT '  Və ya:';
    PRINT '  curl -X PUT http://localhost:8000/api/admin/feature-flags/discount_codes_enabled -H "Content-Type: application/json" -d "{\"enabled\": true}"';
END

-- ============================================================
-- 4. Nəticəni göstər
-- ============================================================
PRINT '';
PRINT '=== Cari endirim kodları ===';
SELECT
    dc.code,
    dc.discount_type,
    dc.discount_value,
    dc.status,
    c.customer_pin AS owner_pin,
    c.full_name AS owner_name
FROM discount_codes dc
JOIN customers c ON c.id = dc.issued_to_customer_id
WHERE dc.code IN ('ALPUL-TEST01', 'ALPUL-TEST02');

PRINT '';
PRINT '=== Feature flag statusu ===';
SELECT key, value, description FROM system_settings WHERE key = 'discount_codes_enabled';

COMMIT TRANSACTION;

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
