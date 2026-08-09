package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"rdc-source/internal/middleware"
	"rdc-source/internal/model"
	"rdc-source/internal/service"
)

// UserHandler handles admin user management endpoints (CRUD, lock/unlock, reset password).
type UserHandler struct {
	authSvc *service.AuthService
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(authSvc *service.AuthService) *UserHandler {
	return &UserHandler{authSvc: authSvc}
}

// userResponse is the public user representation for the admin panel.
type userResponse struct {
	ID                  int    `json:"id"`
	Username            string `json:"username"`
	Role                string `json:"role"`
	IsActive            bool   `json:"is_active"`
	IsLocked            bool   `json:"is_locked"`
	FailedLoginAttempts int    `json:"failed_login_attempts"`
	LastLoginAt         string `json:"last_login_at,omitempty"`
	CreatedAt           string `json:"created_at"`
}

func toUserResponse(u model.User) userResponse {
	resp := userResponse{
		ID:                  u.ID,
		Username:            u.Username,
		Role:                u.Role,
		IsActive:            u.IsActive,
		IsLocked:            u.IsLocked,
		FailedLoginAttempts: u.FailedLoginAttempts,
		CreatedAt:           u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.LastLoginAt != nil {
		resp.LastLoginAt = u.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

// ListUsers handles GET /api/admin/users — lists all users.
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.authSvc.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "istifadəçilər siyahısı alınmadı")
		return
	}

	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateUser handles POST /api/admin/users — creates a new user.
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req service.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body: "+err.Error())
		return
	}

	user, err := h.authSvc.CreateUser(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toUserResponse(*user))
}

// UpdateRole handles PUT /api/admin/users/{id}/role — changes user role.
func (h *UserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body")
		return
	}

	if err := h.authSvc.UpdateRole(r.Context(), id, req.Role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("admin updated user role", "user_id", id, "role", req.Role)
	writeJSON(w, http.StatusOK, map[string]string{"message": "rol yeniləndi"})
}

// SetActive handles PUT /api/admin/users/{id}/active — activates/deactivates user.
func (h *UserHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body")
		return
	}

	// Prevent self-deactivation
	currentUser := middleware.PrincipalFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id && !req.Active {
		writeError(w, http.StatusBadRequest, "özünüzü deaktiv edə bilməzsiniz")
		return
	}

	if err := h.authSvc.SetActive(r.Context(), id, req.Active); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("admin set user active", "user_id", id, "active", req.Active)
	writeJSON(w, http.StatusOK, map[string]string{"message": "status yeniləndi"})
}

// LockUser handles PUT /api/admin/users/{id}/lock — manually locks user.
func (h *UserHandler) LockUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Prevent self-lock
	currentUser := middleware.PrincipalFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id {
		writeError(w, http.StatusBadRequest, "özünüzü bloklaya bilməzsiniz")
		return
	}

	if err := h.authSvc.LockUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("admin locked user", "user_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "istifadəçi bloklandı"})
}

// UnlockUser handles PUT /api/admin/users/{id}/unlock — clears lock state.
func (h *UserHandler) UnlockUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.authSvc.UnlockUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	slog.Info("admin unlocked user", "user_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "blokdan çıxarıldı"})
}

// ResetPassword handles PUT /api/admin/users/{id}/password — admin resets user password.
func (h *UserHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body")
		return
	}

	if err := h.authSvc.AdminResetPassword(r.Context(), id, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("admin reset user password", "user_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "şifrə sıfırlandı"})
}

// DeleteUser handles DELETE /api/admin/users/{id} — removes user.
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	currentUser := middleware.PrincipalFromContext(r.Context())
	if currentUser == nil {
		writeError(w, http.StatusUnauthorized, "giriş edilməyib")
		return
	}

	if err := h.authSvc.DeleteUser(r.Context(), id, currentUser.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("admin deleted user", "user_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "istifadəçi silindi"})
}

// parseUserID extracts and validates the user ID from the URL path.
func parseUserID(r *http.Request) (int, error) {
	raw := r.PathValue("id")
	if raw == "" {
		return 0, errInvalidUserID
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, errInvalidUserID
	}
	return id, nil
}

var errInvalidUserID = &simpleError{"yanlış istifadəçi ID"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
