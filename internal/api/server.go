package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
)

// Config represents API server configuration
type Config struct {
	Prefix string // API path prefix (default: "/api")
}

// DefaultConfig returns default API configuration
func DefaultConfig() *Config {
	return &Config{
		Prefix: "/api",
	}
}

// Server represents the API server
type Server struct {
	config          *Config
	queueRepo       *database.Repository
	healthRepo      *database.HealthRepository
	mediaRepo       *database.MediaRepository
	authService     *auth.Service
	userRepo        *database.UserRepository
	configManager   ConfigManager
	metadataReader  *metadata.MetadataReader
	healthWorker    *health.HealthWorker
	importerService *importer.Service
	poolManager     pool.Manager
	arrsService     *arrs.Service
	logger          *slog.Logger
	startTime       time.Time
	mux             *http.ServeMux
}

// NewServer creates a new API server that can optionally register routes on the provided mux (for backwards compatibility)
func NewServer(
	config *Config,
	queueRepo *database.Repository,
	healthRepo *database.HealthRepository,
	mediaRepo *database.MediaRepository,
	authService *auth.Service,
	userRepo *database.UserRepository,
	configManager ConfigManager,
	metadataReader *metadata.MetadataReader,
	poolManager pool.Manager,
	importService *importer.Service,
	arrsService *arrs.Service) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	server := &Server{
		config:          config,
		queueRepo:       queueRepo,
		healthRepo:      healthRepo,
		mediaRepo:       mediaRepo,
		authService:     authService,
		userRepo:        userRepo,
		configManager:   configManager,
		metadataReader:  metadataReader,
		importerService: importService, // Will be set later via SetImporterService
		poolManager:     poolManager,
		arrsService:     arrsService,
		logger:          slog.Default(),
		startTime:       time.Now(),
	}

	return server
}

// SetHealthWorker sets the health worker reference for the server
func (s *Server) SetHealthWorker(healthWorker *health.HealthWorker) {
	s.healthWorker = healthWorker
}

// SetupFiberRoutes configures API routes directly on the Fiber app
func (s *Server) SetupRoutes(app *fiber.App) {
	api := app.Group(s.config.Prefix)
	api.Use(cors.New())
	api.Use(recover.New())

	// Queue endpoints
	api.Get("/queue", s.handleListQueue)
	api.Get("/queue/stats", s.handleGetQueueStats)
	api.Delete("/queue/completed", s.handleClearCompletedQueue)
	api.Get("/queue/:id", s.handleGetQueue)
	api.Delete("/queue/:id", s.handleDeleteQueue)
	api.Delete("/queue/bulk", s.handleDeleteQueueBulk)
	api.Post("/queue/:id/retry", s.handleRetryQueue)

	// Health endpoints
	api.Get("/health", s.handleListHealth)
	api.Post("/health/bulk/delete", s.handleDeleteHealthBulk)
	api.Get("/health/corrupted", s.handleListCorrupted)
	api.Get("/health/stats", s.handleGetHealthStats)
	api.Delete("/health/cleanup", s.handleCleanupHealth)
	api.Post("/health/check", s.handleAddHealthCheck)
	api.Get("/health/worker/status", s.handleGetHealthWorkerStatus)
	api.Post("/health/:id/repair", s.handleRepairHealth)
	api.Post("/health/:id/check-now", s.handleDirectHealthCheck)
	api.Post("/health/:id/cancel", s.handleCancelHealthCheck)
	api.Get("/health/:id", s.handleGetHealth)
	api.Delete("/health/:id", s.handleDeleteHealth)

	// File endpoints (if metadata reader is available)
	if s.metadataReader != nil {
		api.Get("/files/info", s.handleGetFileMetadata)
	}

	// Import endpoints (if importer service is available)
	if s.importerService != nil {
		api.Post("/import/scan", s.handleStartManualScan)
		api.Get("/import/scan/status", s.handleGetScanStatus)
		api.Delete("/import/scan", s.handleCancelScan)
		api.Post("/import/file", s.handleManualImportFile)
	}

	// System endpoints
	api.Get("/system/stats", s.handleGetSystemStats)
	api.Get("/system/health", s.handleGetSystemHealth)
	api.Get("/system/pool/metrics", s.handleGetPoolMetrics)
	api.Post("/system/cleanup", s.handleSystemCleanup)
	api.Post("/system/restart", s.handleSystemRestart)

	// Configuration endpoints (if config manager is available)
	if s.configManager != nil {
		api.Get("/config", s.handleGetConfig)
		api.Put("/config", s.handleUpdateConfig)
		api.Patch("/config/:section", s.handlePatchConfigSection)
		api.Post("/config/reload", s.handleReloadConfig)
		api.Post("/config/validate", s.handleValidateConfig)

		// Provider management endpoints
		api.Post("/providers/test", s.handleTestProvider)
		api.Post("/providers", s.handleCreateProvider)
		api.Put("/providers/:id", s.handleUpdateProvider)
		api.Delete("/providers/:id", s.handleDeleteProvider)
		api.Put("/providers/reorder", s.handleReorderProviders)
	}

	// Arrs endpoints (if arrs service is available)
	if s.arrsService != nil {
		// Configuration-based instance endpoints
		api.Get("/arrs/instances", s.handleListArrsInstances)
		api.Get("/arrs/instances/:type/:name", s.handleGetArrsInstance)
		api.Post("/arrs/instances", s.handleCreateArrsInstance)       // Deprecated
		api.Put("/arrs/instances/:id", s.handleUpdateArrsInstance)    // Deprecated
		api.Delete("/arrs/instances/:id", s.handleDeleteArrsInstance) // Deprecated
		api.Post("/arrs/instances/test", s.handleTestArrsConnection)
		api.Get("/arrs/stats", s.handleGetArrsStats)
		api.Get("/arrs/movies/search", s.handleSearchMovies)     // Deprecated
		api.Get("/arrs/episodes/search", s.handleSearchEpisodes) // Deprecated
	}

	// Authentication endpoints (if auth service is available)
	if s.authService != nil {
		// Direct authentication endpoints (converted to native Fiber)
		api.Post("/auth/login", s.handleDirectLogin)
		api.Post("/auth/register", s.handleRegister)
		api.Get("/auth/registration-status", s.handleCheckRegistration)

		// Protected API endpoints for user management (require authentication)
		// These remain HTTP-based due to middleware dependencies
		tokenService := s.authService.TokenService()
		if tokenService != nil {
			authMiddleware := auth.RequireAuth(tokenService, s.userRepo)
			api.Get("/user", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthUserHTTP))))
			api.Post("/user/refresh", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthRefreshHTTP))))
			api.Post("/user/logout", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthLogoutHTTP))))
			api.Post("/user/api-key/regenerate", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleRegenerateAPIKeyHTTP))))

			// Admin endpoints (require admin privileges)
			adminMiddleware := auth.RequireAdmin(tokenService, s.userRepo)
			api.Get("/users", adaptor.HTTPHandler(adminMiddleware(http.HandlerFunc(s.handleListUsersHTTP))))
			api.Put("/users/:user_id/admin", adaptor.HTTPHandler(adminMiddleware(http.HandlerFunc(s.handleUpdateUserAdminHTTP))))
		}
	}

	// SABnzbd-compatible API endpoints (conditionally enabled)
	if s.configManager != nil {
		config := s.configManager.GetConfig()
		if config.SABnzbd.Enabled != nil && *config.SABnzbd.Enabled {
			sabnzbdHandler := adaptor.HTTPHandler(s.applyMiddleware(http.HandlerFunc(s.handleSABnzbd)))
			app.Use("/sabnzbd/api", sabnzbdHandler)
		}
	}
}

