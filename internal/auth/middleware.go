package auth

import (
	"context"
	"net/http"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/internal/database"
)

// UserContextKey is the key used to store user information in request context
type contextKey string

const UserContextKey contextKey = "user"

// JWTMiddleware provides JWT authentication middleware
func JWTMiddleware(tokenService *token.Service, userRepo *database.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for nil dependencies
			if tokenService == nil || userRepo == nil {
				// Continue without user context if dependencies are missing
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from request
			claims, _, err := tokenService.Get(r)
			if err != nil {
				// No valid token found, continue without user context
				next.ServeHTTP(w, r)
				return
			}

			// Check if claims and user are valid
			if claims.User == nil {
				// Invalid claims, continue without user context
				next.ServeHTTP(w, r)
				return
			}

			// Get user from database
			userID := claims.User.ID
			if userID == "" {
				userID = claims.Subject
			}

			if userID == "" {
				// No user ID available, continue without user context
				next.ServeHTTP(w, r)
				return
			}

			user, err := userRepo.GetUserByID(userID)
			if err != nil || user == nil {
				// User not found in database, continue without user context
				next.ServeHTTP(w, r)
				return
			}

			// Add user to request context
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth middleware requires authentication for protected routes
func RequireAuth(tokenService *token.Service, userRepo *database.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for nil dependencies
			if tokenService == nil || userRepo == nil {
				http.Error(w, "Authentication service unavailable", http.StatusInternalServerError)
				return
			}

			// Extract token from request
			claims, _, err := tokenService.Get(r)
			if err != nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Check if claims and user are valid
			if claims.User == nil {
				http.Error(w, "Invalid authentication token", http.StatusUnauthorized)
				return
			}

			// Get user from database
			userID := claims.User.ID
			if userID == "" {
				userID = claims.Subject
			}

			if userID == "" {
				http.Error(w, "Invalid user identifier", http.StatusUnauthorized)
				return
			}

			user, err := userRepo.GetUserByID(userID)
			if err != nil || user == nil {
				http.Error(w, "User not found", http.StatusUnauthorized)
				return
			}

			// Add user to request context
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin middleware requires admin privileges for protected routes
func RequireAdmin(tokenService *token.Service, userRepo *database.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// First apply RequireAuth
			authMiddleware := RequireAuth(tokenService, userRepo)
			authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Get user from context
				user := GetUserFromContext(r.Context())
				if user == nil {
					http.Error(w, "Authentication required", http.StatusUnauthorized)
					return
				}

				// Check admin privileges
				if !user.IsAdmin {
					http.Error(w, "Admin privileges required", http.StatusForbidden)
					return
				}

				next.ServeHTTP(w, r)
			})).ServeHTTP(w, r)
		})
	}
}

// GetUserFromContext extracts user from request context
func GetUserFromContext(ctx context.Context) *database.User {
	user, ok := ctx.Value(UserContextKey).(*database.User)
	if !ok {
		return nil
	}
	return user
}

// IsAuthenticated checks if the request has a valid authenticated user
func IsAuthenticated(ctx context.Context) bool {
	return GetUserFromContext(ctx) != nil
}

// IsAdmin checks if the authenticated user has admin privileges
func IsAdmin(ctx context.Context) bool {
	user := GetUserFromContext(ctx)
	return user != nil && user.IsAdmin
}