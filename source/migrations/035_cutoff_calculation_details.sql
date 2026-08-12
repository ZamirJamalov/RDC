-- 035_cutoff_calculation_details.sql
-- PR #173: cutoff_results cədvəlinə calculation_details sütunu əlavə edilməsi.
-- Kompleks kesimlərin (DELAY_RATIO_HIGH, MONTHLY_PAYMENTS_HIGH və s.)
-- hesablama detalını saxlayır.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('cutoff_results') AND name = 'calculation_details')
BEGIN
    ALTER TABLE cutoff_results ADD calculation_details NVARCHAR(MAX) NULL;
END
GO
