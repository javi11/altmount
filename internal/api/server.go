package api

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v2"
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
	app.Use("/sabnzbd/api", s.handleSABnzbd)

	api := app.Group(s.config.Prefix)
	// Import do not need user authentication
	api.Post("/import/file", s.handleManualImportFile)

	// Apply global middleware
	api.Use(cors.New())
	api.Use(recover.New())

	// Apply JWT authentication middleware globally except for public auth routes
	if s.authService != nil && s.userRepo != nil {
		tokenService := s.authService.TokenService()
		if tokenService != nil {
			// Define paths that should skip authentication
			skipPaths := []string{
				s.config.Prefix + "/auth/login",
				s.config.Prefix + "/auth/register",
				s.config.Prefix + "/auth/registration-status",
			}

			// Apply authentication middleware with skip paths
			api.Use(auth.RequireAuthWithSkip(tokenService, s.userRepo, skipPaths))
		}
	}

	// Queue endpoints
	api.Get("/queue", s.handleListQueue)
	api.Get("/queue/stats", s.handleGetQueueStats)
	api.Delete("/queue/completed", s.handleClearCompletedQueue)
	api.Delete("/queue/failed", s.handleClearFailedQueue)
	api.Delete("/queue/bulk", s.handleDeleteQueueBulk)
	api.Post("/queue/bulk/restart", s.handleRestartQueueBulk)
	api.Post("/queue/upload", s.handleUploadToQueue)
	api.Get("/queue/:id", s.handleGetQueue)
	api.Delete("/queue/:id", s.handleDeleteQueue)
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

	api.Get("/files/info", s.handleGetFileMetadata)

	api.Post("/import/scan", s.handleStartManualScan)
	api.Get("/import/scan/status", s.handleGetScanStatus)
	api.Delete("/import/scan", s.handleCancelScan)
	// System endpoints
	api.Get("/system/stats", s.handleGetSystemStats)
	api.Get("/system/health", s.handleGetSystemHealth)
	api.Get("/system/pool/metrics", s.handleGetPoolMetrics)
	api.Post("/system/cleanup", s.handleSystemCleanup)
	api.Post("/system/restart", s.handleSystemRestart)

	api.Get("/config", s.handleGetConfig)
	api.Put("/config", s.handleUpdateConfig)
	api.Patch("/config/:section", s.handlePatchConfigSection)
	api.Post("/config/reload", s.handleReloadConfig)
	api.Post("/config/validate", s.handleValidateConfig)

	// Provider management endpoints
	api.Post("/providers/test", s.handleTestProvider)
	api.Post("/providers", s.handleCreateProvider)
	api.Put("/providers/reorder", s.handleReorderProviders)
	api.Put("/providers/:id", s.handleUpdateProvider)
	api.Delete("/providers/:id", s.handleDeleteProvider)

	// Configuration-based instance endpoints
	api.Get("/arrs/instances", s.handleListArrsInstances)
	api.Get("/arrs/instances/:type/:name", s.handleGetArrsInstance)
	api.Post("/arrs/instances/test", s.handleTestArrsConnection)
	api.Get("/arrs/stats", s.handleGetArrsStats)

	// Direct authentication endpoints (converted to native Fiber)
	api.Post("/auth/login", s.handleDirectLogin)
	api.Post("/auth/register", s.handleRegister)
	api.Get("/auth/registration-status", s.handleCheckRegistration)

	// Protected API endpoints for user management (authentication already handled globally)
	api.Get("/user", s.handleAuthUser)
	api.Post("/user/refresh", s.handleAuthRefresh)
	api.Post("/user/logout", s.handleAuthLogout)
	api.Post("/user/api-key/regenerate", s.handleRegenerateAPIKey)

	// Admin endpoints (admin check is done inside handlers)
	api.Get("/users", s.handleListUsers)
	api.Put("/users/:user_id/admin", s.handleUpdateUserAdmin)
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
func (s *Server) checkSystemHealth(_ context.Context) SystemHealthResponse {
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
