package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/scraper"
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
	authService     *auth.Service
	userRepo        *database.UserRepository
	configManager   ConfigManager
	metadataReader  *metadata.MetadataReader
	healthWorker    *health.HealthWorker
	importerService *importer.Service
	poolManager     pool.Manager
	scraperService  *scraper.Service
	logger          *slog.Logger
	startTime       time.Time
	mux             *http.ServeMux
}

// NewServer creates a new API server that registers routes on the provided mux
func NewServer(
	config *Config,
	queueRepo *database.Repository,
	healthRepo *database.HealthRepository,
	authService *auth.Service,
	userRepo *database.UserRepository,
	configManager ConfigManager,
	metadataReader *metadata.MetadataReader,
	poolManager pool.Manager,
	mux *http.ServeMux,
	importService *importer.Service,
	scraperService *scraper.Service) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	server := &Server{
		config:          config,
		queueRepo:       queueRepo,
		healthRepo:      healthRepo,
		authService:     authService,
		userRepo:        userRepo,
		configManager:   configManager,
		metadataReader:  metadataReader,
		importerService: importService, // Will be set later via SetImporterService
		poolManager:     poolManager,
		scraperService:  scraperService,
		logger:          slog.Default(),
		startTime:       time.Now(),
		mux:             mux,
	}

	server.setupRoutes()
	return server
}

// SetHealthWorker sets the health worker reference for the server
func (s *Server) SetHealthWorker(healthWorker *health.HealthWorker) {
	s.healthWorker = healthWorker
}

// ServeHTTP implements http.Handler interface (for backwards compatibility)
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// setupRoutes configures all API routes with middleware on the shared mux
func (s *Server) setupRoutes() {
	// Register API routes with middleware
	apiHandler := s.applyMiddleware(http.HandlerFunc(s.handleAPI))
	s.mux.Handle(s.config.Prefix+"/", http.StripPrefix(s.config.Prefix, apiHandler))

	// SABnzbd-compatible API endpoints (conditionally enabled and protected by API key authentication)
	if s.configManager != nil {
		config := s.configManager.GetConfig()
		if config.SABnzbd.Enabled != nil && *config.SABnzbd.Enabled {
			sabnzbdHandler := s.applyMiddleware(http.HandlerFunc(s.handleSABnzbd))
			s.mux.Handle("/sabnzbd/api/", sabnzbdHandler)
		}
	}

}

