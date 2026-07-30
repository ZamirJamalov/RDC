-- ============================================================
-- seed_discount_code.sql
-- PR #97: Test endirim kodları və müştərilər əlavə edir.
-- ============================================================
--
-- Bu script endirim kodu funksionalını sadə yolla sınaqdan
-- keçirmək üçün DB-yə test datası əlavə edir.
--
-- Əlavə olunanlar:
--   1. 2 test müştərisi (TESTA, TESTB)
--   2. 2 endirim kodu (ALPUL-TEST01, ALPUL-TEST02)
--
-- İstifadə:
--   sqlcmd -S DB_HOST -U DB_USER -P DB_PASSWORD -d RDC -i seed_discount_code.sql
--
-- Tələb: Migration 023 və 024 artıq run olunmalıdır.
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
-- PR #97: issued_from_application_id artıq NULLABLE-dır,
-- ona görə manual seed kodları təmiz şəkildə əlavə olunur.

DECLARE @customer_a_id INT;
SELECT @customer_a_id = id FROM customers WHERE customer_pin = 'TESTA';

-- Kod 1: 10% endirim (komissiyanın 10%-i)
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST01')
BEGIN
    INSERT INTO discount_codes (
        code,
        issued_to_customer_id,
        issued_from_application_id,  -- NULL = manually created (PR #97)
        discount_type,
        discount_value,
        status
    ) VALUES (
        'ALPUL-TEST01',
        @customer_a_id,
        NULL,           -- manually seeded, no source application
        'percent',      -- komissiyanın faizi kimi
        10.00,          -- 10% endirim
        'active'
    );
    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST01 (10% percent, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST01';

-- Kod 2: 5 AZN fixed endirim (mütləq məbləğ)
IF NOT EXISTS (SELECT 1 FROM discount_codes WHERE code = 'ALPUL-TEST02')
BEGIN
    INSERT INTO discount_codes (
        code,
        issued_to_customer_id,
        issued_from_application_id,
        discount_type,
        discount_value,
        status
    ) VALUES (
        'ALPUL-TEST02',
        @customer_a_id,
        NULL,
        'fixed',        -- mütləq məbləğ
        5.00,           -- 5 AZN endirim
        'active'
    );
    PRINT '✓ Endirim kodu əlavə edildi: ALPUL-TEST02 (5 AZN fixed, sahib: TESTA)';
END
ELSE
    PRINT '  Endirim kodu artıq mövcuddur: ALPUL-TEST02';

-- ============================================================
-- 3. Nəticəni göstər
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
PRINT 'Və ya curl ilə validate et:';
PRINT '  curl "http://localhost:8000/api/discount-codes/validate?code=ALPUL-TEST01"';
PRINT '============================================================';
