package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-pkgz/auth/v2"
	"github.com/go-pkgz/auth/v2/avatar"
	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// Service handles authentication operations using go-pkgz/auth
type Service struct {
	authService *auth.Service
	userRepo    *database.UserRepository
	logger      *slog.Logger
}

// Config represents authentication service configuration
type Config struct {
	// JWT configuration
	JWTSecret      string        // JWT signing secret
	TokenDuration  time.Duration // JWT token duration
	CookieDomain   string        // Cookie domain
	CookieSecure   bool          // Secure cookie flag
	CookieSameSite http.SameSite // SameSite cookie attribute

	// Direct authentication
	DirectAuthEnabled bool   // Enable direct username/password authentication
	DirectAuthSalt    string // Salt for direct authentication

	// Application settings
	Issuer   string // JWT issuer
	Audience string // JWT audience
}

// DefaultConfig returns default authentication configuration
func DefaultConfig() *Config {
	return &Config{
		TokenDuration:     24 * time.Hour, // 24 hours
		CookieDomain:      "localhost",
		CookieSecure:      false, // true for production
		CookieSameSite:    http.SameSiteStrictMode,
		DirectAuthEnabled: true,
		Issuer:            "altmount",
		Audience:          "altmount-api",
	}
}

// LoadConfigFromEnv loads configuration from environment variables
func LoadConfigFromEnv() *Config {
	config := DefaultConfig()

	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		config.JWTSecret = secret
	} else {
		config.JWTSecret = "default-dev-secret-change-in-production"
	}

	if domain := os.Getenv("COOKIE_DOMAIN"); domain != "" {
		config.CookieDomain = domain
	}

	if secure := os.Getenv("COOKIE_SECURE"); secure == "true" {
		config.CookieSecure = true
	}

	if salt := os.Getenv("DIRECT_AUTH_SALT"); salt != "" {
		config.DirectAuthSalt = salt
	} else {
		// Generate a random salt for direct authentication
		config.DirectAuthSalt = generateRandomSalt()
	}

	if directAuth := os.Getenv("DIRECT_AUTH_ENABLED"); directAuth == "false" {
		config.DirectAuthEnabled = false
	}

	return config
}

// NewService creates a new authentication service
func NewService(config *Config, userRepo *database.UserRepository) (*Service, error) {
	if config == nil {
		config = LoadConfigFromEnv()
	}

	logger := slog.Default()

	// Create auth service options
	opts := auth.Opts{
		SecretReader: token.SecretFunc(func(string) (string, error) {
			return config.JWTSecret, nil
		}),
		TokenDuration:  config.TokenDuration,
		CookieDuration: config.TokenDuration,
		DisableXSRF:    false, // Enable XSRF protection
		SecureCookies:  config.CookieSecure,
		Issuer:         config.Issuer,
		URL:            "http://" + config.CookieDomain + ":8080",
		AvatarStore:    avatar.NewNoOp(), // No avatar storage for now
		ClaimsUpd: token.ClaimsUpdFunc(func(claims token.Claims) token.Claims {
			// Add audience
			if claims.Audience == nil {
				claims.Audience = []string{config.Audience}
			}
			return claims
		}),
	}

	authService := auth.NewService(opts)

	service := &Service{
		authService: authService,
		userRepo:    userRepo,
		logger:      logger,
	}

	return service, nil
}

// SetupProviders configures authentication providers
func (s *Service) SetupProviders(config *Config) error {
	// Direct authentication provider (username/password)
	if config.DirectAuthEnabled {
		s.authService.AddDirectProvider("altmount", &directCredChecker{service: s})
		s.logger.Info("Direct authentication provider configured")
	}

	return nil
}

// AuthService returns the underlying auth service
func (s *Service) AuthService() *auth.Service {
	return s.authService
}

// TokenService returns the token service for JWT operations
func (s *Service) TokenService() *token.Service {
	return s.authService.TokenService()
}

// CreateOrUpdateUser creates or updates a user based on token claims
func (s *Service) CreateOrUpdateUser(claims token.Claims) (*database.User, error) {
	// Extract user info from claims
	userID := claims.User.ID
	if userID == "" {
		userID = claims.Subject
	}

	// Check if user already exists
	existingUser, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	// Create user object with updated information
	user := &database.User{
		UserID:   userID,
		Provider: "direct", // Always use direct provider
		IsAdmin:  false,    // Default to false, can be updated separately
	}

	// Set name if available, otherwise use userID
	if claims.User.Name != "" {
		user.Name = &claims.User.Name
	} else {
		user.Name = &userID
	}

	// Set email if available
	if claims.User.Email != "" {
		user.Email = &claims.User.Email
	}

	// Set avatar URL if available
	if claims.User.Picture != "" {
		user.AvatarURL = &claims.User.Picture
	}

	if existingUser == nil {
		// Check if this is the first user - make them admin
		userCount, countErr := s.userRepo.GetUserCount()
		if countErr != nil {
			s.logger.Warn("Failed to get user count", "error", countErr)
		} else if userCount == 0 {
			user.IsAdmin = true
			s.logger.Info("First user registered - granting admin privileges", "user_id", userID)
		}

		// Create new user
		err = s.userRepo.CreateUser(user)
		if err != nil {
			return nil, err
		}
		s.logger.Info("Created new user", "user_id", userID, "is_admin", user.IsAdmin)
	} else {
		// Update existing user
		user.ID = existingUser.ID
		user.IsAdmin = existingUser.IsAdmin // Preserve admin status
		err = s.userRepo.UpdateUser(user)
		if err != nil {
			return nil, err
		}
		s.logger.Info("Updated existing user", "user_id", userID)
	}

	// Update last login
	err = s.userRepo.UpdateLastLogin(userID)
	if err != nil {
		s.logger.Warn("Failed to update last login", "user_id", userID, "error", err)
	}

	return user, nil
}

