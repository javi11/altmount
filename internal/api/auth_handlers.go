package api

import (
	"log/slog"
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
	user, err := s.authService.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Invalid credentials",
		})
	}

	// Create JWT token
	tokenService := s.authService.TokenService()
	claims := auth.CreateClaimsFromUser(user)

	// Generate JWT token string
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

	// Update last login
	err = s.userRepo.UpdateLastLogin(user.UserID)
	if err != nil {
		// Log but don't fail the login
		slog.Warn("Failed to update last login", "user_id", user.UserID, "error", err)
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
	user, err := s.authService.RegisterUser(req.Username, req.Email, req.Password)
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
	userCount, err := s.userRepo.GetUserCount()
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
	users, err := s.userRepo.ListUsers(pagination.Limit, pagination.Offset)
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
	err := s.userRepo.SetAdminStatus(userID, req.IsAdmin)
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
	user := auth.GetUserFromContext(c)
	if user == nil {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Not authenticated",
		})
	}

	// Regenerate API key
	apiKey, err := s.userRepo.RegenerateAPIKey(user.UserID)
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

// setJWTCookie sets the JWT cookie using Fiber's native cookie handling
func (s *Server) setJWTCookie(c *fiber.Ctx, tokenString string) error {
	config := auth.LoadConfigFromEnv()

	cookie := &fiber.Cookie{
		Name:     "JWT", // Default JWT cookie name
		Value:    tokenString,
		Path:     "/",
		Domain:   config.CookieDomain,
		Expires:  time.Now().Add(config.TokenDuration),
		Secure:   config.CookieSecure,
		HTTPOnly: true,
		SameSite: "Lax", // Use Lax for Safari compatibility
	}

	c.Cookie(cookie)
	return nil
}

// clearJWTCookie clears the JWT cookie using Fiber's native cookie handling
func (s *Server) clearJWTCookie(c *fiber.Ctx) {
	config := auth.LoadConfigFromEnv()

	cookie := &fiber.Cookie{
		Name:     "JWT",
		Value:    "",
		Path:     "/",
		Domain:   config.CookieDomain,
		Expires:  time.Now().Add(-time.Hour), // Expire in the past
		Secure:   config.CookieSecure,
		HTTPOnly: true,
		SameSite: "Lax",
	}

	c.Cookie(cookie)
}
