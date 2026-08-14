package model

import "time"

// VideoRecord represents a video identity verification record for an application.
// PR #188: Kvadrat Lab video record service inteqrasiyası.
//
// Müştəri kredit təsdiq etməzdən əvvəl video identifikasiya keçməlidir.
// Hər müraciət üçün 1 video record order yaradılır, status poll olunur.
type VideoRecord struct {
	ID                  int       `json:"id"`
	ApplicationID       int       `json:"application_id"`
	AppIDExternal       string    `json:"app_id_external"`        // video service-ə göndərilən app_id
	OrderRedirectURL    string    `json:"order_redirect_url"`     // video service-dən qayıdan redirect_url
	Phone               string    `json:"phone"`
	Amount              float64   `json:"amount"`
	CustomerName        string    `json:"customer_name"`
	RequestBody         string    `json:"request_body"`            // raw JSON sent to video service
	ResponseBody        string    `json:"response_body"`           // raw JSON from video service
	StatusRequestBody   string    `json:"status_request_body"`     // raw status poll JSON
	StatusResponseBody  string    `json:"status_response_body"`    // raw status poll response JSON
	Recorded            bool      `json:"recorded"`                // video tamamlanıb?
	StatusCheckedAt     *time.Time `json:"status_checked_at"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// CreateVideoOrderRequest is the request body sent to the video record service.
// PR #188: POST {baseURL}/api/orders
type CreateVideoOrderRequest struct {
	AppID      string  `json:"app_id"`
	Phone      string  `json:"phone"`
	Amount     float64 `json:"amount"`
	WebhookURL string  `json:"webhook_url,omitempty"`
	RedirectURL string `json:"redirect_url,omitempty"`
	Name       string  `json:"name"`
	Lang       string  `json:"lang"`
	City       string  `json:"city,omitempty"`
	Address    string  `json:"address,omitempty"`
	Salary     float64 `json:"salary,omitempty"`
}

// CreateVideoOrderResponse is the response from the video record service.
type CreateVideoOrderResponse struct {
	Success     int    `json:"success"`      // 1 = success
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url"` // URL to embed in iframe
}

// VideoOrderStatusRequest is the polling request body.
// PR #188: POST {baseURL}/api/orders/status
type VideoOrderStatusRequest struct {
	AppIDs []string `json:"app_ids"`
}

// VideoOrderStatusResult is one entry in the status polling response.
type VideoOrderStatusResult struct {
	AppID    string `json:"app_id"`
	Recorded bool   `json:"recorded"`
}

// VideoOrderStatusResponse is the polling response.
type VideoOrderStatusResponse struct {
	Results []VideoOrderStatusResult `json:"results"`
}
