package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	mssql "github.com/microsoft/go-mssqldb"

	"rdc-source/internal/model"
)

// ListByStatus retrieves all applications with the given status, ordered by
// oldest first (FIFO — experts should review the oldest applications first).
// Used by the expert queue endpoint to list pending_approval applications.
// PR #313: rejection_reason de qaytarilir — "Imtina olunmus" tab-da sebeb gosterilir.
// PR #376: processed_by_username de qaytarilir — siyahida "Işləyən" görünür.
//
// PR #94: includes discount_code so the expert dashboard can show whether
// the customer entered a referral code (transparency for the decision).
func (r *ApplicationRepo) ListByStatus(ctx context.Context, status string) ([]model.LoanApplication, error) {
	rows, err := r.db.QueryContext(ctx, `
                SELECT id, public_id, customer_pin, customer_full_name, amount, term_months,
                       loan_purpose, status, credit_level, approved_amount, approved_rate,
                       total_amount,
                       discount_code,
                       rejection_reason, processed_by_username,
                       created_at, updated_at
                FROM loan_applications
                WHERE status = ?
                ORDER BY created_at ASC`, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list applications by status: %w", err)
	}
	defer rows.Close()

	var apps []model.LoanApplication
	for rows.Next() {
		var app model.LoanApplication
		var rawPublicID mssql.UniqueIdentifier
		var creditLevel, loanPurpose, discountCode, rejectionReason, processedByUsername sql.NullString
		var approvedAmount, approvedRate, totalAmount sql.NullFloat64
		if err := rows.Scan(
			&app.ID, &rawPublicID, &app.CustomerPIN, &app.CustomerFullName, &app.Amount,
			&app.TermMonths, &loanPurpose, &app.Status, &creditLevel,
			&approvedAmount, &approvedRate, &totalAmount,
			&discountCode, &rejectionReason, &processedByUsername,
			&app.CreatedAt, &app.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan application: %w", err)
		}
		app.PublicID = uuid.UUID(rawPublicID).String() // PR #194
		app.LoanPurpose = loanPurpose.String
		app.CreditLevel = creditLevel.String
		app.ApprovedAmount = approvedAmount.Float64
		app.ApprovedRate = approvedRate.Float64
		app.TotalAmount = totalAmount.Float64 // PR #224
		app.DiscountCode = discountCode.String
		app.RejectionReason = rejectionReason.String         // PR #313
		app.ProcessedByUsername = processedByUsername.String // PR #376
		apps = append(apps, app)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating applications: %w", err)
	}
	return apps, nil
}

// GetApplicationByPublicID fetches a loan application by its UUID public_id.
// PR #191: xarici API və UI public_id UUID istifadə edir.
// PR #192: UUID string mssql.UniqueIdentifier-a çevrilir (string → UNIQUEIDENTIFIER conversion xətası fix).
func (r *ApplicationRepo) GetApplicationByPublicID(ctx context.Context, publicID string) (*model.LoanApplication, error) {
	// Validate and parse the UUID string
	parsed, err := uuid.Parse(publicID)
	if err != nil {
		return nil, fmt.Errorf("invalid public_id format (not a valid UUID): %w", err)
	}

	// Convert to mssql.UniqueIdentifier for proper SQL Server comparison
	var mssqlUUID mssql.UniqueIdentifier
	copy(mssqlUUID[:], parsed[:])

	// Reuse GetApplicationByID by first looking up the INT id
	var id int
	err = r.db.QueryRowContext(ctx, `SELECT id FROM loan_applications WHERE public_id = ?`, mssqlUUID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup application by public_id: %w", err)
	}
	return r.GetApplicationByID(ctx, id)
}

// FindLatestByPINAndDate fetches the most recent application created on the
// given day for the given customer PIN. PR #427: LW partner video-url endpoint.
// day formatı yyyy-mm-dd-dir; CONVERT(date, created_at) müqayisəsi DB serverin
// LOKAL vaxtına görədir — app server başqa timezone-da işləsə belə gün
// sərhədi düzgün hesablanır (PIN filtrı seçici olduğundan CONVERT-in index
// istifadə etməməsi problema deyil).
// Eyni PIN + gündə bir neçə müraciət varsa ən yenisi (id DESC) qaytarılır.
// Tapılmayanda (nil, nil) qaytarır.
func (r *ApplicationRepo) FindLatestByPINAndDate(ctx context.Context, pin, day string) (*model.LoanApplication, error) {
	row := r.db.QueryRowContext(ctx, `
                		SELECT TOP 1 id, public_id, customer_pin, created_at
                		FROM loan_applications
                		WHERE customer_pin = ? AND CONVERT(date, created_at) = ?
                		ORDER BY id DESC`, pin, day)

	var app model.LoanApplication
	var rawPublicID mssql.UniqueIdentifier
	if err := row.Scan(&app.ID, &rawPublicID, &app.CustomerPIN, &app.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup application by pin+date: %w", err)
	}
	app.PublicID = uuid.UUID(rawPublicID).String() // PR #194
	return &app, nil
}

// FindLatestAppIDByPINWithRecordedVideo — PR #432: PIN-lə axtarış (tarixsiz).
// Həmin PIN-in ÇƏKİLMİŞ (recorded=1) videolu ƏN SON müraciətinin ID-sini
// qaytarır. Yeni müraciətlərdə video hələ çəkilməyibsə onlar ötürülür —
// LW "müştərinin videosunu göstər" sorğusunda ən son İZLƏNİLƏ BİLƏN videonu
// alır. Tapılmayanda 0 qaytarır.
func (r *ApplicationRepo) FindLatestAppIDByPINWithRecordedVideo(ctx context.Context, pin string) (int, error) {
	var appID int
	err := r.db.QueryRowContext(ctx, `
		SELECT TOP 1 a.id
		FROM loan_applications a
		WHERE a.customer_pin = ?
		  AND EXISTS (SELECT 1 FROM video_records v
		              WHERE v.application_id = a.id AND v.recorded = 1)
		ORDER BY a.id DESC`, pin).Scan(&appID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to lookup application by pin (recorded video): %w", err)
	}
	return appID, nil
}
