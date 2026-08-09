package service

import (
        "context"
        "errors"
        "fmt"
        "log/slog"
        "net/http"
        "time"

        "github.com/google/uuid"
        "golang.org/x/crypto/bcrypt"

        "rdc-source/internal/model"
)

// UserStore is the persistence interface for auth users and sessions.
// The concrete implementation is *repository.UserRepo.
type UserStore interface {
        GetByUsername(ctx context.Context, username string) (*model.User, error)
        GetByID(ctx context.Context, id int) (*model.User, error)
        List(ctx context.Context) ([]model.User, error)
        Create(ctx context.Context, u *model.User) error
        UpdatePassword(ctx context.Context, userID int, passwordHash string) error
        UpdateRole(ctx context.Context, userID int, role string) error
        SetActive(ctx context.Context, userID int, active bool) error
        SetLock(ctx context.Context, userID int, locked bool, lockedUntil *time.Time) error
        IncrementFailedAttempts(ctx context.Context, userID int, maxAttempts int, lockDuration time.Duration) error
        ResetFailedAttempts(ctx context.Context, userID int) error
        UpdateLastLogin(ctx context.Context, userID int) error
        Delete(ctx context.Context, userID int) error
        CountAdmins(ctx context.Context) (int, error)

        CreateSession(ctx context.Context, s *model.Session) error
        GetSessionByToken(ctx context.Context, token string) (*model.Session, *model.User, error)
        DeleteSession(ctx context.Context, token string) error
}

// AuthConfig holds auth-related configuration values.
type AuthConfig struct {
        SessionTTL      time.Duration // how long a session token is valid
        MaxFailedAttempts int         // lock after this many failed logins
        LockDuration    time.Duration // how long to lock after too many failures
        BcryptCost      int           // bcrypt cost factor (10-14)
}

// DefaultAuthConfig returns sensible defaults for auth configuration.
func DefaultAuthConfig() AuthConfig {
        return AuthConfig{
                SessionTTL:        8 * time.Hour,
                MaxFailedAttempts: 5,
                LockDuration:      15 * time.Minute,
                BcryptCost:        12,
        }
}

// AuthService handles authentication, session management, and user CRUD.
type AuthService struct {
        repo  UserStore
        cfg   AuthConfig
}

// NewAuthService creates a new AuthService.
func NewAuthService(repo UserStore, cfg AuthConfig) *AuthService {
        if cfg.BcryptCost == 0 {
                cfg = DefaultAuthConfig()
        }
        return &AuthService{repo: repo, cfg: cfg}
}

// LoginResult is returned by Login on success.
type LoginResult struct {
        Token    string       `json:"token"`
        User     *model.User  `json:"user"`
        ExpiresAt time.Time   `json:"expires_at"`
}

// Login validates credentials and creates a new session.
// Returns ErrInvalidCredentials if the username/password is wrong,
// ErrUserLocked if the account is auto-locked, ErrUserInactive if deactivated.
func (s *AuthService) Login(ctx context.Context, username, password, ipAddress, userAgent string) (*LoginResult, error) {
        user, err := s.repo.GetByUsername(ctx, username)
        if err != nil {
                return nil, fmt.Errorf("failed to query user: %w", err)
        }
        if user == nil {
                return nil, ErrInvalidCredentials
        }

        if !user.IsActive {
                return nil, ErrUserInactive
        }

        if user.IsLocked {
                // Check if lock has expired
                if user.LockedUntil != nil && time.Now().After(*user.LockedUntil) {
                        // Lock expired — reset
                        if err := s.repo.ResetFailedAttempts(ctx, user.ID); err != nil {
                                return nil, fmt.Errorf("failed to reset lock: %w", err)
                        }
                        user.IsLocked = false
                        user.FailedLoginAttempts = 0
                } else {
                        return nil, fmt.Errorf("%w (cəhd edə bilərsiniz: %s)", ErrUserLocked, user.LockedUntil.Format("15:04:05"))
                }
        }

        // Check password
        if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
                // Increment failed attempts
                if err := s.repo.IncrementFailedAttempts(ctx, user.ID, s.cfg.MaxFailedAttempts, s.cfg.LockDuration); err != nil {
                        slog.Error("failed to increment failed attempts", "user_id", user.ID, "error", err)
                }
                return nil, ErrInvalidCredentials
        }

        // Success — reset failed attempts and update last login
        if err := s.repo.ResetFailedAttempts(ctx, user.ID); err != nil {
                slog.Error("failed to reset failed attempts", "user_id", user.ID, "error", err)
        }
        if err := s.repo.UpdateLastLogin(ctx, user.ID); err != nil {
                slog.Error("failed to update last login", "user_id", user.ID, "error", err)
        }

        // Create session
        token := uuid.NewString()
        expiresAt := time.Now().Add(s.cfg.SessionTTL)
        session := &model.Session{
                UserID:    user.ID,
                Token:     token,
                ExpiresAt: expiresAt,
                IPAddress: ipAddress,
                UserAgent: userAgent,
        }
        if err := s.repo.CreateSession(ctx, session); err != nil {
                return nil, fmt.Errorf("failed to create session: %w", err)
        }

        return &LoginResult{
                Token:     token,
                User:      user,
                ExpiresAt: expiresAt,
        }, nil
}

