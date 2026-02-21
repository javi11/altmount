package auth

import (
	"strings"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

type contextKey string

const UserContextKey contextKey = "user"

// validateAuth performs the authentication check logic
func validateAuth(c *fiber.Ctx, tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter) (*database.User, error) {
	// 1. Check if auth is disabled (login_required = false)
	if configGetter != nil {
		cfg := configGetter()
		if cfg != nil && cfg.Auth.LoginRequired != nil && !*cfg.Auth.LoginRequired {
			// Check if request is from localhost
			ip := c.IP()
			isLocal := ip == "127.0.0.1" || ip == "::1"
			
			if isLocal {
				// Bypass authentication for localhost
				return &database.User{
					UserID:   "admin",
					Provider: "system",
					IsAdmin:  true,
					Name:     func(s string) *string { return &s }("System Admin"),
				}, nil
			}
			// If not local, fall through to regular authentication
		}
	}

	// 2. Standard Authentication Logic
	if tokenService == nil || userRepo == nil {
		// Only return error if we actually need auth service and it's missing
		// In bypass mode (above), we might not need it if conditions met
		return nil, fiber.NewError(fiber.StatusInternalServerError, "Authentication service unavailable")
	}

	httpReq, err := adaptor.ConvertRequest(c, false)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required")
	}

	claims, _, err := tokenService.Get(httpReq)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required")
	}

	if claims.User == nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Invalid authentication token")
	}

	userID := claims.User.ID
	if userID == "" {
		userID = claims.Subject
	}

	if userID == "" {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Invalid user identifier")
	}

	user, err := userRepo.GetUserByID(c.Context(), userID)
	if err != nil || user == nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "User not found")
	}

	return user, nil
}

// JWTMiddleware provides JWT authentication middleware for  (soft auth - optional)
// This middleware adds user to context if valid token exists, but doesn't require it
func JWTMiddleware(tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := validateAuth(c, tokenService, userRepo, configGetter)
		if err == nil && user != nil {
			c.Locals(string(UserContextKey), user)
		}
		// Always continue, even if auth fails (soft auth)
		return c.Next()
	}
}

// RequireAuth middleware requires authentication for protected routes (hard auth - required)
func RequireAuth(tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, err := validateAuth(c, tokenService, userRepo, configGetter)
		if err != nil {
			// Map error to proper JSON response
			if fiberErr, ok := err.(*fiber.Error); ok {
				return c.Status(fiberErr.Code).JSON(fiber.Map{
					"success": false,
					"message": fiberErr.Message,
				})
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": err.Error(),
			})
		}
		
		// Add user to context
		c.Locals(string(UserContextKey), user)
		return c.Next()
	}
}

// RequireAdmin middleware requires admin privileges for protected routes
func RequireAdmin(tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Use validateAuth directly instead of wrapping RequireAuth middleware
		// to avoid double execution or flow control issues
		user, err := validateAuth(c, tokenService, userRepo, configGetter)
		if err != nil {
			if fiberErr, ok := err.(*fiber.Error); ok {
				return c.Status(fiberErr.Code).JSON(fiber.Map{
					"success": false,
					"message": fiberErr.Message,
				})
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success": false,
				"message": err.Error(),
			})
		}

		// Check admin privileges
		if !user.IsAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"message": "Admin privileges required",
			})
		}

		// Add user to context
		c.Locals(string(UserContextKey), user)
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
func AuthMiddleware(tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter, skipPaths []string) fiber.Handler {
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
		return JWTMiddleware(tokenService, userRepo, configGetter)(c)
	}
}

// RequireAuthWithSkip requires auth but skips certain paths
func RequireAuthWithSkip(tokenService *token.Service, userRepo *database.UserRepository, configGetter config.ConfigGetter, skipPaths []string) fiber.Handler {
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
		return RequireAuth(tokenService, userRepo, configGetter)(c)
	}
}
