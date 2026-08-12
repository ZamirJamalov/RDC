-- 035_cutoff_calculation_details.sql
-- PR #174: cutoff_results cədvəlinə calculation_details sütunu.
-- Kompleks kesimlərin hesablama detalını saxlayır.

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('cutoff_results') AND name = 'calculation_details')
BEGIN
    ALTER TABLE cutoff_results ADD calculation_details NVARCHAR(MAX) NULL;
END
GO
