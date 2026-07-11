package model

// Customer represents a registered customer in the system.
// Each customer has a unique PIN (FIN code) and a single profile.
// Customer info is stored here, not duplicated per application.
type Customer struct {
	ID            int    `json:"id"`
	CustomerPIN   string `json:"customer_pin"`           // FIN code, unique
	FullName      string `json:"full_name"`
	Phone         string `json:"phone,omitempty"`        // primary phone (verified via OTP)
	Email         string `json:"email,omitempty"`
	ActualAddress string `json:"actual_address,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// CreateOrUpdateCustomerRequest is used to find or create a customer.
type CreateOrUpdateCustomerRequest struct {
	CustomerPIN   string `json:"customer_pin"`
	FullName      string `json:"full_name"`
	Phone         string `json:"phone,omitempty"`
	Email         string `json:"email,omitempty"`
	ActualAddress string `json:"actual_address,omitempty"`
}
