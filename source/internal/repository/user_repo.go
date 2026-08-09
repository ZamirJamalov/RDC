package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"rdc-source/internal/model"
)

// UserRepo handles database operations for dashboard users (auth).
type UserRepo struct {
	db *sql.DB
}

// NewUserRepo creates a new UserRepo with the given database connection.
func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

// GetByUsername fetches a user by their username.
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return r.queryUser(ctx, "WHERE username = ?", username)
}

// GetByID fetches a user by their primary key.
func (r *UserRepo) GetByID(ctx context.Context, id int) (*model.User, error) {
	return r.queryUser(ctx, "WHERE id = ?", id)
}

// List fetches all users ordered by ID (for admin panel).
func (r *UserRepo) List(ctx context.Context) ([]model.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, is_active, is_locked,
		       locked_until, failed_login_attempts, last_login_at, created_at, updated_at
		FROM users
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := scanUser(rows, &u); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// queryUser is a helper that fetches a single user with a WHERE clause.
func (r *UserRepo) queryUser(ctx context.Context, whereClause string, args ...interface{}) (*model.User, error) {
	var u model.User
	query := `
		SELECT id, username, password_hash, role, is_active, is_locked,
		       locked_until, failed_login_attempts, last_login_at, created_at, updated_at
		FROM users ` + whereClause
	row := r.db.QueryRowContext(ctx, query, args...)
	if err := scanUserRow(row, &u); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // user not found — not an error
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

// Create inserts a new user and sets the ID on the struct.
func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, role, is_active, is_locked)
		OUTPUT INSERTED.id, INSERTED.created_at, INSERTED.updated_at
		VALUES (?, ?, ?, ?, 0)`,
		u.Username, u.PasswordHash, u.Role, boolToBit(u.IsActive),
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// UpdatePassword sets a new password hash for the given user ID.
func (r *UserRepo) UpdatePassword(ctx context.Context, userID int, passwordHash string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = GETDATE() WHERE id = ?`,
		passwordHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	return nil
}

// UpdateRole sets the role for the given user ID.
func (r *UserRepo) UpdateRole(ctx context.Context, userID int, role string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET role = ?, updated_at = GETDATE() WHERE id = ?`,
		role, userID)
	if err != nil {
		return fmt.Errorf("failed to update role: %w", err)
	}
	return nil
}

// SetActive toggles the is_active flag for the given user ID.
func (r *UserRepo) SetActive(ctx context.Context, userID int, active bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET is_active = ?, updated_at = GETDATE() WHERE id = ?`,
		boolToBit(active), userID)
	if err != nil {
		return fmt.Errorf("failed to update active status: %w", err)
	}
	return nil
}

// SetLock sets the lock state for the given user ID.
// When locked=true, locked_until is set to the given expiry; when false, both are cleared.
func (r *UserRepo) SetLock(ctx context.Context, userID int, locked bool, lockedUntil *time.Time) error {
	if locked {
		_, err := r.db.ExecContext(ctx, `
			UPDATE users SET is_locked = 1, locked_until = ?, updated_at = GETDATE() WHERE id = ?`,
			lockedUntil, userID)
		if err != nil {
			return fmt.Errorf("failed to lock user: %w", err)
		}
	} else {
		_, err := r.db.ExecContext(ctx, `
			UPDATE users SET is_locked = 0, locked_until = NULL, failed_login_attempts = 0, updated_at = GETDATE() WHERE id = ?`,
			userID)
		if err != nil {
			return fmt.Errorf("failed to unlock user: %w", err)
		}
	}
	return nil
}

