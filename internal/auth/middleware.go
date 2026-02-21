package auth

import (
	"strings"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/javi11/altmount/internal/database"
)

type contextKey string

const UserContextKey contextKey = "user"

// JWTMiddleware provides JWT authentication middleware for  (soft auth - optional)
// This middleware adds user to context if valid token exists, but doesn't require it
func JWTMiddleware(tokenService *token.Service, userRepo *database.UserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check for nil dependencies
		if tokenService == nil || userRepo == nil {
			// Continue without user context if dependencies are missing
			return c.Next()
		}

		// Convert  request to HTTP request for token service
		httpReq, err := adaptor.ConvertRequest(c, false)
		if err != nil {
			// Continue without user context if conversion fails
			return c.Next()
		}

		// Extract token from request
		claims, _, err := tokenService.Get(httpReq)
		if err != nil {
			// No valid token found, continue without user context
			return c.Next()
		}

		// Check if claims and user are valid
		if claims.User == nil {
			// Invalid claims, continue without user context
			return c.Next()
		}

		// Get user from database
		userID := claims.User.ID
		if userID == "" {
			userID = claims.Subject
		}

		if userID == "" {
			// No user ID available, continue without user context
			return c.Next()
		}

		user, err := userRepo.GetUserByID(c.Context(), userID)
		if err != nil || user == nil {
			// User not found in database, continue without user context
			return c.Next()
		}

		// Add user to  context
		c.Locals(string(UserContextKey), user)
		return c.Next()
	}
}

// RequireAuth middleware requires authentication for protected routes (hard auth - required)
func RequireAuth(tokenService *token.Service, userRepo *database.UserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check for nil dependencies
		if tokenService == nil || userRepo == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"success": false,
				"message": "Authentication service unavailable",
			})
		}

		// Convert  request to HTTP request for token service
		httpReq, err := adaptor.ConvertRequest(c, false)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Authentication required",
			})
		}

		// Extract token from request
		claims, _, err := tokenService.Get(httpReq)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Authentication required",
			})
		}

		// Check if claims and user are valid
		if claims.User == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Invalid authentication token",
			})
		}

		// Get user from database
		userID := claims.User.ID
		if userID == "" {
			userID = claims.Subject
		}

		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Invalid user identifier",
			})
		}

		user, err := userRepo.GetUserByID(c.Context(), userID)
		if err != nil || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "User not found",
			})
		}

		// Add user to  context
		c.Locals(string(UserContextKey), user)
		return c.Next()
	}
}

// RequireAdmin middleware requires admin privileges for protected routes
func RequireAdmin(tokenService *token.Service, userRepo *database.UserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// First apply RequireAuth
		authMiddleware := RequireAuth(tokenService, userRepo)
		if err := authMiddleware(c); err != nil {
			return err
		}

		// Get user from context
		user := GetUserFromContext(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Authentication required",
			})
		}

		// Check admin privileges
		if !user.IsAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"message": "Admin privileges required",
			})
		}

		return c.Next()
	}
}

// GetUserFromContext extracts user from  context
func GetUserFromContext(c *fiber.Ctx) *database.User {
	user, ok := c.Locals(string(UserContextKey)).(*database.User)
	if !ok {
		return nil
	}
	return user
}

// AuthMiddleware is a flexible auth middleware that can skip certain paths
func AuthMiddleware(tokenService *token.Service, userRepo *database.UserRepository, skipPaths []string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check if current path should skip authentication
		path := c.Path()
		for _, skipPath := range skipPaths {
			if strings.HasPrefix(path, skipPath) {
				// Skip authentication for this path
				return c.Next()
			}
		}

		// Apply JWT middleware for all other paths
		return JWTMiddleware(tokenService, userRepo)(c)
	}
}

// RequireAuthWithSkip requires auth but skips certain paths
func RequireAuthWithSkip(tokenService *token.Service, userRepo *database.UserRepository, skipPaths []string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check if current path should skip authentication
		path := c.Path()
		for _, skipPath := range skipPaths {
			if strings.HasPrefix(path, skipPath) {
				// Skip authentication for this path
				return c.Next()
			}
		}

		// Require authentication for all other paths
		return RequireAuth(tokenService, userRepo)(c)
	}
}
