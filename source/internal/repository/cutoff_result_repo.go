package repository

import (
	"context"
	"database/sql"
	"fmt"

	"rdc-source/internal/model"
)

// CutoffResultRepo handles database operations for cutoff results.
type CutoffResultRepo struct {
	db *sql.DB
}

// NewCutoffResultRepo creates a new CutoffResultRepo.
func NewCutoffResultRepo(db *sql.DB) *CutoffResultRepo {
	return &CutoffResultRepo{db: db}
}

// Insert logs a cutoff check result.
func (r *CutoffResultRepo) Insert(ctx context.Context, cr *model.CutoffResult) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cutoff_results
			(application_id, cutoff_code, cutoff_name, service_name, checked, passed, actual_value, threshold, details, calculation_details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cr.ApplicationID, cr.CutoffCode, cr.CutoffName, cr.ServiceName,
		cr.Checked, cr.Passed, cr.ActualValue, cr.Threshold, cr.Details, cr.CalculationDetails,
	)
	if err != nil {
		return fmt.Errorf("failed to insert cutoff result: %w", err)
	}
	return nil
}

// ListByApplication retrieves all cutoff results for a given application.
func (r *CutoffResultRepo) ListByApplication(ctx context.Context, appID int) ([]model.CutoffResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, application_id, cutoff_code, cutoff_name, service_name,
		       checked, passed, actual_value, threshold, details, calculation_details, created_at
		FROM cutoff_results
		WHERE application_id = ?
		ORDER BY id ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cutoff results: %w", err)
	}
	defer rows.Close()

	var results []model.CutoffResult
	for rows.Next() {
		var cr model.CutoffResult
		var serviceName, actualValue, threshold, details, calcDetails sql.NullString
		if err := rows.Scan(
			&cr.ID, &cr.ApplicationID, &cr.CutoffCode, &cr.CutoffName, &serviceName,
			&cr.Checked, &cr.Passed, &actualValue, &threshold, &details, &calcDetails, &cr.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan cutoff result: %w", err)
		}
		cr.ServiceName = serviceName.String
		cr.ActualValue = actualValue.String
		cr.Threshold = threshold.String
		cr.Details = details.String
		cr.CalculationDetails = calcDetails.String
		results = append(results, cr)
	}
	return results, rows.Err()
}
