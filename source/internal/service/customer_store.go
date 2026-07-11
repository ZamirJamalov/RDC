package service

import (
	"context"

	"rdc-source/internal/model"
)

// CustomerStore is the interface used by ApplicationService to talk to the
// customer repository. The concrete *repository.CustomerRepo satisfies this
// interface structurally (Go duck typing).
//
// Defining the interface here (consumer package) follows Go idiom and keeps
// the service layer testable without a real DB connection.
type CustomerStore interface {
	// GetOrCreate fetches a customer by PIN. If not found, creates a new
	// record. The customer struct is updated with the DB-assigned ID and
	// timestamps.
	GetOrCreate(ctx context.Context, c *model.Customer) error

	// LinkApplication sets customer_id on a loan application record.
	// Best-effort: failures are logged but do not block application creation.
	LinkApplication(ctx context.Context, appID, customerID int) error

	// UpdatePhone sets the verified phone number on a customer record.
	// Called after successful OTP verification.
	UpdatePhone(ctx context.Context, id int, phone string) error
}
