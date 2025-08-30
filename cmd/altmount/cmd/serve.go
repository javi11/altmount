package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/frontend"
	"github.com/javi11/altmount/internal/adapters/webdav"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	"github.com/javi11/altmount/internal/integration"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/scraper"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/altmount/pkg/rclonecli"
	"github.com/spf13/cobra"
)

// For development, serve static files from disk
// In production, these would be embedded
var frontendBuildPath = "../../frontend/build"

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
	defer poolManager.ClearPool()

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
	defer nsys.Close()

	// Create shared HTTP mux
	mux := http.NewServeMux()

	// Declare auth services at function scope so WebDAV can access them
	var authService *auth.Service
	var userRepo *database.UserRepository

	// Create API server (always enabled)
	// Create repositories for API access
	dbConn := nsys.Database().Connection()
	mainRepo := database.NewRepository(dbConn)
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

	// Create media repository for scraper
	mediaRepo := database.NewMediaRepository(dbConn, logger)

	// Create scraper service
	scraperService := scraper.NewService(configManager.GetConfigGetter(), mediaRepo, logger)

	// Create API server configuration (hardcoded to /api)
	apiConfig := &api.Config{
		Prefix: "/api",
	}

	// Create API server with shared mux
	apiServer := api.NewServer(
		apiConfig,
		mainRepo,
		healthRepo,
		authService,
		userRepo,
		configManager,
		nsys.MetadataReader(),
		poolManager,
		mux,
		nsys.ImporterService(),
		scraperService)
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
			configManager.GetConfigGetter(),
			logger,
		)

		// Set health worker reference in API server
		apiServer.SetHealthWorker(healthWorker)

		// Start health worker with the main context
		if err := healthWorker.Start(ctx); err != nil {
			logger.Error("Failed to start health worker", "error", err)
		} else {
			logger.Info("Health worker started", "enabled", cfg.Health.Enabled)
		}
	} else {
		logger.Info("Health worker is disabled in configuration")
	}

	// Start scraper service if enabled
	if cfg.Scraper.Enabled != nil && *cfg.Scraper.Enabled {
		if err := scraperService.Start(); err != nil {
			logger.Error("Failed to start scraper service", "error", err)
		} else {
			logger.Info("Scraper service started")
		}
	} else {
		logger.Info("Scraper service is disabled in configuration")
	}

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

	// Stop scraper service if running
	if cfg.Scraper.Enabled != nil && *cfg.Scraper.Enabled {
		if err := scraperService.Stop(); err != nil {
			logger.Error("Failed to stop scraper service", "error", err)
		} else {
			logger.Info("Scraper service stopped")
		}
	}

	server.Stop()

	logger.Info("AltMount server shutting down gracefully")
	return nil
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
		// Development mode - serve from disk
		return http.StripPrefix("/", http.FileServer(http.Dir(frontendBuildPath)))
	}

	// Production mode - serve from embedded filesystem
	buildFS, err := frontend.GetBuildFS()
	if err != nil {
		slog.Info("Failed to get embedded filesystem", "error", err)
		// Fallback to disk if embedded fails
		return http.StripPrefix("/", http.FileServer(http.Dir(frontendBuildPath)))
	}

	return http.StripPrefix("/", http.FileServer(http.FS(buildFS)))
}
