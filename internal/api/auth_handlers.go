package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/database"
)

// AuthResponse represents authentication response data
type AuthResponse struct {
	User        *UserResponse `json:"user,omitempty"`
	RedirectURL string        `json:"redirect_url,omitempty"`
	Message     string        `json:"message,omitempty"`
}

// UserResponse represents user data for API responses
type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key,omitempty"`
	IsAdmin   bool   `json:"is_admin"`
	LastLogin string `json:"last_login,omitempty"`
}

// LoginRequest represents direct authentication login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RegisterRequest represents user registration request
type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Password string `json:"password"`
}

// handleDirectLogin handles username/password authentication
func (s *Server) handleDirectLogin(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	if req.Username == "" || req.Password == "" {
		return RespondBadRequest(c, "Username and password are required", "")
	}

	// Authenticate user
	user, err := s.authService.AuthenticateUser(c.Context(), req.Username, req.Password)
	if err != nil {
		return RespondUnauthorized(c, "Invalid credentials", "")
	}

	// Create JWT token
	tokenService := s.authService.TokenService()
	claims := auth.CreateClaimsFromUser(c.Context(), user)

	// Generate JWT token string
	tokenString, err := tokenService.Token(claims)
	if err != nil {
		return RespondInternalError(c, "Failed to create token", err.Error())
	}

	// Set JWT cookie using Fiber's native API  (merged config)
	err = s.setJWTCookie(c, tokenString)
	if err != nil {
		return RespondInternalError(c, "Failed to set cookie", err.Error())
	}

	// Update last login
	err = s.userRepo.UpdateLastLogin(c.Context(), user.UserID)
	if err != nil {
		// Log but don't fail the login
		slog.WarnContext(c.Context(), "Failed to update last login", "user_id", user.UserID, "error", err)
	}

	response := AuthResponse{
		User:    s.mapUserToResponse(user),
		Message: "Login successful",
	}
	return RespondSuccess(c, response)
}

// handleRegister handles user registration (first user only)
func (s *Server) handleRegister(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	if req.Username == "" || req.Password == "" {
		return RespondBadRequest(c, "Username and password are required", "")
	}

	// Validate username (basic validation)
	if len(req.Username) < 3 {
		return RespondValidationError(c, "Username must be at least 3 characters", "")
	}

	// Validate password (basic validation)
	if len(req.Password) < 12 {
		return RespondValidationError(c, "Password must be at least 12 characters", "")
	}

	// Create user
	user, err := s.authService.RegisterUser(c.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		if err.Error() == "user registration is currently disabled" {
			return RespondForbidden(c, "User registration is disabled", "")
		} else if err.Error() == "username already exists" || err.Error() == "email already exists" {
			return RespondConflict(c, err.Error(), "")
		} else {
			return RespondInternalError(c, "Failed to register user", err.Error())
		}
	}

	response := AuthResponse{
		User:    s.mapUserToResponse(user),
		Message: "Registration successful. API key generated automatically.",
	}
	return RespondSuccess(c, response)
}

// handleCheckRegistration checks if registration is allowed
func (s *Server) handleCheckRegistration(c *fiber.Ctx) error {
	userCount, err := s.userRepo.GetUserCount(c.Context())
	if err != nil {
		return RespondInternalError(c, "Failed to check registration status", err.Error())
	}

	response := fiber.Map{
		"registration_enabled": userCount == 0,
		"user_count":           userCount,
	}
	return RespondSuccess(c, response)
}

// handleGetAuthConfig returns authentication configuration (public endpoint)
func (s *Server) handleGetAuthConfig(c *fiber.Ctx) error {
	cfg := s.configManager.GetConfig()
	loginRequired := true // Default to true if not set
	if cfg != nil && cfg.Auth.LoginRequired != nil {
		loginRequired = *cfg.Auth.LoginRequired
	}

	response := fiber.Map{
		"login_required": loginRequired,
	}
	return RespondSuccess(c, response)
}

