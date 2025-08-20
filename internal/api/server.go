package api

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/javi11/altmount/internal/database"
)

// Config represents API server configuration
type Config struct {
	Enabled  bool   // Whether API is enabled
	Prefix   string // API path prefix (default: "/api")
	Username string // Optional basic auth username
	Password string // Optional basic auth password
}

// DefaultConfig returns default API configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled: true,
		Prefix:  "/api",
	}
}

// Server represents the API server
type Server struct {
	config          *Config
	queueRepo       *database.Repository
	healthRepo      *database.HealthRepository
	startTime       time.Time
	mux             *http.ServeMux
}

// NewServer creates a new API server that registers routes on the provided mux
func NewServer(config *Config, queueRepo *database.Repository, healthRepo *database.HealthRepository, mux *http.ServeMux) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	server := &Server{
		config:     config,
		queueRepo:  queueRepo,
		healthRepo: healthRepo,
		startTime:  time.Now(),
		mux:        mux,
	}

	server.setupRoutes()
	return server
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
	apiMux.HandleFunc("GET /health/corrupted", s.handleListCorrupted)
	apiMux.HandleFunc("GET /health/stats", s.handleGetHealthStats)
	apiMux.HandleFunc("DELETE /health/cleanup", s.handleCleanupHealth)

	// System endpoints
	apiMux.HandleFunc("GET /system/stats", s.handleGetSystemStats)
	apiMux.HandleFunc("GET /system/health", s.handleGetSystemHealth)
	apiMux.HandleFunc("POST /system/cleanup", s.handleSystemCleanup)
	
	apiMux.ServeHTTP(w, r)
}

// applyMiddleware applies the middleware chain to the handler
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// Apply middleware in reverse order (last middleware is applied first)
	handler = RecoveryMiddleware(handler)
	handler = LoggingMiddleware(handler)
	handler = ContentTypeMiddleware(handler)
	handler = CORSMiddleware(handler)
	
	// Apply authentication if configured
	if s.config.Username != "" && s.config.Password != "" {
		handler = BasicAuthMiddleware(s.config.Username, s.config.Password)(handler)
	}
	
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