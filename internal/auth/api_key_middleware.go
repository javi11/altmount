package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
)

// APIKeyMiddleware provides API key authentication middleware for
// This middleware checks for API key in query params or headers
func APIKeyMiddleware(userRepo *database.UserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check for nil dependencies
		if userRepo == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"success": false,
				"message": "Authentication service unavailable",
			})
		}

		// Check for API key in query params first
		apiKey := c.Query("apikey")
		if apiKey == "" {
			// Check for API key in headers
			apiKey = c.Get("X-API-Key")
			if apiKey == "" {
				// Check Authorization header
				authHeader := c.Get("Authorization")
				if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
					apiKey = after
				}
			}
		}

		// If no API key found, check if user is already authenticated via JWT
		existingUser := GetUserFromContext(c)
		if existingUser != nil {
			// User already authenticated via JWT
			return c.Next()
		}

		// If no API key and no JWT auth, return error
		if apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Authentication required",
				"details": "Please provide an API key or valid JWT token",
			})
		}

		// Validate API key
		user, err := userRepo.GetUserByAPIKey(c.Context(), apiKey)
		if err != nil || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": "Invalid API key",
			})
		}

		// Add user to  context
		c.Locals(string(UserContextKey), user)
		return c.Next()
	}
}

// OptionalAPIKeyMiddleware provides optional API key authentication
// This middleware adds user to context if valid API key exists, but doesn't require it
func OptionalAPIKeyMiddleware(userRepo *database.UserRepository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check for nil dependencies
		if userRepo == nil {
			// Continue without user context if dependencies are missing
			return c.Next()
		}

		// Check for API key in query params first
		apiKey := c.Query("apikey")
		if apiKey == "" {
			// Check for API key in headers
			apiKey = c.Get("X-API-Key")
			if apiKey == "" {
				// Check Authorization header
				authHeader := c.Get("Authorization")
				if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
					apiKey = after
				}
			}
		}

		// If no API key found, continue without auth
		if apiKey == "" {
			return c.Next()
		}

		// Validate API key
		user, err := userRepo.GetUserByAPIKey(c.Context(), apiKey)
		if err != nil || user == nil {
			// Invalid API key, continue without auth
			return c.Next()
		}

		// Add user to  context
		c.Locals(string(UserContextKey), user)
		return c.Next()
	}
}