// handleAuthUser returns current authenticated user information
func (s *Server) handleAuthUser(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return RespondUnauthorized(c, "Not authenticated", "")
	}

	response := s.mapUserToResponse(user)
	return RespondSuccess(c, *response)
}

// handleAuthLogout logs out the current user
func (s *Server) handleAuthLogout(c *fiber.Ctx) error {
	// Clear JWT cookie using Fiber's native API
	s.clearJWTCookie(c)

	response := AuthResponse{
		Message: "Logged out successfully",
	}
	return RespondSuccess(c, response)
}

// handleAuthRefresh refreshes the current JWT token
func (s *Server) handleAuthRefresh(c *fiber.Ctx) error {
	tokenService := s.authService.TokenService()

	// Convert Fiber request to HTTP request for token service
	httpReq, err := adaptor.ConvertRequest(c, false)
	if err != nil {
		return RespondInternalError(c, "Failed to convert request", err.Error())
	}

	// Get current token
	claims, _, err := tokenService.Get(httpReq)
	if err != nil {
		return RespondUnauthorized(c, "No valid token found", "")
	}

	// Generate new token string
	tokenString, err := tokenService.Token(claims)
	if err != nil {
		return RespondInternalError(c, "Failed to create token", err.Error())
	}

	// Set JWT cookie using Fiber's native API
	err = s.setJWTCookie(c, tokenString)
	if err != nil {
		return RespondInternalError(c, "Failed to set cookie", err.Error())
	}

	response := AuthResponse{
		Message: "Token refreshed successfully",
	}
	return RespondSuccess(c, response)
}

// handleListUsers returns a list of users (admin only)
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil || !user.IsAdmin {
		return RespondForbidden(c, "Admin privileges required", "")
	}

	pagination := ParsePaginationFiber(c)
	users, err := s.userRepo.ListUsers(c.Context(), pagination.Limit, pagination.Offset)
	if err != nil {
		return RespondInternalError(c, "Failed to list users", err.Error())
	}

	// Convert to response format
	var userResponses []*UserResponse
	for _, user := range users {
		userResponses = append(userResponses, s.mapUserToResponse(user))
	}

	return RespondSuccess(c, userResponses)
}

// handleUpdateUserAdmin updates a user's admin status (admin only)
func (s *Server) handleUpdateUserAdmin(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil || !user.IsAdmin {
		return RespondForbidden(c, "Admin privileges required", "")
	}

	userID := c.Params("user_id")
	if userID == "" {
		return RespondBadRequest(c, "User ID is required", "")
	}

	// Parse request body
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid request body", err.Error())
	}

	// Update admin status
	err := s.userRepo.SetAdminStatus(c.Context(), userID, req.IsAdmin)
	if err != nil {
		return RespondInternalError(c, "Failed to update user admin status", err.Error())
	}

	response := AuthResponse{
		Message: "User admin status updated successfully",
	}
	return RespondSuccess(c, response)
}