// Logout deletes the session associated with the given token.
func (s *AuthService) Logout(ctx context.Context, token string) error {
        return s.repo.DeleteSession(ctx, token)
}

// ValidateSession checks if a token is valid and returns the associated user.
// Returns nil user (no error) if the token is invalid or expired.
func (s *AuthService) ValidateSession(ctx context.Context, token string) (*model.User, error) {
        _, user, err := s.repo.GetSessionByToken(ctx, token)
        if err != nil {
                return nil, fmt.Errorf("failed to validate session: %w", err)
        }
        return user, nil // may be nil if not found
}

// --- User CRUD (admin operations) ---

// CreateUserRequest is the input for creating a new user.
type CreateUserRequest struct {
        Username string `json:"username"`
        Password string `json:"password"`
        Role     string `json:"role"` // "admin" or "expert"
}

// CreateUser creates a new user (admin operation).
func (s *AuthService) CreateUser(ctx context.Context, req *CreateUserRequest) (*model.User, error) {
        if req.Username == "" || len(req.Username) < 3 {
                return nil, errors.New("istifadəçi adı ən az 3 simvol olmalıdır")
        }
        if len(req.Password) < 6 {
                return nil, errors.New("şifrə ən az 6 simvol olmalıdır")
        }
        if !model.IsValidRole(req.Role) {
                return nil, errors.New("rol yalnız 'admin' və ya 'expert' ola bilər")
        }

        // Check if username already exists
        existing, err := s.repo.GetByUsername(ctx, req.Username)
        if err != nil {
                return nil, fmt.Errorf("failed to check existing user: %w", err)
        }
        if existing != nil {
                return nil, errors.New("bu istifadəçi adı artıq mövcuddur")
        }

        hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BcryptCost)
        if err != nil {
                return nil, fmt.Errorf("failed to hash password: %w", err)
        }

        user := &model.User{
                Username:     req.Username,
                PasswordHash: string(hash),
                Role:         req.Role,
                IsActive:     true,
        }
        if err := s.repo.Create(ctx, user); err != nil {
                return nil, fmt.Errorf("failed to create user: %w", err)
        }

        slog.Info("user created", "user_id", user.ID, "username", user.Username, "role", user.Role)
        return user, nil
}

// ListUsers returns all users (admin operation).
func (s *AuthService) ListUsers(ctx context.Context) ([]model.User, error) {
        return s.repo.List(ctx)
}

// ChangePassword changes the password for the given user ID.
// oldPassword is verified against the current hash before updating.
func (s *AuthService) ChangePassword(ctx context.Context, userID int, oldPassword, newPassword string) error {
        if len(newPassword) < 6 {
                return errors.New("yeni şifrə ən az 6 simvol olmalıdır")
        }

        user, err := s.repo.GetByID(ctx, userID)
        if err != nil {
                return fmt.Errorf("failed to get user: %w", err)
        }
        if user == nil {
                return errors.New("istifadəçi tapılmadı")
        }

        if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
                return errors.New("köhnə şifrə yanlışdır")
        }

        hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cfg.BcryptCost)
        if err != nil {
                return fmt.Errorf("failed to hash password: %w", err)
        }

        if err := s.repo.UpdatePassword(ctx, userID, string(hash)); err != nil {
                return fmt.Errorf("failed to update password: %w", err)
        }

        slog.Info("password changed", "user_id", userID)
        return nil
}

// AdminResetPassword resets a user's password (admin operation, no old password needed).
func (s *AuthService) AdminResetPassword(ctx context.Context, userID int, newPassword string) error {
        if len(newPassword) < 6 {
                return errors.New("yeni şifrə ən az 6 simvol olmalıdır")
        }

        hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cfg.BcryptCost)
        if err != nil {
                return fmt.Errorf("failed to hash password: %w", err)
        }

        if err := s.repo.UpdatePassword(ctx, userID, string(hash)); err != nil {
                return fmt.Errorf("failed to reset password: %w", err)
        }

        slog.Info("admin reset user password", "user_id", userID)
        return nil
}

