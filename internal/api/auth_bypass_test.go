package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockConfigManager for testing
type MockConfigManager struct {
	mock.Mock
}

func (m *MockConfigManager) GetConfig() *config.Config {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*config.Config)
}

func (m *MockConfigManager) GetConfigGetter() config.ConfigGetter {
	args := m.Called()
	return args.Get(0).(config.ConfigGetter)
}

func (m *MockConfigManager) UpdateConfig(c *config.Config) error {
	args := m.Called(c)
	return args.Error(0)
}

func (m *MockConfigManager) SaveConfig() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConfigManager) ValidateConfigUpdate(c *config.Config) error {
	args := m.Called(c)
	return args.Error(0)
}

func (m *MockConfigManager) ValidateConfig(c *config.Config) error {
	args := m.Called(c)
	return args.Error(0)
}

func (m *MockConfigManager) ReloadConfig() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConfigManager) OnConfigChange(callback config.ChangeCallback) {
	m.Called(callback)
}

func (m *MockConfigManager) NeedsLibrarySync() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockConfigManager) GetPreviousMountPath() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConfigManager) ClearLibrarySyncFlag() {
	m.Called()
}

func TestAuthBypass(t *testing.T) {
	// Setup SQLite in-memory DB
	db, err := sql.Open("sqlite3", ":memory:")
	assert.NoError(t, err)
	defer db.Close()

	// Create users table
	schema := `
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT UNIQUE NOT NULL,
		email TEXT,
		name TEXT,
		avatar_url TEXT,
		provider TEXT NOT NULL,
		provider_id TEXT,
		is_admin BOOLEAN DEFAULT FALSE,
		password_hash TEXT,
		api_key TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login DATETIME,
		UNIQUE(provider, provider_id)
	);`
	_, err = db.Exec(schema)
	assert.NoError(t, err)

	userRepo := database.NewUserRepository(db)
	
	// Setup Auth Service
	authConfig := auth.DefaultConfig()
	authService, err := auth.NewService(authConfig, userRepo)
	assert.NoError(t, err)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		ProxyHeader: "X-Forwarded-For",
	})

	// Mock Config Manager with login_required = false
	mockConfigManager := new(MockConfigManager)
	loginRequired := false
	cfg := &config.Config{
		Auth: config.AuthConfig{
			LoginRequired: &loginRequired,
		},
		API: config.APIConfig{
			Prefix: "/api",
		},
	}
	
	mockConfigManager.On("GetConfig").Return(cfg)
	mockConfigManager.On("GetConfigGetter").Return(config.ConfigGetter(func() *config.Config { return cfg }))

	// Create Server instance (partial)
	server := &Server{
		config:        &Config{Prefix: "/api"}, // Use api.Config here
		configManager: mockConfigManager,
		authService:   authService,
		userRepo:      userRepo,
	}

	// Middleware setup
	tokenService := authService.TokenService()
	// Use the middleware to test the bypass logic
	app.Use(auth.RequireAuthWithSkip(tokenService, userRepo, mockConfigManager.GetConfigGetter(), []string{}))

	// Register the specific route we are testing
	app.Get("/api/user", server.handleAuthUser)

	// Test Case: Request from Localhost
	req := httptest.NewRequest("GET", "/api/user", nil)
	req.RemoteAddr = "127.0.0.1:12345" // Simulate localhost
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	
	resp, err := app.Test(req)
	assert.NoError(t, err)

	// Expectation:
	// Should return 200 OK because middleware should bypass auth for localhost when login_required=false
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read body to confirm message
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	
	// Check if user is returned as admin
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		// Fallback if structure is flat (depends on RespondSuccess implementation)
		data = result
	}
	
	if val, ok := data["is_admin"]; ok {
		assert.Equal(t, true, val)
	} else {
		// If is_admin is not in data, check User object inside data?
		// AuthResponse has User field?
		// handleAuthUser returns UserResponse directly.
		t.Logf("Response body: %s", string(body))
		t.Error("is_admin field missing in response")
	}
}