// handleRegenerateAPIKey regenerates API key for the authenticated user
func (s *Server) handleRegenerateAPIKey(c *fiber.Ctx) error {
	// Try to get user from context (auth enabled case)
	user := auth.GetUserFromContext(c)

	// If no user in context, try to get first user (auth disabled case)
	if user == nil && s.userRepo != nil {
		users, err := s.userRepo.ListUsers(c.Context(), 1, 0)
		if err != nil {
			return RespondInternalError(c, "Failed to retrieve user list", err.Error())
		}
		if len(users) > 0 {
			user = users[0]
		}
	}

	// If still no user, and authentication is disabled, let's create a default admin user
	if user == nil && s.userRepo != nil {
		cfg := s.configManager.GetConfig()
		loginRequired := true
		if cfg.Auth.LoginRequired != nil {
			loginRequired = *cfg.Auth.LoginRequired
		}

		if !loginRequired {
			// Auto-bootstrap a default admin user when auth is disabled
			user = &database.User{
				UserID:   "admin",
				Provider: "direct",
				IsAdmin:  true,
			}
			err := s.userRepo.CreateUser(c.Context(), user)
			if err != nil {
				return RespondInternalError(c, "Failed to bootstrap default admin user", err.Error())
			}
			slog.InfoContext(c.Context(), "Bootstrapped default admin user for API key generation")
		}
	}

	// If still no user, return error
	if user == nil {
		return RespondUnauthorized(c, "No user found to regenerate API key for. Please register first.", "")
	}

	// Regenerate API key
	apiKey, err := s.userRepo.RegenerateAPIKey(c.Context(), user.UserID)
	if err != nil {
		return RespondInternalError(c, "Failed to regenerate API key", err.Error())
	}

	// If key_override is configured (has a value with 33 chars), update it with the new key
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		if cfg.API.KeyOverride != "" && len(cfg.API.KeyOverride) == 33 {
			// Update the key_override in config to match the new key
			newConfig := cfg.DeepCopy()
			newConfig.API.KeyOverride = apiKey

			if err := s.configManager.UpdateConfig(newConfig); err != nil {
				slog.WarnContext(c.Context(), "Failed to update key_override in config", "error", err)
				// Don't fail the request, just log the warning
			} else {
				if err := s.configManager.SaveConfig(); err != nil {
					slog.WarnContext(c.Context(), "Failed to save config after updating key_override", "error", err)
				} else {
					slog.InfoContext(c.Context(), "Updated key_override in config with new API key")
				}
			}
		}
	}

	response := fiber.Map{
		"api_key": apiKey,
		"message": "API key regenerated successfully",
	}
	return RespondSuccess(c, response)
}

// mapUserToResponse converts database User to API UserResponse
func (s *Server) mapUserToResponse(user *database.User) *UserResponse {
	// Use username as display name if no name is set
	displayName := user.UserID
	if user.Name != nil && *user.Name != "" {
		displayName = *user.Name
	}

	response := &UserResponse{
		ID:       user.UserID,
		Name:     displayName,
		Provider: user.Provider,
		IsAdmin:  user.IsAdmin,
	}

	if user.Email != nil {
		response.Email = *user.Email
	}

	if user.AvatarURL != nil {
		response.AvatarURL = *user.AvatarURL
	}

	if user.LastLogin != nil {
		response.LastLogin = user.LastLogin.Format("2006-01-02T15:04:05Z")
	}

	if user.APIKey != nil {
		response.APIKey = *user.APIKey
	}

	return response
}

// sameSiteToString converts http.SameSite to Fiber cookie SameSite string
func sameSiteToString(sameSite http.SameSite) string {
	switch sameSite {
	case http.SameSiteDefaultMode:
		return "Lax" // Default mode uses Lax behavior
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return "Lax" // Fallback for safety
	}
}


// setJWTCookie sets the JWT cookie using Fiber's native cookie handling (merged config)
func (s *Server) setJWTCookie(c *fiber.Ctx, tokenString string) error {
	cfg := s.authService.GetConfig()

	cookie := &fiber.Cookie{
		Name:     "JWT", // Default JWT cookie name
		Value:    tokenString,
		Path:     "/",
		Domain:   cfg.CookieDomain,
		Expires:  time.Now().Add(cfg.TokenDuration),
		Secure:   cfg.CookieSecure,
		HTTPOnly: true,
		SameSite: sameSiteToString(cfg.CookieSameSite),
	}

	c.Cookie(cookie)
	return nil
}

// clearJWTCookie clears the JWT cookie using Fiber's native cookie handling (merged config)
func (s *Server) clearJWTCookie(c *fiber.Ctx) {
	cfg := s.authService.GetConfig()

	cookie := &fiber.Cookie{
		Name:     "JWT",
		Value:    "",
		Path:     "/",
		Domain:   cfg.CookieDomain,
		Expires:  time.Now().Add(-time.Hour), // Expire in the past
		Secure:   cfg.CookieSecure,
		HTTPOnly: true,
		SameSite: sameSiteToString(cfg.CookieSameSite),
	}

	c.Cookie(cookie)
}
