package model

import "time"

// ServiceHealth — PR #421: xarici servis sağlamlıq göstəricisi.
// service_audit_logs cədvəlindən son N saatlıq aqreqasiya (bir GROUP BY sorğu).
type ServiceHealth struct {
	ServiceName    string     `json:"service_name"`     // AZMK_CARD, AZMK_PARTNER, VIDEO_RECORD_CREATE_ORDER, ...
	LastSuccessAt  *time.Time `json:"last_success_at"`  // son uğurlu çağırış (error boş)
	LastFailureAt  *time.Time `json:"last_failure_at"`  // son uğursuz çağırış (error dolu)
	TotalCalls     int        `json:"total_calls"`      // pəncərədəki ümumi çağırış
	OkCalls        int        `json:"ok_calls"`         // uğurlu çağırış
	FailedCalls    int        `json:"failed_calls"`     // uğursuz çağırış
	AvgDurationMs  int        `json:"avg_duration_ms"`  // orta gecikmə (ms)
	LastDurationMs int        `json:"last_duration_ms"` // son çağırışın gecikməsi (ms)

	// Status computed (handler-də): "ok" | "error" | "unknown"
	//   ok      — son uğursuzluqdan sonra uğur var (və ya xəta heç olmayıb)
	//   error   — ən son çağırış xəta ilə bitib (uğur ondan əvvəl)
	//   unknown — pəncərədə çağırış yoxdur
	Status string `json:"status"`
}

// ComputeStatus fills Status from the timestamps.
func (h *ServiceHealth) ComputeStatus() {
	switch {
	case h.LastSuccessAt == nil && h.LastFailureAt == nil:
		h.Status = "unknown"
	case h.LastFailureAt == nil:
		h.Status = "ok"
	case h.LastSuccessAt == nil:
		h.Status = "error"
	case h.LastSuccessAt.After(*h.LastFailureAt):
		h.Status = "ok"
	default:
		h.Status = "error"
	}
}
