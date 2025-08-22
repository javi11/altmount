package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/javi11/altmount/internal/adapters/webdav"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/integration"
	"github.com/javi11/altmount/internal/pool"
	"github.com/spf13/cobra"
)

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
	logger := slog.Default()

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		logger.Error("failed to load config", "err", err)
		return err
	}

	// Create config manager for dynamic configuration updates
	configManager := config.NewManager(cfg, configFile)

	// Create pool manager for dynamic NNTP connection management
	poolManager := pool.NewManager(logger)

	// Create component registry for dynamic configuration updates
	componentRegistry := config.NewComponentRegistry(logger)

	// Create logging updater
	loggingUpdater := config.NewLoggingUpdater(cfg.Debug)
	componentRegistry.RegisterLogging(loggingUpdater)

	// Register configuration change handler
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		logger.Info("Configuration updated")

		// Handle provider changes dynamically
		providersChanged := len(oldConfig.Providers) != len(newConfig.Providers)
		if !providersChanged {
			// Check if any provider details changed
			for i, oldProvider := range oldConfig.Providers {
				if i >= len(newConfig.Providers) {
					providersChanged = true
					break
				}
				newProvider := newConfig.Providers[i]
				if oldProvider.Host != newProvider.Host ||
					oldProvider.Port != newProvider.Port ||
					oldProvider.Username != newProvider.Username ||
					oldProvider.Password != newProvider.Password ||
					oldProvider.MaxConnections != newProvider.MaxConnections ||
					oldProvider.TLS != newProvider.TLS ||
					oldProvider.InsecureTLS != newProvider.InsecureTLS {
					providersChanged = true
					break
				}
			}
		}

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

		// Apply dynamic configuration updates via component registry
		componentRegistry.ApplyUpdates(oldConfig, newConfig)
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

	// Create NZB system with metadata + queue
	nsys, err := integration.NewNzbSystem(integration.NzbConfig{
		QueueDatabasePath:  cfg.Database.Path,
		MetadataRootPath:   cfg.Metadata.RootPath,
		MaxRangeSize:       cfg.Streaming.MaxRangeSize,
		StreamingChunkSize: cfg.Streaming.StreamingChunkSize,
		WatchPath:          cfg.WatchPath,
		Password:           cfg.RClone.Password,
		Salt:               cfg.RClone.Salt,
		DownloadWorkers:    cfg.Workers.Download,
		ProcessorWorkers:   cfg.Workers.Processor,
	}, poolManager)
	if err != nil {
		logger.Error("failed to init NZB system", "err", err)
		return err
	}
	defer nsys.Close()

	// Register NZB system as worker pool updater
	componentRegistry.RegisterWorkerPool(nsys)

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

	// Create API server configuration (hardcoded to /api)
	apiConfig := &api.Config{
		Prefix: "/api",
	}

	// Create API server with shared mux
	apiServer := api.NewServer(apiConfig, mainRepo, healthRepo, authService, userRepo, configManager, nsys.MetadataReader(), mux)
	logger.Info("API server enabled", "prefix", "/api")

	// Register API server for auth updates
	apiAuthUpdater := api.NewAuthUpdater(apiServer)
	componentRegistry.RegisterAPI(apiAuthUpdater)

	// Create WebDAV server with shared mux
	var tokenService *token.Service
	var webdavUserRepo *database.UserRepository

	// Pass authentication services if available
	if authService != nil {
		tokenService = authService.TokenService()
		webdavUserRepo = userRepo
	}

	server, err := webdav.NewServer(&webdav.Config{
		Port:  cfg.WebDAV.Port,
		User:  cfg.WebDAV.User,
		Pass:  cfg.WebDAV.Password,
		Debug: cfg.WebDAV.Debug || cfg.Debug,
	}, nsys.FileSystem(), mux, tokenService, webdavUserRepo)
	if err != nil {
		logger.Error("failed to start webdav", "err", err)
		return err
	}

	// Register WebDAV auth updater with dynamic credentials
	webdavAuthUpdater := webdav.NewAuthUpdater()
	webdavAuthUpdater.SetAuthCredentials(server.GetAuthCredentials())
	componentRegistry.RegisterWebDAV(webdavAuthUpdater)

	logger.Info("Starting AltMount server",
		"webdav_port", cfg.WebDAV.Port,
		"providers", len(cfg.Providers),
		"download_workers", cfg.Workers.Download,
		"processor_workers", cfg.Workers.Processor)

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