// IncrementFailedAttempts increments the failed login counter and auto-locks if threshold reached.
func (r *UserRepo) IncrementFailedAttempts(ctx context.Context, userID int, maxAttempts int, lockDuration time.Duration) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET failed_login_attempts = failed_login_attempts + 1,
		    is_locked = CASE WHEN failed_login_attempts + 1 >= ? THEN 1 ELSE is_locked END,
		    locked_until = CASE WHEN failed_login_attempts + 1 >= ? THEN DATEADD(second, ?, GETDATE()) ELSE locked_until END,
		    updated_at = GETDATE()
		WHERE id = ?`,
		maxAttempts, maxAttempts, int(lockDuration.Seconds()), userID)
	if err != nil {
		return fmt.Errorf("failed to increment failed attempts: %w", err)
	}
	return nil
}

// ResetFailedAttempts clears the failed login counter (called on successful login).
func (r *UserRepo) ResetFailedAttempts(ctx context.Context, userID int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET failed_login_attempts = 0, is_locked = 0, locked_until = NULL, updated_at = GETDATE() WHERE id = ?`,
		userID)
	if err != nil {
		return fmt.Errorf("failed to reset failed attempts: %w", err)
	}
	return nil
}

// UpdateLastLogin sets the last_login_at timestamp to now.
func (r *UserRepo) UpdateLastLogin(ctx context.Context, userID int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET last_login_at = GETDATE(), updated_at = GETDATE() WHERE id = ?`,
		userID)
	if err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}
	return nil
}

// Delete removes a user by ID (admin cannot delete themselves or the last admin).
func (r *UserRepo) Delete(ctx context.Context, userID int) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// CountAdmins returns the number of active admin users.
func (r *UserRepo) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = 1`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count admins: %w", err)
	}
	return count, nil
}

// --- Session methods ---

// CreateSession inserts a new session record.
func (r *UserRepo) CreateSession(ctx context.Context, s *model.Session) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sessions (user_id, token, expires_at, ip_address, user_agent)
		OUTPUT INSERTED.id, INSERTED.created_at
		VALUES (?, ?, ?, ?, ?)`,
		s.UserID, s.Token, s.ExpiresAt, s.IPAddress, s.UserAgent,
	).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

// GetSessionByToken fetches a session (and the associated user) by token.
// Returns nil if the token is not found or expired.
func (r *UserRepo) GetSessionByToken(ctx context.Context, token string) (*model.Session, *model.User, error) {
	var s model.Session
	var u model.User
	var ipAddr, userAgent sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.token, s.expires_at, s.ip_address, s.user_agent, s.created_at,
		       u.id, u.username, u.password_hash, u.role, u.is_active, u.is_locked,
		       u.locked_until, u.failed_login_attempts, u.last_login_at, u.created_at, u.updated_at
		FROM sessions s
		INNER JOIN users u ON s.user_id = u.id
		WHERE s.token = ? AND s.expires_at > GETDATE() AND u.is_active = 1 AND u.is_locked = 0`,
		token,
	).Scan(
		&s.ID, &s.UserID, &s.Token, &s.ExpiresAt, &ipAddr, &userAgent, &s.CreatedAt,
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsActive, &u.IsLocked,
		&u.LockedUntil, &u.FailedLoginAttempts, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil // session not found or expired
		}
		return nil, nil, fmt.Errorf("failed to get session: %w", err)
	}
	s.IPAddress = ipAddr.String
	s.UserAgent = userAgent.String
	return &s, &u, nil
}

// DeleteSession removes a session by token (logout).
func (r *UserRepo) DeleteSession(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes all expired sessions (cleanup, optional).
func (r *UserRepo) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < GETDATE()`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

// --- Helpers ---

func boolToBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scanUser scans a user row from sql.Rows.
func scanUser(rows *sql.Rows, u *model.User) error {
	var lockedUntil, lastLogin sql.NullTime
	err := rows.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsActive, &u.IsLocked,
		&lockedUntil, &u.FailedLoginAttempts, &lastLogin, &u.CreatedAt, &u.UpdatedAt,
	)
	if lockedUntil.Valid {
		u.LockedUntil = &lockedUntil.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return err
}

// scanUserRow scans a user row from sql.Row.
func scanUserRow(row *sql.Row, u *model.User) error {
	var lockedUntil, lastLogin sql.NullTime
	err := row.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsActive, &u.IsLocked,
		&lockedUntil, &u.FailedLoginAttempts, &lastLogin, &u.CreatedAt, &u.UpdatedAt,
	)
	if lockedUntil.Valid {
		u.LockedUntil = &lockedUntil.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return err
}
