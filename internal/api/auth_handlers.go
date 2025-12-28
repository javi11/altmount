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
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Username and password are required",
		})
	}

	// Authenticate user
	user, err := s.authService.AuthenticateUser(c.Context(), req.Username, req.Password)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Invalid credentials",
		})
	}

	// Create JWT token
	tokenService := s.authService.TokenService()
	claims := auth.CreateClaimsFromUser(c.Context(), user)

	// Generate JWT token string
	tokenString, err := tokenService.Token(claims)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create token",
			"details": err.Error(),
		})
	}

	// Set JWT cookie using Fiber's native API  (merged config)
	err = s.setJWTCookie(c, tokenString)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to set cookie",
			"details": err.Error(),
		})
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
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleRegister handles user registration (first user only)
func (s *Server) handleRegister(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Username and password are required",
		})
	}

	// Validate username (basic validation)
	if len(req.Username) < 3 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Username must be at least 3 characters",
		})
	}

	// Validate password (basic validation)
	if len(req.Password) < 8 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Password must be at least 8 characters",
		})
	}

	// Create user
	user, err := s.authService.RegisterUser(c.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		if err.Error() == "user registration is currently disabled" {
			return c.Status(403).JSON(fiber.Map{
				"success": false,
				"message": "User registration is disabled",
			})
		} else if err.Error() == "username already exists" || err.Error() == "email already exists" {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"message": err.Error(),
			})
		} else {
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"message": "Failed to register user",
				"details": err.Error(),
			})
		}
	}

	response := AuthResponse{
		User:    s.mapUserToResponse(user),
		Message: "Registration successful. API key generated automatically.",
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleCheckRegistration checks if registration is allowed
func (s *Server) handleCheckRegistration(c *fiber.Ctx) error {
	userCount, err := s.userRepo.GetUserCount(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to check registration status",
			"details": err.Error(),
		})
	}

	response := fiber.Map{
		"registration_enabled": userCount == 0,
		"user_count":           userCount,
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
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
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleAuthUser returns current authenticated user information
func (s *Server) handleAuthUser(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Not authenticated",
		})
	}

	response := s.mapUserToResponse(user)
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    *response,
	})
}

// handleAuthLogout logs out the current user
func (s *Server) handleAuthLogout(c *fiber.Ctx) error {
	// Clear JWT cookie using Fiber's native API
	s.clearJWTCookie(c)

	response := AuthResponse{
		Message: "Logged out successfully",
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleAuthRefresh refreshes the current JWT token
func (s *Server) handleAuthRefresh(c *fiber.Ctx) error {
	tokenService := s.authService.TokenService()

	// Convert Fiber request to HTTP request for token service
	httpReq, err := adaptor.ConvertRequest(c, false)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to convert request",
			"details": err.Error(),
		})
	}

	// Get current token
	claims, _, err := tokenService.Get(httpReq)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "No valid token found",
		})
	}

	// Generate new token string
	tokenString, err := tokenService.Token(claims)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to create token",
			"details": err.Error(),
		})
	}

	// Set JWT cookie using Fiber's native API
	err = s.setJWTCookie(c, tokenString)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to set cookie",
			"details": err.Error(),
		})
	}

	response := AuthResponse{
		Message: "Token refreshed successfully",
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleListUsers returns a list of users (admin only)
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil || !user.IsAdmin {
		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"message": "Admin privileges required",
		})
	}

	pagination := ParsePaginationFiber(c)
	users, err := s.userRepo.ListUsers(c.Context(), pagination.Limit, pagination.Offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to list users",
			"details": err.Error(),
		})
	}

	// Convert to response format
	var userResponses []*UserResponse
	for _, user := range users {
		userResponses = append(userResponses, s.mapUserToResponse(user))
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    userResponses,
	})
}

// handleUpdateUserAdmin updates a user's admin status (admin only)
func (s *Server) handleUpdateUserAdmin(c *fiber.Ctx) error {
	user := auth.GetUserFromContext(c)
	if user == nil || !user.IsAdmin {
		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"message": "Admin privileges required",
		})
	}

	userID := c.Params("user_id")
	if userID == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "User ID is required",
		})
	}

	// Parse request body
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	// Update admin status
	err := s.userRepo.SetAdminStatus(c.Context(), userID, req.IsAdmin)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to update user admin status",
			"details": err.Error(),
		})
	}

	response := AuthResponse{
		Message: "User admin status updated successfully",
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleRegenerateAPIKey regenerates API key for the authenticated user
func (s *Server) handleRegenerateAPIKey(c *fiber.Ctx) error {
	// Try to get user from context (auth enabled case)
	user := auth.GetUserFromContext(c)

	// If no user in context, try to get first user (auth disabled case)
	if user == nil && s.userRepo != nil {
		users, err := s.userRepo.ListUsers(c.Context(), 1, 0)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"success": false,
				"message": "Failed to retrieve user list",
				"details": err.Error(),
			})
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
				return c.Status(500).JSON(fiber.Map{
					"success": false,
					"message": "Failed to bootstrap default admin user",
					"details": err.Error(),
				})
			}
			slog.InfoContext(c.Context(), "Bootstrapped default admin user for API key generation")
		}
	}

	// If still no user, return error
	if user == nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "No user found to regenerate API key for. Please register first.",
		})
	}

	// Regenerate API key
	apiKey, err := s.userRepo.RegenerateAPIKey(c.Context(), user.UserID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to regenerate API key",
			"details": err.Error(),
		})
	}

	response := fiber.Map{
		"api_key": apiKey,
		"message": "API key regenerated successfully",
	}
	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
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

// mergeAuthConfig merges env config with service config
func mergeAuthConfig(serviceCfg, envCfg *auth.Config) *auth.Config {
	merged := *serviceCfg

	if envCfg.CookieDomain != "" {
		merged.CookieDomain = envCfg.CookieDomain
	}
	if envCfg.CookieSecure {
		merged.CookieSecure = envCfg.CookieSecure
	}
	if envCfg.CookieSameSite != http.SameSiteDefaultMode {
		merged.CookieSameSite = envCfg.CookieSameSite
	}
	if envCfg.TokenDuration != 0 {
		merged.TokenDuration = envCfg.TokenDuration
	}

	return &merged
}

// setJWTCookie sets the JWT cookie using Fiber's native cookie handling (merged config)
func (s *Server) setJWTCookie(c *fiber.Ctx, tokenString string) error {
	serviceCfg := s.authService.GetConfig()
	envCfg := auth.LoadConfigFromEnv()

	cfg := mergeAuthConfig(serviceCfg, envCfg)

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
	serviceCfg := s.authService.GetConfig()
	envCfg := auth.LoadConfigFromEnv()

	cfg := mergeAuthConfig(serviceCfg, envCfg)

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