// UpdateRole changes a user's role (admin operation).
func (s *AuthService) UpdateRole(ctx context.Context, userID int, role string) error {
        if !model.IsValidRole(role) {
                return errors.New("rol yalnız 'admin' və ya 'expert' ola bilər")
        }
        return s.repo.UpdateRole(ctx, userID, role)
}

// SetActive toggles a user's active status (admin operation).
func (s *AuthService) SetActive(ctx context.Context, userID int, active bool) error {
        return s.repo.SetActive(ctx, userID, active)
}

// LockUser manually locks a user (admin operation).
func (s *AuthService) LockUser(ctx context.Context, userID int) error {
        return s.repo.SetLock(ctx, userID, true, nil) // nil = indefinite until admin unlocks
}

// UnlockUser clears the lock state (admin operation).
func (s *AuthService) UnlockUser(ctx context.Context, userID int) error {
        return s.repo.SetLock(ctx, userID, false, nil)
}

// DeleteUser removes a user. Prevents deleting the last admin or self-deletion.
func (s *AuthService) DeleteUser(ctx context.Context, userID int, currentUserID int) error {
        if userID == currentUserID {
                return errors.New("özünüzü silmək olmaz")
        }

        user, err := s.repo.GetByID(ctx, userID)
        if err != nil {
                return fmt.Errorf("failed to get user: %w", err)
        }
        if user == nil {
                return errors.New("istifadəçi tapılmadı")
        }

        // Prevent deleting the last active admin
        if user.Role == model.RoleAdmin && user.IsActive {
                adminCount, err := s.repo.CountAdmins(ctx)
                if err != nil {
                        return fmt.Errorf("failed to count admins: %w", err)
                }
                if adminCount <= 1 {
                        return errors.New("son admin istifadəçisini silmək olmaz")
                }
        }

        return s.repo.Delete(ctx, userID)
}

// EnsureAdminUser creates the default admin user if no users exist.
// Called on app startup. The password comes from ADMIN_INITIAL_PASSWORD env var
// (default: "admin123" — a warning is logged in this case).
func (s *AuthService) EnsureAdminUser(ctx context.Context, initialPassword string) error {
        users, err := s.repo.List(ctx)
        if err != nil {
                return fmt.Errorf("failed to list users: %w", err)
        }
        if len(users) > 0 {
                return nil // users already exist, skip seeding
        }

        if initialPassword == "" {
                initialPassword = "admin123"
                slog.Warn("ADMIN_INITIAL_PASSWORD not set — using default 'admin123'. CHANGE IT IMMEDIATELY after first login!")
        }

        username := "admin"
        hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), s.cfg.BcryptCost)
        if err != nil {
                return fmt.Errorf("failed to hash admin password: %w", err)
        }

        admin := &model.User{
                Username:     username,
                PasswordHash: string(hash),
                Role:         model.RoleAdmin,
                IsActive:     true,
        }
        if err := s.repo.Create(ctx, admin); err != nil {
                return fmt.Errorf("failed to create admin user: %w", err)
        }

        slog.Info("default admin user created",
                "user_id", admin.ID,
                "username", admin.Username,
                "role", admin.Role,
                "warning", "change the default password immediately after first login")
        return nil
}

// ExtractToken pulls the session token from a request.
// Checks the cookie "rdc_session" first, then the Authorization: Bearer header.
func ExtractToken(r *http.Request) string {
        // 1. Cookie
        if cookie, err := r.Cookie("rdc_session"); err == nil && cookie.Value != "" {
                return cookie.Value
        }
        // 2. Authorization header
        auth := r.Header.Get("Authorization")
        if len(auth) > 7 && auth[:7] == "Bearer " {
                return auth[7:]
        }
        return ""
}

// SetSessionCookie sets the rdc_session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
        http.SetCookie(w, &http.Cookie{
                Name:     "rdc_session",
                Value:    token,
                Path:     "/",
                Expires:  expiresAt,
                MaxAge:   int(time.Until(expiresAt).Seconds()),
                HttpOnly: true,
                Secure:   false, // set to true in production behind TLS proxy
                SameSite: http.SameSiteLaxMode,
        })
}

// ClearSessionCookie clears the rdc_session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
        http.SetCookie(w, &http.Cookie{
                Name:     "rdc_session",
                Value:    "",
                Path:     "/",
                MaxAge:   -1,
                HttpOnly: true,
                SameSite: http.SameSiteLaxMode,
        })
}

// --- Sentinel errors ---

var (
        ErrInvalidCredentials = errors.New("istifadəçi adı və ya şifrə yanlışdır")
        ErrUserLocked         = errors.New("hesab bloklanıb")
        ErrUserInactive       = errors.New("hesab deaktiv edilib")
)