// ServeHTTP implements http.Handler interface (for backwards compatibility)
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.mux != nil {
		s.mux.ServeHTTP(w, r)
	} else {
		http.Error(w, "API server configured for Fiber-only mode", http.StatusNotFound)
	}
}

// applyMiddleware applies the middleware chain to the handler
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// Apply JWT authentication middleware for user context (optional)
	if s.authService != nil && s.userRepo != nil {
		tokenService := s.authService.TokenService()
		if tokenService != nil {
			handler = auth.JWTMiddleware(tokenService, s.userRepo)(handler)
		}
	}

	// Basic authentication is now handled by OAuth flow only

	return handler
}

// getSystemInfo returns current system information
func (s *Server) getSystemInfo() SystemInfoResponse {
	uptime := time.Since(s.startTime)
	return SystemInfoResponse{
		StartTime: s.startTime,
		Uptime:    uptime.String(),
		GoVersion: runtime.Version(),
	}
}

// checkSystemHealth performs a basic health check
func (s *Server) checkSystemHealth(ctx context.Context) SystemHealthResponse {
	components := make(map[string]ComponentHealth)
	overallStatus := "healthy"

	// Check database connectivity
	if _, err := s.queueRepo.GetQueueStats(); err != nil {
		components["database"] = ComponentHealth{
			Status:  "unhealthy",
			Message: "Database connection failed",
			Details: err.Error(),
		}
		overallStatus = "unhealthy"
	} else {
		components["database"] = ComponentHealth{
			Status:  "healthy",
			Message: "Database connection OK",
		}
	}

	// Check health repository
	if _, err := s.healthRepo.GetHealthStats(); err != nil {
		components["health_repository"] = ComponentHealth{
			Status:  "unhealthy",
			Message: "Health repository failed",
			Details: err.Error(),
		}
		if overallStatus != "unhealthy" {
			overallStatus = "degraded"
		}
	} else {
		components["health_repository"] = ComponentHealth{
			Status:  "healthy",
			Message: "Health repository OK",
		}
	}

	return SystemHealthResponse{
		Status:     overallStatus,
		Timestamp:  time.Now(),
		Components: components,
	}
}
