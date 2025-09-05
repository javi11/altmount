package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/frontend"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	"github.com/javi11/altmount/internal/integration"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/altmount/internal/webdav"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/spf13/cobra"
)

// For development, serve static files from disk
// In production, these would be embedded
var frontendBuildPath = "/app/frontend/dist"

// getEffectiveLogLevel returns the effective log level, preferring new config over legacy
func getEffectiveLogLevel(newLevel, legacyLevel string) string {
	if newLevel != "" {
		return newLevel
	}
	if legacyLevel != "" {
		return legacyLevel
	}
	return "info"
}

func init() {
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the AltMount WebDAV server",
		Long:  `Start the AltMount WebDAV server using configuration from YAML file.`,
		RunE:  runServe,
	}

	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load configuration first (using default logger for config loading errors)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		slog.Default().Error("failed to load config", "err", err)
		return err
	}

	// Setup log rotation with the loaded configuration
	logger := slogutil.SetupLogRotationWithFallback(cfg.Log, cfg.LogLevel)
	slog.SetDefault(logger)

	logger.Info("Starting AltMount server with log rotation configured",
		"log_file", cfg.Log.File,
		"log_level", getEffectiveLogLevel(cfg.Log.Level, cfg.LogLevel),
		"max_size_mb", cfg.Log.MaxSize,
		"max_age_days", cfg.Log.MaxAge,
		"max_backups", cfg.Log.MaxBackups,
		"compress", cfg.Log.Compress)

	// Create config manager for dynamic configuration updates
	configManager := config.NewManager(cfg, configFile)

	// Create pool manager for dynamic NNTP connection management
	poolManager := pool.NewManager(logger)

	// Register configuration change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		logger.Info("Configuration updated", "new_config", newConfig)

		// Handle provider changes dynamically using comprehensive comparison
		providersChanged := !oldConfig.ProvidersEqual(newConfig)

		if providersChanged {
			logger.Info("NNTP providers changed - updating connection pool",
				"old_count", len(oldConfig.Providers),
				"new_count", len(newConfig.Providers))

			// Update pool with new providers
			providers := newConfig.ToNNTPProviders()
			if err := poolManager.SetProviders(providers); err != nil {
				logger.Error("Failed to update NNTP connection pool", "err", err)
			} else {
				if len(providers) > 0 {
					logger.Info("NNTP connection pool updated successfully", "provider_count", len(providers))
				} else {
					logger.Info("NNTP connection pool cleared - no providers configured")
				}
			}
		}

		// Log changes that still require restart
		if oldConfig.Metadata.RootPath != newConfig.Metadata.RootPath {
			logger.Info("Metadata root path changed (restart required)",
				"old", oldConfig.Metadata.RootPath,
				"new", newConfig.Metadata.RootPath)
		}
	})

	// Initialize pool if providers are configured
	if len(cfg.Providers) > 0 {
		providers := cfg.ToNNTPProviders()
		if err := poolManager.SetProviders(providers); err != nil {
			logger.Error("failed to create initial NNTP pool", "err", err)
			return err
		}
		logger.Info("NNTP connection pool initialized", "provider_count", len(cfg.Providers))
	} else {
		logger.Info("Starting server without NNTP providers - configure via API to enable downloads")
	}
	defer func() {
		_ = poolManager.ClearPool()
	}()

	// Create rclone client for VFS notifications (if configured)
	var rcloneClient rclonecli.RcloneRcClient
	if cfg.RClone.VFSEnabled != nil && *cfg.RClone.VFSEnabled && cfg.RClone.VFSUrl != "" {
		rcloneConfig := &rclonecli.Config{
			VFSEnabled: *cfg.RClone.VFSEnabled,
			VFSUrl:     cfg.RClone.VFSUrl,
			VFSUser:    cfg.RClone.VFSUser,
			VFSPass:    cfg.RClone.VFSPass,
		}

		httpClient := &http.Client{}
		rcloneClient = rclonecli.NewRcloneRcClient(rcloneConfig, httpClient)
		logger.Info("RClone VFS client initialized", "vfs_url", cfg.RClone.VFSUrl)
	} else {
		logger.Info("RClone VFS notifications disabled")
	}

	// Create NZB system with metadata + queue
	nsys, err := integration.NewNzbSystem(integration.NzbConfig{
		QueueDatabasePath:   cfg.Database.Path,
		MetadataRootPath:    cfg.Metadata.RootPath,
		MaxRangeSize:        cfg.Streaming.MaxRangeSize,
		StreamingChunkSize:  cfg.Streaming.StreamingChunkSize,
		Password:            cfg.RClone.Password,
		Salt:                cfg.RClone.Salt,
		MaxDownloadWorkers:  cfg.Streaming.MaxDownloadWorkers,
		MaxProcessorWorkers: cfg.Import.MaxProcessorWorkers,
	}, poolManager, configManager.GetConfigGetter(), rcloneClient)
	if err != nil {
		logger.Error("failed to init NZB system", "err", err)
		return err
	}
	defer func() {
		_ = nsys.Close()
	}()

	// Create shared HTTP mux
	mux := http.NewServeMux()

	// Declare auth services at function scope so WebDAV can access them
	var authService *auth.Service
	var userRepo *database.UserRepository

	// Create API server (always enabled)
	// Create repositories for API access
	dbConn := nsys.Database().Connection()
	mainRepo := database.NewRepository(dbConn)

	// Create media repository for scraper and health correlation
	mediaRepo := database.NewMediaRepository(dbConn, logger)

	// Create health repository
	healthRepo := database.NewHealthRepository(dbConn)
	userRepo = database.NewUserRepository(dbConn)

	// Create authentication service
	authConfig := auth.LoadConfigFromEnv()
	if authConfig != nil {
		var err error
		authService, err = auth.NewService(authConfig, userRepo)
		if err != nil {
			logger.Warn("Failed to create authentication service", "err", err)
		} else {
			// Setup OAuth providers
			err = authService.SetupProviders(authConfig)
			if err != nil {
				logger.Warn("Failed to setup OAuth providers", "err", err)
			} else {
				logger.Info("Authentication service initialized")
			}
		}
	}

	// Create arrs service for health monitoring and repair
	arrsService := arrs.NewService(configManager.GetConfigGetter(), logger)

	// Create API server configuration (hardcoded to /api)
	apiConfig := &api.Config{
		Prefix: "/api",
	}

	// Create API server with shared mux
	apiServer := api.NewServer(
		apiConfig,
		mainRepo,
		healthRepo,
		mediaRepo,
		authService,
		userRepo,
		configManager,
		nsys.MetadataReader(),
		poolManager,
		mux,
		nsys.ImporterService(),
		arrsService)
	logger.Info("API server enabled", "prefix", "/api")

	// Register API server for auth updates

	// Create WebDAV server with shared mux
	var tokenService *token.Service
	var webdavUserRepo *database.UserRepository

	// Pass authentication services if available
	if authService != nil {
		tokenService = authService.TokenService()
		webdavUserRepo = userRepo
	}

	server, err := webdav.NewServer(&webdav.Config{
		Port:   cfg.WebDAV.Port,
		User:   cfg.WebDAV.User,
		Pass:   cfg.WebDAV.Password,
		Debug:  cfg.LogLevel == "debug",
		Prefix: "/webdav",
	}, nsys.FileSystem(), mux, tokenService, webdavUserRepo, configManager.GetConfigGetter())
	if err != nil {
		logger.Error("failed to start webdav", "err", err)
		return err
	}

	// Register WebDAV auth updater with dynamic credentials
	webdavAuthUpdater := webdav.NewAuthUpdater()
	webdavAuthUpdater.SetAuthCredentials(server.GetAuthCredentials())

	// Add WebDAV-specific config change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Sync WebDAV auth credentials if they changed
		if oldConfig.WebDAV.User != newConfig.WebDAV.User || oldConfig.WebDAV.Password != newConfig.WebDAV.Password {
			server.SyncAuthCredentials()
			logger.Info("WebDAV auth credentials updated",
				"old_user", oldConfig.WebDAV.User,
				"new_user", newConfig.WebDAV.User)
		}
	})

	// Add log level config change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Determine old and new log levels (prioritize Log.Level over LogLevel)
		oldLevel := oldConfig.LogLevel
		if oldConfig.Log.Level != "" {
			oldLevel = oldConfig.Log.Level
		}

		newLevel := newConfig.LogLevel
		if newConfig.Log.Level != "" {
			newLevel = newConfig.Log.Level
		}

		// Apply log level change if it changed
		if oldLevel != newLevel {
			api.ApplyLogLevel(newLevel)
			logger.Info("Log level updated dynamically",
				"old_level", oldLevel,
				"new_level", newLevel)
		}
	})

	logger.Info("Starting AltMount server",
		"webdav_port", cfg.WebDAV.Port,
		"providers", len(cfg.Providers),
		"download_workers", cfg.Streaming.MaxDownloadWorkers,
		"processor_workers", cfg.Import.MaxProcessorWorkers)

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create health worker if enabled
	var healthWorker *health.HealthWorker
	if *cfg.Health.Enabled {
		// Create metadata service for health worker
		metadataService := metadata.NewMetadataService(cfg.Metadata.RootPath)

		// Create health checker
		healthChecker := health.NewHealthChecker(
			healthRepo,
			metadataService,
			poolManager,
			configManager.GetConfigGetter(),
			rcloneClient, // Pass rclone client for VFS notifications
			nil,          // No event handler for now
		)

		healthWorker = health.NewHealthWorker(
			healthChecker,
			healthRepo,
			metadataService,
			arrsService,
			configManager.GetConfigGetter(),
			logger,
		)

		// Set health worker reference in API server
		apiServer.SetHealthWorker(healthWorker)

		// Start health worker with the main context
		if err := healthWorker.Start(ctx); err != nil {
			logger.Error("Failed to start health worker", "error", err)
		} else {
			logger.Info("Health worker started", "enabled", *cfg.Health.Enabled)
		}
	} else {
		logger.Info("Health worker is disabled in configuration")
	}

	// ARRs service is now always ready for health monitoring and repair
	if cfg.Arrs.Enabled != nil && *cfg.Arrs.Enabled {
		logger.Info("Arrs service ready for health monitoring and repair")
	} else {
		logger.Info("Arrs service is disabled in configuration")
	}

	// Add simple liveness endpoint for Docker health checks
	mux.HandleFunc("/live", handleSimpleHealth)

	mux.Handle("/", getStaticFileHandler())

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		if err := server.Start(ctx); err != nil {
			slog.Error("WebDAV server error", "err", err)
		}
	}()

	// Wait for shutdown signal or server error
	signalHandler(ctx)

	// Stop health worker if running
	if healthWorker != nil {
		if err := healthWorker.Stop(); err != nil {
			logger.Error("Failed to stop health worker", "error", err)
		} else {
			logger.Info("Health worker stopped")
		}
	}

	// ARRs service cleanup (no background processes to stop)
	if cfg.Arrs.Enabled != nil && *cfg.Arrs.Enabled {
		logger.Info("Arrs service cleanup completed")
	}

	server.Stop()

	logger.Info("AltMount server shutting down gracefully")
	return nil
}

