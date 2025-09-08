package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
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

	// Queue endpoints
	api.Get("/queue", adaptor.HTTPHandlerFunc(s.handleListQueue))
	api.Get("/queue/{id}", adaptor.HTTPHandlerFunc(s.handleGetQueue))
	api.Delete("/queue/{id}", adaptor.HTTPHandlerFunc(s.handleDeleteQueue))
	api.Delete("/queue/bulk", adaptor.HTTPHandlerFunc(s.handleDeleteQueueBulk))
	api.Post("/queue/{id}/retry", adaptor.HTTPHandlerFunc(s.handleRetryQueue))
	api.Get("/queue/stats", adaptor.HTTPHandlerFunc(s.handleGetQueueStats))
	api.Delete("/queue/completed", adaptor.HTTPHandlerFunc(s.handleClearCompletedQueue))

	// Health endpoints
	api.Get("/health", adaptor.HTTPHandlerFunc(s.handleListHealth))
	api.Get("/health/{id}", adaptor.HTTPHandlerFunc(s.handleGetHealth))
	api.Delete("/health/{id}", adaptor.HTTPHandlerFunc(s.handleDeleteHealth))
	api.Post("/health/bulk/delete", adaptor.HTTPHandlerFunc(s.handleDeleteHealthBulk))
	api.Post("/health/{id}/repair", adaptor.HTTPHandlerFunc(s.handleRepairHealth))
	api.Get("/health/corrupted", adaptor.HTTPHandlerFunc(s.handleListCorrupted))
	api.Get("/health/stats", adaptor.HTTPHandlerFunc(s.handleGetHealthStats))
	api.Delete("/health/cleanup", adaptor.HTTPHandlerFunc(s.handleCleanupHealth))
	api.Post("/health/check", adaptor.HTTPHandlerFunc(s.handleAddHealthCheck))
	api.Get("/health/worker/status", adaptor.HTTPHandlerFunc(s.handleGetHealthWorkerStatus))
	api.Post("/health/{id}/check-now", adaptor.HTTPHandlerFunc(s.handleDirectHealthCheck))
	api.Post("/health/{id}/cancel", adaptor.HTTPHandlerFunc(s.handleCancelHealthCheck))

	// File endpoints (if metadata reader is available)
	if s.metadataReader != nil {
		api.Get("/files/info", adaptor.HTTPHandlerFunc(s.handleGetFileMetadata))
	}

	// Import endpoints (if importer service is available)
	if s.importerService != nil {
		api.Post("/import/scan", adaptor.HTTPHandlerFunc(s.handleStartManualScan))
		api.Get("/import/scan/status", adaptor.HTTPHandlerFunc(s.handleGetScanStatus))
		api.Delete("/import/scan", adaptor.HTTPHandlerFunc(s.handleCancelScan))
		api.Post("/import/file", adaptor.HTTPHandlerFunc(s.handleManualImportFile))
	}

	// System endpoints
	api.Get("/system/stats", adaptor.HTTPHandlerFunc(s.handleGetSystemStats))
	api.Get("/system/health", adaptor.HTTPHandlerFunc(s.handleGetSystemHealth))
	api.Get("/system/pool/metrics", adaptor.HTTPHandlerFunc(s.handleGetPoolMetrics))
	api.Post("/system/cleanup", adaptor.HTTPHandlerFunc(s.handleSystemCleanup))
	api.Post("/system/restart", adaptor.HTTPHandlerFunc(s.handleSystemRestart))

	// Configuration endpoints (if config manager is available)
	if s.configManager != nil {
		api.Get("/config", adaptor.HTTPHandlerFunc(s.handleGetConfig))
		api.Put("/config", adaptor.HTTPHandlerFunc(s.handleUpdateConfig))
		api.Patch("/config/{section}", adaptor.HTTPHandlerFunc(s.handlePatchConfigSection))
		api.Post("/config/reload", adaptor.HTTPHandlerFunc(s.handleReloadConfig))
		api.Post("/config/validate", adaptor.HTTPHandlerFunc(s.handleValidateConfig))

		// Provider management endpoints
		api.Post("/providers/test", adaptor.HTTPHandlerFunc(s.handleTestProvider))
		api.Post("/providers", adaptor.HTTPHandlerFunc(s.handleCreateProvider))
		api.Put("/providers/{id}", adaptor.HTTPHandlerFunc(s.handleUpdateProvider))
		api.Delete("/providers/{id}", adaptor.HTTPHandlerFunc(s.handleDeleteProvider))
		api.Put("/providers/reorder", adaptor.HTTPHandlerFunc(s.handleReorderProviders))
	}

	// Arrs endpoints (if arrs service is available)
	if s.arrsService != nil {
		// Configuration-based instance endpoints
		api.Get("/arrs/instances", adaptor.HTTPHandlerFunc(s.handleListArrsInstances))
		api.Get("/arrs/instances/{type}/{name}", adaptor.HTTPHandlerFunc(s.handleGetArrsInstance))
		api.Post("/arrs/instances", adaptor.HTTPHandlerFunc(s.handleCreateArrsInstance))        // Deprecated
		api.Put("/arrs/instances/{id}", adaptor.HTTPHandlerFunc(s.handleUpdateArrsInstance))    // Deprecated
		api.Delete("/arrs/instances/{id}", adaptor.HTTPHandlerFunc(s.handleDeleteArrsInstance)) // Deprecated
		api.Post("/arrs/instances/test", adaptor.HTTPHandlerFunc(s.handleTestArrsConnection))
		api.Get("/arrs/stats", adaptor.HTTPHandlerFunc(s.handleGetArrsStats))
		api.Get("/arrs/movies/search", adaptor.HTTPHandlerFunc(s.handleSearchMovies))     // Deprecated
		api.Get("/arrs/episodes/search", adaptor.HTTPHandlerFunc(s.handleSearchEpisodes)) // Deprecated
	}

	// Authentication endpoints (if auth service is available)
	if s.authService != nil {
		// Direct authentication endpoints
		api.Post("/auth/login", adaptor.HTTPHandlerFunc(s.handleDirectLogin))
		api.Post("/auth/register", adaptor.HTTPHandlerFunc(s.handleRegister))
		api.Get("/auth/registration-status", adaptor.HTTPHandlerFunc(s.handleCheckRegistration))

		// Protected API endpoints for user management (require authentication)
		tokenService := s.authService.TokenService()
		if tokenService != nil {
			authMiddleware := auth.RequireAuth(tokenService, s.userRepo)
			api.Get("/user", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthUser))))
			api.Post("/user/refresh", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthRefresh))))
			api.Post("/user/logout", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleAuthLogout))))
			api.Post("/user/api-key/regenerate", adaptor.HTTPHandler(authMiddleware(http.HandlerFunc(s.handleRegenerateAPIKey))))

			// Admin endpoints (require admin privileges)
			adminMiddleware := auth.RequireAdmin(tokenService, s.userRepo)
			api.Get("/users", adaptor.HTTPHandler(adminMiddleware(http.HandlerFunc(s.handleListUsers))))
			api.Put("/users/{user_id}/admin", adaptor.HTTPHandler(adminMiddleware(http.HandlerFunc(s.handleUpdateUserAdmin))))
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
	// Apply middleware in reverse order (last middleware is applied first)
	handler = RecoveryMiddleware(handler)
	handler = LoggingMiddleware(handler)
	handler = ContentTypeMiddleware(handler)
	handler = CORSMiddleware(handler)

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
