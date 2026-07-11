package repository

import (
	"context"
	"database/sql"
	"fmt"

	"rdc-source/internal/model"
)

// CustomerRepo handles database operations for customers.
type CustomerRepo struct {
	db *sql.DB
}

// NewCustomerRepo creates a new CustomerRepo.
func NewCustomerRepo(db *sql.DB) *CustomerRepo {
	return &CustomerRepo{db: db}
}

// GetByPIN retrieves a customer by their PIN (FIN code).
// Returns sql.ErrNoRows if no customer with that PIN exists.
func (r *CustomerRepo) GetByPIN(ctx context.Context, pin string) (*model.Customer, error) {
	var c model.Customer
	err := r.db.QueryRowContext(ctx, `
		SELECT id, customer_pin, full_name, phone, email, actual_address, created_at, updated_at
		FROM customers WHERE customer_pin = ?`, pin).Scan(
		&c.ID,
		&c.CustomerPIN,
		&c.FullName,
		&c.Phone,
		&c.Email,
		&c.ActualAddress,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Create inserts a new customer record and sets the ID on the struct.
func (r *CustomerRepo) Create(ctx context.Context, c *model.Customer) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO customers (customer_pin, full_name, phone, email, actual_address)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (?, ?, ?, ?, ?)`,
		c.CustomerPIN,
		c.FullName,
		c.Phone,
		c.Email,
		c.ActualAddress,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert customer: %w", err)
	}
	return nil
}

// GetOrCreate fetches a customer by PIN. If the customer does not exist,
// it creates a new one with the given info and returns it.
// This is the main entry point used by ApplicationService when creating
// an application — we always want a customer record before the application.
func (r *CustomerRepo) GetOrCreate(ctx context.Context, c *model.Customer) error {
	// Try to fetch existing customer first
	existing, err := r.GetByPIN(ctx, c.CustomerPIN)
	if err == nil {
		// Customer exists — copy DB values to the struct and return
		*c = *existing
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to lookup customer: %w", err)
	}
	// Customer doesn't exist — create
	return r.Create(ctx, c)
}

// UpdatePhone sets the verified phone number on a customer record.
// Called after successful OTP verification.
func (r *CustomerRepo) UpdatePhone(ctx context.Context, id int, phone string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customers SET phone = ?, updated_at = GETDATE() WHERE id = ?`,
		phone, id)
	if err != nil {
		return fmt.Errorf("failed to update customer phone: %w", err)
	}
	return nil
}

// LinkApplication sets customer_id on a loan application record.
// Called after creating both the customer and the application.
func (r *CustomerRepo) LinkApplication(ctx context.Context, appID, customerID int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE loan_applications SET customer_id = ? WHERE id = ?`,
		customerID, appID)
	if err != nil {
		return fmt.Errorf("failed to link application to customer: %w", err)
	}
	return nil
}