// handleSimpleHealth provides a lightweight liveness check endpoint for Docker
func handleSimpleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	
	json.NewEncoder(w).Encode(response)
}

func signalHandler(ctx context.Context) {
	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	select {
	case <-ctx.Done():
	case <-c:
	}
}

func getStaticFileHandler() http.Handler {
	// Check if we should use embedded filesystem or development path
	if _, err := os.Stat(frontendBuildPath); err == nil {
		// Development mode - serve from disk with SPA fallback
		return createSPAHandler(http.Dir(frontendBuildPath), false)
	}

	// Production mode - serve from embedded filesystem with SPA fallback
	buildFS, err := frontend.GetBuildFS()
	if err != nil {
		slog.Info("Failed to get embedded filesystem", "error", err)
		// Fallback to disk if embedded fails
		return createSPAHandler(http.Dir(frontendBuildPath), false)
	}

	return createSPAHandler(http.FS(buildFS), true)
}

// createSPAHandler creates a handler that serves static files with SPA fallback
func createSPAHandler(fs http.FileSystem, isEmbedded bool) http.Handler {
	fileServer := http.FileServer(fs)
	
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Try to open the requested file
		var file http.File
		var err error
		
		if isEmbedded {
			// For embedded FS, we need to handle the path differently
			if embeddedFS, ok := fs.(http.FileSystem); ok {
				file, err = embeddedFS.Open(path)
			}
		} else {
			file, err = fs.Open(path)
		}

		// If file exists, serve it normally
		if err == nil {
			if stat, err := file.Stat(); err == nil && !stat.IsDir() {
				file.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			file.Close()
		}

		// File doesn't exist or is a directory, check if it's a static asset request
		// Static assets typically have file extensions
		if hasFileExtension(path) {
			// This looks like a static asset request that doesn't exist, return 404
			http.NotFound(w, r)
			return
		}

		// No file extension - assume it's a client-side route, serve index.html
		r.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r)
	})
}

// hasFileExtension checks if the path appears to be requesting a static asset
func hasFileExtension(path string) bool {
	// Common static asset extensions
	staticExtensions := []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot", ".map", ".json", ".xml", ".txt"}
	
	for _, ext := range staticExtensions {
		if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}
