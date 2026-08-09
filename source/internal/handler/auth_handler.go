package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"rdc-source/internal/middleware"
	"rdc-source/internal/service"
)

// AuthHandler handles authentication endpoints (login, logout, me, change-password).
type AuthHandler struct {
	authSvc *service.AuthService
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(authSvc *service.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

// loginRequest is the body for POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is the successful login response.
type loginResponse struct {
	Token     string      `json:"token"`
	User      userInfo    `json:"user"`
	ExpiresAt string      `json:"expires_at"`
}

// userInfo is the public user representation (no password hash).
type userInfo struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	IsActive     bool   `json:"is_active"`
	LastLoginAt  string `json:"last_login_at,omitempty"`
}

// Login handles POST /api/auth/login.
// Validates credentials, creates a session, sets the rdc_session cookie.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body: "+err.Error())
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "istifadəçi adı və şifrə tələb olunur")
		return
	}

	ipAddress := r.RemoteAddr
	userAgent := r.UserAgent()

	result, err := h.authSvc.Login(r.Context(), req.Username, req.Password, ipAddress, userAgent)
	if err != nil {
		slog.Warn("login failed", "username", req.Username, "error", err)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	// Set session cookie
	service.SetSessionCookie(w, result.Token, result.ExpiresAt)

	resp := loginResponse{
		Token: result.Token,
		User: userInfo{
			ID:       result.User.ID,
			Username: result.User.Username,
			Role:     result.User.Role,
			IsActive: result.User.IsActive,
		},
		ExpiresAt: result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if result.User.LastLoginAt != nil {
		resp.User.LastLoginAt = result.User.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
	}

	slog.Info("user logged in", "user_id", result.User.ID, "username", result.User.Username)
	writeJSON(w, http.StatusOK, resp)
}

// Logout handles POST /api/auth/logout.
// Deletes the session and clears the cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := service.ExtractToken(r)
	if token != "" {
		if err := h.authSvc.Logout(r.Context(), token); err != nil {
			slog.Error("logout failed", "error", err)
		}
	}
	service.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"message": "çıkış edildi"})
}

// Me handles GET /api/auth/me.
// Returns the currently authenticated user's info.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.PrincipalFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "giriş edilməyib")
		return
	}

	resp := userInfo{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
		IsActive: user.IsActive,
	}
	if user.LastLoginAt != nil {
		resp.LastLoginAt = user.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
	}
	writeJSON(w, http.StatusOK, resp)
}

// changePasswordRequest is the body for POST /api/auth/change-password.
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword handles POST /api/auth/change-password.
// Allows the authenticated user to change their own password.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.PrincipalFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "giriş edilməyib")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "yanlış request body: "+err.Error())
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "köhnə və yeni şifrə tələb olunur")
		return
	}

	if err := h.authSvc.ChangePassword(r.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("user changed password", "user_id", user.ID, "username", user.Username)
	writeJSON(w, http.StatusOK, map[string]string{"message": "şifrə dəyişdirildi"})
}