// handleAPI routes API requests to appropriate handlers
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	// Create internal mux for API routing
	apiMux := http.NewServeMux()

	// Queue endpoints
	apiMux.HandleFunc("GET /queue", s.handleListQueue)
	apiMux.HandleFunc("GET /queue/{id}", s.handleGetQueue)
	apiMux.HandleFunc("DELETE /queue/{id}", s.handleDeleteQueue)
	apiMux.HandleFunc("POST /queue/{id}/retry", s.handleRetryQueue)
	apiMux.HandleFunc("GET /queue/stats", s.handleGetQueueStats)
	apiMux.HandleFunc("DELETE /queue/completed", s.handleClearCompletedQueue)

	// Health endpoints
	apiMux.HandleFunc("GET /health", s.handleListHealth)
	apiMux.HandleFunc("GET /health/{id}", s.handleGetHealth)
	apiMux.HandleFunc("DELETE /health/{id}", s.handleDeleteHealth)
	apiMux.HandleFunc("POST /health/{id}/retry", s.handleRetryHealth)
	apiMux.HandleFunc("POST /health/{id}/repair", s.handleRepairHealth)
	apiMux.HandleFunc("GET /health/corrupted", s.handleListCorrupted)
	apiMux.HandleFunc("GET /health/stats", s.handleGetHealthStats)
	apiMux.HandleFunc("DELETE /health/cleanup", s.handleCleanupHealth)
	apiMux.HandleFunc("POST /health/check", s.handleAddHealthCheck)
	apiMux.HandleFunc("GET /health/worker/status", s.handleGetHealthWorkerStatus)
	apiMux.HandleFunc("POST /health/{id}/check-now", s.handleDirectHealthCheck)
	apiMux.HandleFunc("POST /health/{id}/cancel", s.handleCancelHealthCheck)

	// File endpoints (if metadata reader is available)
	if s.metadataReader != nil {
		apiMux.HandleFunc("GET /files/info", s.handleGetFileMetadata)
	}

	// Import endpoints (if importer service is available)
	if s.importerService != nil {
		apiMux.HandleFunc("POST /import/scan", s.handleStartManualScan)
		apiMux.HandleFunc("GET /import/scan/status", s.handleGetScanStatus)
		apiMux.HandleFunc("DELETE /import/scan", s.handleCancelScan)
		apiMux.HandleFunc("POST /import/file", s.handleManualImportFile)
	}

	// System endpoints
	apiMux.HandleFunc("GET /system/stats", s.handleGetSystemStats)
	apiMux.HandleFunc("GET /system/health", s.handleGetSystemHealth)
	apiMux.HandleFunc("GET /system/pool/metrics", s.handleGetPoolMetrics)
	apiMux.HandleFunc("POST /system/cleanup", s.handleSystemCleanup)
	apiMux.HandleFunc("POST /system/restart", s.handleSystemRestart)

	// Configuration endpoints (if config manager is available)
	if s.configManager != nil {
		apiMux.HandleFunc("GET /config", s.handleGetConfig)
		apiMux.HandleFunc("PUT /config", s.handleUpdateConfig)
		apiMux.HandleFunc("PATCH /config/{section}", s.handlePatchConfigSection)
		apiMux.HandleFunc("POST /config/reload", s.handleReloadConfig)
		apiMux.HandleFunc("POST /config/validate", s.handleValidateConfig)

		// Provider management endpoints
		apiMux.HandleFunc("POST /providers/test", s.handleTestProvider)
		apiMux.HandleFunc("POST /providers", s.handleCreateProvider)
		apiMux.HandleFunc("PUT /providers/{id}", s.handleUpdateProvider)
		apiMux.HandleFunc("DELETE /providers/{id}", s.handleDeleteProvider)
		apiMux.HandleFunc("PUT /providers/reorder", s.handleReorderProviders)
	}

	// Scraper endpoints (if scraper service is available)
	if s.scraperService != nil {
		// Configuration-based instance endpoints
		apiMux.HandleFunc("GET /scraper/instances", s.handleListScraperInstances)
		apiMux.HandleFunc("GET /scraper/instances/{type}/{name}", s.handleGetScraperInstance)
		apiMux.HandleFunc("POST /scraper/instances", s.handleCreateScraperInstance)        // Deprecated
		apiMux.HandleFunc("PUT /scraper/instances/{id}", s.handleUpdateScraperInstance)    // Deprecated
		apiMux.HandleFunc("DELETE /scraper/instances/{id}", s.handleDeleteScraperInstance) // Deprecated
		apiMux.HandleFunc("POST /scraper/instances/test", s.handleTestScraperConnection)
		apiMux.HandleFunc("POST /scraper/instances/{type}/{name}/scrape", s.handleTriggerScrape)
		apiMux.HandleFunc("GET /scraper/stats", s.handleGetScraperStats)
		apiMux.HandleFunc("GET /scraper/movies/search", s.handleSearchMovies)     // Deprecated
		apiMux.HandleFunc("GET /scraper/episodes/search", s.handleSearchEpisodes) // Deprecated
		// Status and control endpoints
		apiMux.HandleFunc("GET /scraper/instances/{type}/{name}/status", s.handleGetScrapeStatus)
		apiMux.HandleFunc("GET /scraper/instances/{type}/{name}/result", s.handleGetLastScrapeResult)
		apiMux.HandleFunc("POST /scraper/instances/{type}/{name}/cancel", s.handleCancelScrape)
		apiMux.HandleFunc("GET /scraper/active", s.handleGetAllActiveScrapes)
	}

	// Authentication endpoints (if auth service is available)
	if s.authService != nil {
		// Direct authentication endpoints
		apiMux.HandleFunc("POST /auth/login", s.handleDirectLogin)
		apiMux.HandleFunc("POST /auth/register", s.handleRegister)
		apiMux.HandleFunc("GET /auth/registration-status", s.handleCheckRegistration)

		// Protected API endpoints for user management (require authentication)
		tokenService := s.authService.TokenService()
		if tokenService != nil {
			authMiddleware := auth.RequireAuth(tokenService, s.userRepo)
			apiMux.Handle("GET /user", authMiddleware(http.HandlerFunc(s.handleAuthUser)))
			apiMux.Handle("POST /user/refresh", authMiddleware(http.HandlerFunc(s.handleAuthRefresh)))
			apiMux.Handle("POST /user/logout", authMiddleware(http.HandlerFunc(s.handleAuthLogout)))
			apiMux.Handle("POST /user/api-key/regenerate", authMiddleware(http.HandlerFunc(s.handleRegenerateAPIKey)))

			// Admin endpoints (require admin privileges)
			adminMiddleware := auth.RequireAdmin(tokenService, s.userRepo)
			apiMux.Handle("GET /users", adminMiddleware(http.HandlerFunc(s.handleListUsers)))
			apiMux.Handle("PUT /users/{user_id}/admin", adminMiddleware(http.HandlerFunc(s.handleUpdateUserAdmin)))
		}
	}

	apiMux.ServeHTTP(w, r)
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
