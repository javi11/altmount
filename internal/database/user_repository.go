package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// UserRepository handles user database operations
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// GetUserByID retrieves a user by their unique user ID
func (r *UserRepository) GetUserByID(ctx context.Context, userID string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE user_id = ?
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
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
func (r *UserRepository) GetUserByProvider(ctx context.Context, provider, providerID string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE provider = ? AND provider_id = ?
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, provider, providerID).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
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
func (r *UserRepository) CreateUser(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (user_id, email, name, avatar_url, provider, provider_id, password_hash, api_key, is_admin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		user.UserID, user.Email, user.Name, user.AvatarURL,
		user.Provider, user.ProviderID, user.PasswordHash, user.APIKey, user.IsAdmin,
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

// UpdateLastLogin updates the user's last login timestamp
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID string) error {
	query := `
		UPDATE users
		SET last_login = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.ExecContext(ctx, query, userID)
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

// GetUserCount returns the total number of users
func (r *UserRepository) GetUserCount(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users`

	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get user count: %w", err)
	}

	return count, nil
}

// GetUserByEmail retrieves a user by their email address for direct authentication
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE email = ? AND provider = 'direct'
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
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
func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE user_id = ? AND provider = 'direct'
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
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
func (r *UserRepository) UpdatePassword(ctx context.Context, userID string, passwordHash string) error {
	query := `
		UPDATE users
		SET password_hash = ?, updated_at = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.ExecContext(ctx, query, passwordHash, userID)
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

// generateAPIKey generates a cryptographically secure API key
func (r *UserRepository) generateAPIKey() (string, error) {
	// Generate 24 random bytes (will become 32 characters in base64)
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode to URL-safe base64 and remove padding
	apiKey := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)
	return apiKey, nil
}

// RegenerateAPIKey generates and updates a new API key for the user
func (r *UserRepository) RegenerateAPIKey(ctx context.Context, userID string) (string, error) {
	// Generate new API key
	apiKey, err := r.generateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	// Update user's API key in database
	query := `
		UPDATE users
		SET api_key = ?, updated_at = datetime('now')
		WHERE user_id = ?
	`

	result, err := r.db.ExecContext(ctx, query, apiKey, userID)
	if err != nil {
		return "", fmt.Errorf("failed to update API key: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return "", fmt.Errorf("user not found: %s", userID)
	}

	return apiKey, nil
}

// GetUserByAPIKey retrieves a user by their API key
func (r *UserRepository) GetUserByAPIKey(ctx context.Context, apiKey string) (*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE api_key = ?
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, apiKey).Scan(
		&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
		&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by API key: %w", err)
	}

	return &user, nil
}

// GetAllUsers retrieves all users with API keys for authentication purposes
func (r *UserRepository) GetAllUsers(ctx context.Context) ([]*User, error) {
	query := `
		SELECT id, user_id, email, name, avatar_url, provider, provider_id,
		       password_hash, api_key, is_admin, created_at, updated_at, last_login
		FROM users
		WHERE api_key IS NOT NULL AND api_key != ''
		ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		err := rows.Scan(
			&user.ID, &user.UserID, &user.Email, &user.Name, &user.AvatarURL,
			&user.Provider, &user.ProviderID, &user.PasswordHash, &user.APIKey, &user.IsAdmin,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLogin,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}
