package database

import (
	"database/sql"
	"fmt"
	"time"
)

// UserRepository handles user database operations
type UserRepository struct {
	db interface {
		Exec(query string, args ...interface{}) (sql.Result, error)
		Query(query string, args ...interface{}) (*sql.Rows, error)
		QueryRow(query string, args ...interface{}) *sql.Row
	}
}

// NewUserRepository creates a new user repository
func NewUserRepository(db interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}) *UserRepository {
	return &UserRepository{db: db}
}

// GetUserByID retrieves a user by their unique user ID
func (r *UserRepository) GetUserByID(userID string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id, 
		       password_hash, is_admin, created_at, updated_at, last_login
		FROM users 
		WHERE user_id = ?
	`

	var user User
	err := r.db.QueryRow(query, userID).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return &user, nil
}

// GetUserByProvider retrieves a user by provider and provider ID
func (r *UserRepository) GetUserByProvider(provider, providerID string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, is_admin, created_at, updated_at, last_login
		FROM users 
		WHERE provider = ? AND provider_id = ?
	`

	var user User
	err := r.db.QueryRow(query, provider, providerID).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by provider: %w", err)
	}

	return &user, nil
}

// CreateUser creates a new user account
func (r *UserRepository) CreateUser(user *User) error {
	query := `
		INSERT INTO users (user_id, email, name, avatar_url, provider, provider_id, password_hash, is_admin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query,
		user.UserID, user.Email, user.Name, user.AvatarURL,
		user.Provider, user.ProviderID, user.PasswordHash, user.IsAdmin,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get user ID: %w", err)
	}

	user.ID = id
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	return nil
}

// UpdateUser updates an existing user's information
func (r *UserRepository) UpdateUser(user *User) error {
	query := `
		UPDATE users 
		SET email = ?, name = ?, avatar_url = ?, updated_at = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.Exec(query, user.Email, user.Name, user.AvatarURL, user.UserID)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", user.UserID)
	}

	return nil
}

// UpdateLastLogin updates the user's last login timestamp
func (r *UserRepository) UpdateLastLogin(userID string) error {
	query := `
		UPDATE users 
		SET last_login = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// SetAdminStatus updates a user's admin status
func (r *UserRepository) SetAdminStatus(userID string, isAdmin bool) error {
	query := `
		UPDATE users 
		SET is_admin = ?, updated_at = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.Exec(query, isAdmin, userID)
	if err != nil {
		return fmt.Errorf("failed to set admin status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// ListUsers returns a list of all users with pagination
func (r *UserRepository) ListUsers(limit, offset int) ([]*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, is_admin, created_at, updated_at, last_login
		FROM users 
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		err := rows.Scan(
			&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
			&user.Provider, &user.ProviderID, &user.PasswordHash, &user.IsAdmin,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate users: %w", err)
	}

	return users, nil
}

// GetUserCount returns the total number of users
func (r *UserRepository) GetUserCount() (int, error) {
	query := `SELECT COUNT(*) FROM users`

	var count int
	err := r.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get user count: %w", err)
	}

	return count, nil
}

// DeleteUser deletes a user by their user ID
func (r *UserRepository) DeleteUser(userID string) error {
	query := `DELETE FROM users WHERE user_id = ?`

	result, err := r.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// GetUserByEmail retrieves a user by their email address for direct authentication
func (r *UserRepository) GetUserByEmail(email string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, is_admin, created_at, updated_at, last_login
		FROM users 
		WHERE email = ? AND provider = 'direct'
	`

	var user User
	err := r.db.QueryRow(query, email).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// GetUserByUsername retrieves a user by their username (user_id) for direct authentication
func (r *UserRepository) GetUserByUsername(username string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, is_admin, created_at, updated_at, last_login
		FROM users 
		WHERE user_id = ? AND provider = 'direct'
	`

	var user User
	err := r.db.QueryRow(query, username).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	return &user, nil
}

// UpdatePassword updates a user's password hash
func (r *UserRepository) UpdatePassword(userID string, passwordHash string) error {
	query := `
		UPDATE users 
		SET password_hash = ?, updated_at = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.Exec(query, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}