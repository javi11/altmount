package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	fLogger "github.com/gofiber/fiber/v2/middleware/logger"
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

	// Validate directory permissions before proceeding
	if err := cfg.ValidateDirectories(); err != nil {
		slog.Default().Error("directory validation failed", "err", err)
		return err
	}

	// Setup log rotation with the loaded configuration
	logger := slogutil.SetupLogRotationWithFallback(cfg.Log, cfg.Log.Level)
	slog.SetDefault(logger)

	logger.Info("Directory validation successful",
		"metadata_path", cfg.Metadata.RootPath,
		"database_path", cfg.Database.Path,
		"log_file", cfg.Log.File)

	logger.Info("Starting AltMount server with log rotation configured",
		"log_file", cfg.Log.File,
		"log_level", getEffectiveLogLevel(cfg.Log.Level, cfg.Log.Level),
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

	// Create Fiber app
	app := fiber.New(fiber.Config{
		RequestMethods: append(
			fiber.DefaultMethods, "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK",
		),
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			logger.Error("Fiber error", "path", c.Path(), "method", c.Method(), "error", err)
			return c.Status(code).JSON(fiber.Map{
				"error": err.Error(),
			})
		},
	})

	// Conditional Fiber request logging - only in debug mode
	// We use a wrapper to allow dynamic enabling/disabling
	var debugMode bool
	effectiveLogLevel := getEffectiveLogLevel(cfg.Log.Level, cfg.Log.Level)
	debugMode = effectiveLogLevel == "debug"

	// Create the logger middleware but wrap it to check debug mode
	fiberLogger := fLogger.New()
	app.Use(func(c *fiber.Ctx) error {
		if debugMode {
			return fiberLogger(c)
		}
		return c.Next()
	})

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

	// Create API server (now using Fiber directly)
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
		nsys.ImporterService(),
		arrsService)

	apiServer.SetupRoutes(app)
	logger.Info("API server enabled with Fiber routes", "prefix", "/api")

	// Register API server for auth updates

	// Create WebDAV handler for Fiber integration
	var tokenService *token.Service
	var webdavUserRepo *database.UserRepository

	// Pass authentication services if available
	if authService != nil {
		tokenService = authService.TokenService()
		webdavUserRepo = userRepo
	}

	webdavHandler, err := webdav.NewHandler(&webdav.Config{
		Port:   cfg.WebDAV.Port,
		User:   cfg.WebDAV.User,
		Pass:   cfg.WebDAV.Password,
		Debug:  cfg.Log.Level == "debug",
		Prefix: "/webdav",
	}, nsys.FileSystem(), tokenService, webdavUserRepo, configManager.GetConfigGetter())
	if err != nil {
		logger.Error("failed to create webdav handler", "err", err)
		return err
	}

	// Register WebDAV auth updater with dynamic credentials
	webdavAuthUpdater := webdav.NewAuthUpdater()
	webdavAuthUpdater.SetAuthCredentials(webdavHandler.GetAuthCredentials())

	// Add WebDAV-specific config change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Sync WebDAV auth credentials if they changed
		if oldConfig.WebDAV.User != newConfig.WebDAV.User || oldConfig.WebDAV.Password != newConfig.WebDAV.Password {
			webdavHandler.SyncAuthCredentials()
			logger.Info("WebDAV auth credentials updated",
				"old_user", oldConfig.WebDAV.User,
				"new_user", newConfig.WebDAV.User)
		}
	})

	// Add log level config change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Determine old and new log levels (prioritize Log.Level over LogLevel)
		oldLevel := oldConfig.Log.Level
		if oldConfig.Log.Level != "" {
			oldLevel = oldConfig.Log.Level
		}

		newLevel := newConfig.Log.Level
		if newConfig.Log.Level != "" {
			newLevel = newConfig.Log.Level
		}

		// Apply log level change if it changed
		if oldLevel != newLevel {
			api.ApplyLogLevel(newLevel)
			// Update Fiber logger debug mode
			debugMode = newLevel == "debug"
			logger.Info("Log level updated dynamically",
				"old_level", oldLevel,
				"new_level", newLevel,
				"fiber_logging", debugMode)
		}
	})

	logger.Info("Initializing AltMount server components",
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

	// Add simple liveness endpoint for Docker health checks directly to Fiber
	app.Get("/live", handleFiberHealth)

	// Use middleware that bypasses Fiber's method validation
	app.All("/webdav*", adaptor.HTTPHandler(webdavHandler.GetHTTPHandler()))

	// Set up Fiber SPA routing
	setupSPARoutes(app)

	logger.Info("Starting AltMount server with Fiber",
		"port", cfg.WebDAV.Port,
		"webdav_path", "/webdav",
		"api_path", "/api",
		"providers", len(cfg.Providers),
		"download_workers", cfg.Streaming.MaxDownloadWorkers,
		"processor_workers", cfg.Import.MaxProcessorWorkers)

	routes := app.GetRoutes()
	for _, route := range routes {
		logger.Debug("Fiber route", "path", route.Path, "method", route.Method)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start Fiber server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := app.Listen(fmt.Sprintf(":%d", cfg.WebDAV.Port)); err != nil {
			logger.Error("Fiber server error", "error", err)
			serverErr <- err
		}
	}()

	logger.Info("AltMount server started successfully")

	// Wait for shutdown signal or server error
	select {
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", "signal", sig.String())
		cancel() // Cancel context to signal all services to stop
	case err := <-serverErr:
		logger.Error("Server error, shutting down", "error", err)
		cancel()
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down")
	}

	// Start graceful shutdown sequence
	logger.Info("Starting graceful shutdown sequence")

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

	// Shutdown Fiber app with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.Info("Shutting down Fiber server...")
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		logger.Error("Error shutting down Fiber app", "error", err)
		return err
	}
	logger.Info("Fiber server shutdown completed")

	logger.Info("AltMount server shutdown completed successfully")
	return nil
}

// handleFiberHealth provides a lightweight liveness check endpoint for Docker using Fiber
func handleFiberHealth(c *fiber.Ctx) error {
	response := map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	return c.JSON(response)
}

// setupSPARoutes configures Fiber SPA routing for the frontend
func setupSPARoutes(app *fiber.App) {
	// Determine frontend build path
	frontendPath := frontendBuildPath
	if _, err := os.Stat(frontendBuildPath); err != nil {
		// Development mode - serve from disk
		frontendPath = "./frontend/dist"
	}

	// Cli mode - use embedded filesystem
	buildFS, err := frontend.GetBuildFS()
	if err != nil {
		// Docker or development
		app.Static("/", frontendPath)
		app.Static("*", frontendPath+"/index.html")
	} else {
		// For embedded filesystem, we'll handle it differently below
		app.All("/*", filesystem.New(filesystem.Config{
			Root:         http.FS(buildFS),
			NotFoundFile: "index.html",
			Index:        "index.html",
		}))

		return
	}
}
