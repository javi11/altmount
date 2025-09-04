package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

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
func (s *Server) handleDirectLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	if req.Username == "" || req.Password == "" {
		WriteBadRequest(w, "Username and password are required", "")
		return
	}

	// Authenticate user
	user, err := s.authService.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		WriteUnauthorized(w, "Invalid credentials", "")
		return
	}

	// Create JWT token
	tokenService := s.authService.TokenService()
	claims := auth.CreateClaimsFromUser(user)

	_, err = tokenService.Set(w, claims)
	if err != nil {
		WriteInternalError(w, "Failed to create session", err.Error())
		return
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
	WriteSuccess(w, response, nil)
}

// handleRegister handles user registration (first user only)
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	if req.Username == "" || req.Password == "" {
		WriteBadRequest(w, "Username and password are required", "")
		return
	}

	// Validate username (basic validation)
	if len(req.Username) < 3 {
		WriteBadRequest(w, "Username must be at least 3 characters", "")
		return
	}

	// Validate password (basic validation)
	if len(req.Password) < 8 {
		WriteBadRequest(w, "Password must be at least 8 characters", "")
		return
	}

	// Create user
	user, err := s.authService.RegisterUser(req.Username, req.Email, req.Password)
	if err != nil {
		if err.Error() == "user registration is currently disabled" {
			WriteForbidden(w, "User registration is disabled", "")
		} else if err.Error() == "username already exists" || err.Error() == "email already exists" {
			WriteConflict(w, err.Error(), "")
		} else {
			WriteInternalError(w, "Failed to register user", err.Error())
		}
		return
	}

	response := AuthResponse{
		User:    s.mapUserToResponse(user),
		Message: "Registration successful",
	}
	WriteSuccess(w, response, nil)
}

// handleCheckRegistration checks if registration is allowed
func (s *Server) handleCheckRegistration(w http.ResponseWriter, r *http.Request) {
	userCount, err := s.userRepo.GetUserCount()
	if err != nil {
		WriteInternalError(w, "Failed to check registration status", err.Error())
		return
	}

	response := map[string]interface{}{
		"registration_enabled": userCount == 0,
		"user_count":           userCount,
	}
	WriteSuccess(w, response, nil)
}

// handleAuthUser returns current authenticated user information
func (s *Server) handleAuthUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		WriteUnauthorized(w, "Not authenticated", "")
		return
	}

	response := s.mapUserToResponse(user)
	WriteSuccess(w, *response, nil)
}

// handleAuthLogout logs out the current user
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	// Clear JWT cookie
	tokenService := s.authService.TokenService()
	tokenService.Reset(w)

	response := AuthResponse{
		Message: "Logged out successfully",
	}
	WriteSuccess(w, response, nil)
}

// handleAuthRefresh refreshes the current JWT token
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	tokenService := s.authService.TokenService()

	// Get current token
	claims, _, err := tokenService.Get(r)
	if err != nil {
		WriteUnauthorized(w, "No valid token found", "")
		return
	}

	// Issue new token with same claims
	_, err = tokenService.Set(w, claims)
	if err != nil {
		WriteInternalError(w, "Failed to refresh token", err.Error())
		return
	}

	response := AuthResponse{
		Message: "Token refreshed successfully",
	}
	WriteSuccess(w, response, nil)
}

// handleListUsers returns a list of users (admin only)
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !auth.IsAdmin(r.Context()) {
		WriteForbidden(w, "Admin privileges required", "")
		return
	}

	pagination := ParsePagination(r)
	users, err := s.userRepo.ListUsers(pagination.Limit, pagination.Offset)
	if err != nil {
		WriteInternalError(w, "Failed to list users", err.Error())
		return
	}

	// Convert to response format
	var userResponses []*UserResponse
	for _, user := range users {
		userResponses = append(userResponses, s.mapUserToResponse(user))
	}

	WriteSuccess(w, userResponses, nil)
}

// handleUpdateUserAdmin updates a user's admin status (admin only)
func (s *Server) handleUpdateUserAdmin(w http.ResponseWriter, r *http.Request) {
	if !auth.IsAdmin(r.Context()) {
		WriteForbidden(w, "Admin privileges required", "")
		return
	}

	userID := r.PathValue("user_id")
	if userID == "" {
		WriteBadRequest(w, "User ID is required", "")
		return
	}

	// Parse request body
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "Invalid request body", err.Error())
		return
	}

	// Update admin status
	err := s.userRepo.SetAdminStatus(userID, req.IsAdmin)
	if err != nil {
		WriteInternalError(w, "Failed to update user admin status", err.Error())
		return
	}

	response := AuthResponse{
		Message: "User admin status updated successfully",
	}
	WriteSuccess(w, response, nil)
}

// handleGenerateInitialAPIKey generates API key for the first user (admin) if they don't have one
func (s *Server) handleGenerateInitialAPIKey(w http.ResponseWriter, r *http.Request) {
	// Check if there is exactly one user in the system
	userCount, err := s.userRepo.GetUserCount()
	if err != nil {
		WriteInternalError(w, "Failed to check user count", err.Error())
		return
	}

	if userCount != 1 {
		WriteForbidden(w, "Initial API key generation is only available when there is exactly one user", "")
		return
	}

	// Get all users (should be just one)
	users, err := s.userRepo.ListUsers(1, 0)
	if err != nil || len(users) != 1 {
		WriteInternalError(w, "Failed to get first user", err.Error())
		return
	}

	user := users[0]

	// Check if user is admin
	if !user.IsAdmin {
		WriteForbidden(w, "Initial API key generation is only available for admin users", "")
		return
	}

	// Check if user already has an API key
	if user.APIKey != nil && *user.APIKey != "" {
		WriteConflict(w, "User already has an API key", "")
		return
	}

	// Generate API key for the user
	apiKey, err := s.userRepo.RegenerateAPIKey(user.UserID)
	if err != nil {
		WriteInternalError(w, "Failed to generate API key", err.Error())
		return
	}

	slog.Info("Initial API key generated for first user", "user_id", user.UserID)

	response := map[string]interface{}{
		"api_key": apiKey,
		"message": "Initial API key generated successfully",
	}
	WriteSuccess(w, response, nil)
}

// handleRegenerateAPIKey regenerates API key for the authenticated user
func (s *Server) handleRegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		WriteUnauthorized(w, "Not authenticated", "")
		return
	}

	// Regenerate API key
	apiKey, err := s.userRepo.RegenerateAPIKey(user.UserID)
	if err != nil {
		WriteInternalError(w, "Failed to regenerate API key", err.Error())
		return
	}

	response := map[string]interface{}{
		"api_key": apiKey,
		"message": "API key regenerated successfully",
	}
	WriteSuccess(w, response, nil)
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
