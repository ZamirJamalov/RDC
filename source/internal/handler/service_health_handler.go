package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"rdc-source/internal/model"
	"rdc-source/internal/repository"
)

// ServiceHealthHandler — PR #421: xarici servis sağlamlıq paneli.
// GET /api/admin/service-health (PR #422: YALNIZ ADMIN — ekspert görmür) —
// dashboard-da hər xarici servisin statusu: son uğurlu/uğursuz çağırış,
// gecikmə, 24 saatlıq statistika.
// Mənbə: service_audit_logs cədvəli (bir aqreqasiya sorğu).
type ServiceHealthHandler struct {
	repo *repository.ServiceAuditLogRepo
}

func NewServiceHealthHandler(repo *repository.ServiceAuditLogRepo) *ServiceHealthHandler {
	return &ServiceHealthHandler{repo: repo}
}

// GetServiceHealth handles GET /api/admin/service-health?hours=24
func (h *ServiceHealthHandler) GetServiceHealth(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			hours = parsed
		}
	}

	healths, err := h.repo.GetServiceHealth(r.Context(), hours)
	if err != nil {
		slog.Error("service health query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "servis sağlamlığı əldə edilə bilmədi")
		return
	}
	if healths == nil {
		healths = []model.ServiceHealth{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": healths,
		"count":    len(healths),
		"hours":    hours,
	})
}
