package cmd

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/javi11/altmount/frontend"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/rclone"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/altmount/internal/webdav"
	"github.com/spf13/cobra"
)

// For development, serve static files from disk
// In production, these would be embedded
var frontendBuildPath = "/app/frontend/dist"

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
	// 1. Load and validate configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return err
	}

	if err := cfg.ValidateDirectories(); err != nil {
		slog.Error("directory validation failed", "err", err)
		return err
	}

	// Setup logging
	logger := slogutil.SetupLogRotationWithFallback(cfg.Log, cfg.Log.Level)
	slog.SetDefault(logger)

	// 2. Create context and managers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configManager := config.NewManager(cfg, configFile)
	poolManager := pool.NewManager(ctx)

	// 3. Initialize core services
	db, err := initializeDatabase(cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		logger.Info("Closing database")
		if err := db.Close(); err != nil {
			logger.Error("failed to close database", "err", err)
		}
	}()

	metadataService, metadataReader := initializeMetadata(cfg)

	// 4. Setup network services
	if err := setupNNTPPool(ctx, cfg, poolManager, logger); err != nil {
		return err
	}
	defer func() {
		logger.Info("Clearing NNTP pool")
		if err := poolManager.ClearPool(); err != nil {
			logger.Error("failed to clear NNTP pool", "err", err)
		}
	}()

	mountService := rclone.NewMountService(configManager)

	var rcloneRCClient = setupRCloneClient(cfg, configManager, logger)
	if cfg.RClone.MountEnabled != nil && *cfg.RClone.MountEnabled {
		rcloneRCClient = mountService.GetManager()
	}

	// 5. Initialize importer and filesystem
	repos := setupRepositories(db, logger)

	// Create progress broadcaster for WebSocket progress updates
	progressBroadcaster := progress.NewProgressBroadcaster()

	importerService, err := initializeImporter(cfg, metadataService, db, poolManager, rcloneRCClient, configManager.GetConfigGetter(), progressBroadcaster, ctx, logger)
	if err != nil {
		return err
	}
	defer func() {
		logger.Info("Closing importer service")
		if err := importerService.Close(); err != nil {
			logger.Error("failed to close importer service", "err", err)
		}
	}()

	fs := initializeFilesystem(metadataService, repos.HealthRepo, poolManager, configManager.GetConfigGetter(), logger)

	// 6. Setup web services
	app, debugMode := createFiberApp(cfg, logger)
	authService := setupAuthService(repos.UserRepo, logger)
	arrsService := arrs.NewService(configManager.GetConfigGetter(), configManager, logger)

	apiServer := setupAPIServer(app, repos, authService, configManager, metadataReader, poolManager, importerService, arrsService, mountService, progressBroadcaster, logger)

	webdavHandler, err := setupWebDAV(cfg, fs, authService, repos.UserRepo, configManager, logger)
	if err != nil {
		return err
	}

	// Setup SPA routes
	setupSPARoutes(app)

	// 7. Register config change handlers
	pool.RegisterConfigHandlers(configManager, poolManager, logger)
	webdav.RegisterConfigHandlers(configManager, webdavHandler, logger)
	api.RegisterLogLevelHandler(configManager, debugMode, logger)

	healthWorker, err := startHealthWorker(ctx, cfg, repos.HealthRepo, poolManager, configManager, rcloneRCClient, arrsService, logger)
	if err != nil {
		logger.Warn("Health worker initialization failed", "err", err)
	}
	if healthWorker != nil {
		apiServer.SetHealthWorker(healthWorker)
	}

	// ARRs service status logging
	if cfg.Arrs.Enabled != nil && *cfg.Arrs.Enabled {
		logger.Info("Arrs service ready for health monitoring and repair")
	} else {
		logger.Info("Arrs service is disabled in configuration")
	}

	// 9. Create HTTP server
	customServer := createHTTPServer(app, webdavHandler, cfg.WebDAV.Port, cfg.ProfilerEnabled)

	logger.Info("AltMount server started",
		"port", cfg.WebDAV.Port,
		"webdav_path", "/webdav",
		"api_path", "/api",
		"providers", len(cfg.Providers),
		"download_workers", cfg.Streaming.MaxDownloadWorkers,
		"processor_workers", cfg.Import.MaxProcessorWorkers)

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	// Start custom server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := customServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Custom server error", "error", err)
			serverErr <- err
		}
	}()

	// Start mount service after HTTP server is running
	// This ensures the WebDAV server is ready to accept connections
	go func() {
		// Wait for HTTP server to be fully ready
		time.Sleep(2 * time.Second)

		if err := startMountService(ctx, cfg, mountService, logger); err != nil {
			logger.Warn("Mount service failed to start", "err", err)
		}
	}()

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

	// Stop RClone mount service if running
	if cfg.RClone.MountEnabled != nil && *cfg.RClone.MountEnabled {
		if err := mountService.Stop(ctx); err != nil {
			logger.Error("Failed to stop mount service", "error", err)
		} else {
			logger.Info("RClone mount service stopped")
		}
	}

	// Shutdown custom server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.Info("Shutting down server...")
	if err := customServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down server", "error", err)
		return err
	}
	logger.Info("Server shutdown completed")

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
		// Docker or development - serve static files with SPA fallback
		app.All("/*", filesystem.New(filesystem.Config{
			Root:         http.Dir(frontendPath),
			NotFoundFile: "index.html",
			Index:        "index.html",
		}))
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
