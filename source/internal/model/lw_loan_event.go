package model

import "time"

// LWLoanEvent represents a single event from the LW loan lifecycle
// (contract signed, money transferred, failure, etc.).
type LWLoanEvent struct {
	ID            int
	ApplicationID int
	EventStatus   string // pending, contract_signed, transfer_completed, failed
	LmsLoanID     string
	Detail        string
	EventAt       time.Time
}

// LWLoanStatus constants.
const (
	LWLoanStatusPending           = "pending"
	LWLoanStatusContractSigned    = "contract_signed"
	LWLoanStatusTransferCompleted = "transfer_completed"
	LWLoanStatusFailed            = "failed"
)
