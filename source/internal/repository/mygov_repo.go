package repository

import (
        "context"
        "database/sql"
        "fmt"
        "time"
)

// MyGovRepo handles database operations for MyGov permissions (T-4.9).
type MyGovRepo struct {
        db *sql.DB
}

// NewMyGovRepo creates a new MyGovRepo.
func NewMyGovRepo(db *sql.DB) *MyGovRepo {
        return &MyGovRepo{db: db}
}

// Create inserts a new MyGov permission record.
func (r *MyGovRepo) Create(ctx context.Context, appID int, customerPIN, token, url string, expiresAt time.Time) error {
        _, err := r.db.ExecContext(ctx, `
                INSERT INTO mygov_permissions (application_id, customer_pin, permission_token, link_url, link_expires_at, status)
                VALUES (?, ?, ?, ?, ?, 'pending')`,
                appID, customerPIN, token, url, expiresAt)
        if err != nil {
                return fmt.Errorf("failed to insert MyGov permission: %w", err)
        }
        return nil
}

// CreateWithDeeplink inserts a MyGov permission with nonce, state, and deeplink.
func (r *MyGovRepo) CreateWithDeeplink(ctx context.Context, appID int, customerPIN, nonce, state, deeplink string, expiresAt time.Time) error {
        _, err := r.db.ExecContext(ctx, `
                INSERT INTO mygov_permissions (application_id, customer_pin, permission_token, link_url, link_expires_at, nonce, state, deeplink, expires_at, status)
                VALUES (?, ?, '', ?, ?, ?, ?, ?, ?, 'pending')`,
                appID, customerPIN, deeplink, expiresAt, expiresAt, nonce, state, deeplink, expiresAt)
        if err != nil {
                return fmt.Errorf("failed to insert MyGov permission with deeplink: %w", err)
        }
        return nil
}

// GetByApplicationID retrieves the most recent MyGov permission for an application.
func (r *MyGovRepo) GetByApplicationID(ctx context.Context, appID int) (*MyGovPermission, error) {
        var p MyGovPermission
        var dataFetchedAt sql.NullTime
        var dataJSON sql.NullString
        err := r.db.QueryRowContext(ctx, `
                SELECT TOP 1 id, application_id, customer_pin, permission_token, link_url,
                       link_expires_at, data_fetched_at, data_json, status, created_at
                FROM mygov_permissions WHERE application_id = ?
                ORDER BY created_at DESC`,
                appID).Scan(
                &p.ID, &p.ApplicationID, &p.CustomerPIN, &p.PermissionToken, &p.LinkURL,
                &p.LinkExpiresAt, &dataFetchedAt, &dataJSON, &p.Status, &p.CreatedAt)
        if err != nil {
                return nil, err
        }
        if dataFetchedAt.Valid {
                p.DataFetchedAt = &dataFetchedAt.Time
        }
        p.DataJSON = dataJSON.String
        return &p, nil
}

// UpdateData stores the fetched authorized data and marks status as 'fetched'.
func (r *MyGovRepo) UpdateData(ctx context.Context, appID int, dataJSON string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE mygov_permissions
                SET data_json = ?, data_fetched_at = GETDATE(), status = 'fetched'
                WHERE application_id = ? AND status = 'pending'`,
                dataJSON, appID)
        if err != nil {
                return fmt.Errorf("failed to update MyGov data: %w", err)
        }
        return nil
}

// UpdateStatus updates the status of a MyGov permission.
func (r *MyGovRepo) UpdateStatus(ctx context.Context, appID int, status string) error {
        _, err := r.db.ExecContext(ctx, `
                UPDATE mygov_permissions SET status = ? WHERE application_id = ?`,
                status, appID)
        if err != nil {
                return fmt.Errorf("failed to update MyGov status: %w", err)
        }
        return nil
}

// MyGovPermission represents a MyGov permission record (alias for the model type).
type MyGovPermission = struct {
        ID              int
        ApplicationID   int
        CustomerPIN     string
        PermissionToken string
        LinkURL         string
        LinkExpiresAt   time.Time
        DataFetchedAt   *time.Time
        DataJSON        string
        Status          string
        CreatedAt       time.Time
}