// GetUserFromToken extracts user information from JWT token
func (s *Service) GetUserFromToken(tokenStr string) (*database.User, error) {
	claims, err := s.authService.TokenService().Parse(tokenStr)
	if err != nil {
		return nil, err
	}

	userID := claims.User.ID
	if userID == "" {
		userID = claims.Subject
	}

	return s.userRepo.GetUserByID(userID)
}

// IsUserAdmin checks if a user has admin privileges
func (s *Service) IsUserAdmin(userID string) (bool, error) {
	user, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return user.IsAdmin, nil
}

// RegisterUser creates a new user with username and password
func (s *Service) RegisterUser(username, email, password string) (*database.User, error) {
	// Check if this is the first user
	userCount, err := s.userRepo.GetUserCount()
	if err != nil {
		return nil, fmt.Errorf("failed to check user count: %w", err)
	}

	// Only allow registration if this is the first user
	if userCount > 0 {
		return nil, fmt.Errorf("user registration is currently disabled")
	}

	// Check if user already exists
	existingUser, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing user: %w", err)
	}
	if existingUser != nil {
		return nil, fmt.Errorf("username already exists")
	}

	// Check email if provided
	if email != "" {
		existingUser, err = s.userRepo.GetUserByEmail(email)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing email: %w", err)
		}
		if existingUser != nil {
			return nil, fmt.Errorf("email already exists")
		}
	}

	// Hash the password
	passwordHash, err := s.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user object
	user := &database.User{
		UserID:       username,
		Provider:     "direct",
		PasswordHash: &passwordHash,
		IsAdmin:      true, // First user is always admin
	}

	// Set name to username if no separate name provided
	user.Name = &username

	if email != "" {
		user.Email = &email
	}

	// Create the user
	err = s.userRepo.CreateUser(user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Generate API key for the first user (admin)
	apiKey, err := s.userRepo.RegenerateAPIKey(user.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate API key for first user: %w", err)
	}

	// Update the user object with the generated API key
	user.APIKey = &apiKey

	s.logger.Info("First user registered with API key", "username", username, "is_admin", true)
	return user, nil
}

// AuthenticateUser verifies username/password and returns user
func (s *Service) AuthenticateUser(username, password string) (*database.User, error) {
	// Try to find user by username first
	user, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// If not found by username, try by email
	if user == nil {
		user, err = s.userRepo.GetUserByEmail(username)
		if err != nil {
			return nil, fmt.Errorf("failed to get user by email: %w", err)
		}
	}

	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Verify password
	if user.PasswordHash == nil {
		return nil, fmt.Errorf("user has no password set")
	}

	err = bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password))
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return user, nil
}

// HashPassword hashes a password using bcrypt
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CreateClaimsFromUser creates JWT claims from a database user
func CreateClaimsFromUser(user *database.User) token.Claims {
	// Use username as display name if no name is set
	displayName := user.UserID
	if user.Name != nil && *user.Name != "" {
		displayName = *user.Name
	}

	claims := token.Claims{
		User: &token.User{
			ID:   user.UserID,
			Name: displayName,
		},
		SessionOnly: false,
	}

	// Set email if available
	if user.Email != nil {
		claims.User.Email = *user.Email
	}

	// Set avatar if available
	if user.AvatarURL != nil {
		claims.User.Picture = *user.AvatarURL
	}

	// Set custom attributes
	if claims.User.Attributes == nil {
		claims.User.Attributes = make(map[string]interface{})
	}
	claims.User.Attributes["is_admin"] = user.IsAdmin
	claims.User.Attributes["provider"] = user.Provider

	return claims
}

// directCredChecker implements the provider.CredChecker interface
type directCredChecker struct {
	service *Service
}

// Check verifies credentials for direct authentication
func (d *directCredChecker) Check(user, password string) (bool, error) {
	dbUser, err := d.service.AuthenticateUser(user, password)
	if err != nil {
		d.service.logger.Debug("Authentication failed", "user", user, "error", err)
		return false, err
	}

	d.service.logger.Debug("Authentication successful", "user", user, "user_id", dbUser.UserID)
	return true, nil
}

// generateRandomSalt generates a random salt for authentication
func generateRandomSalt() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "default-salt-change-in-production"
	}
	return hex.EncodeToString(bytes)
}
