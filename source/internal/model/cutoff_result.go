package model

import "time"

// CutoffResult represents a single cutoff check result for an application.
// PR #168: Hər müraciət üzrə plan/fakt nəticələri.
type CutoffResult struct {
        ID                 int       `json:"id"`
        ApplicationID      int       `json:"application_id"`
        CutoffCode         string    `json:"cutoff_code"`
        CutoffName         string    `json:"cutoff_name"`
        ServiceName        string    `json:"service_name"`
        Checked            bool      `json:"checked"`
        Passed             bool      `json:"passed"`
        ActualValue        string    `json:"actual_value"`
        Threshold          string    `json:"threshold"`
        Details            string    `json:"details"`
        CalculationDetails string    `json:"calculation_details"`
        CreatedAt          time.Time `json:"created_at"`
}
