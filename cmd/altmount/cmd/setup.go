package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-pkgz/auth/v2/token"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	fLogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/auth"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/health"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/rclone"
	"github.com/javi11/altmount/internal/webdav"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// repositorySet holds all database repositories
type repositorySet struct {
	MainRepo   *database.Repository
	MediaRepo  *database.MediaRepository
	HealthRepo *database.HealthRepository
	UserRepo   *database.UserRepository
}

// initializeDatabase creates and initializes the database
func initializeDatabase(cfg *config.Config, logger *slog.Logger) (*database.DB, error) {
	dbConfig := database.Config{
		DatabasePath: cfg.Database.Path,
	}

	db, err := database.NewDB(dbConfig)
	if err != nil {
		logger.Error("failed to initialize database", "err", err)
		return nil, err
	}

	return db, nil
}

// initializeMetadata creates metadata service and reader
func initializeMetadata(cfg *config.Config) (*metadata.MetadataService, *metadata.MetadataReader) {
	metadataService := metadata.NewMetadataService(cfg.Metadata.RootPath)
	metadataReader := metadata.NewMetadataReader(metadataService)
	return metadataService, metadataReader
}

// initializeImporter creates and starts the importer service
func initializeImporter(
	cfg *config.Config,
	metadataService *metadata.MetadataService,
	db *database.DB,
	poolManager pool.Manager,
	rcloneClient rclonecli.RcloneRcClient,
	configGetter config.ConfigGetter,
	broadcaster *progress.ProgressBroadcaster,
	ctx context.Context,
	logger *slog.Logger,
) (*importer.Service, error) {
	// Set defaults for workers if not configured
	maxProcessorWorkers := cfg.Import.MaxProcessorWorkers
	if maxProcessorWorkers <= 0 {
		maxProcessorWorkers = 2 // Default: 2 parallel workers
	}

	serviceConfig := importer.ServiceConfig{
		Workers: maxProcessorWorkers,
	}

	importerService, err := importer.NewService(serviceConfig, metadataService, db, poolManager, rcloneClient, configGetter, broadcaster)
	if err != nil {
		logger.Error("failed to create importer service", "err", err)
		return nil, err
	}

	// Start importer service
	if err := importerService.Start(ctx); err != nil {
		logger.Error("failed to start importer service", "err", err)
		return nil, err
	}

	return importerService, nil
}

// initializeFilesystem creates the NZB filesystem with health tracking
func initializeFilesystem(
	metadataService *metadata.MetadataService,
	healthRepo *database.HealthRepository,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	logger *slog.Logger,
) *nzbfilesystem.NzbFilesystem {
	// Reset all in-progress file health checks on start up
	if err := healthRepo.ResetFileAllChecking(); err != nil {
		logger.Error("failed to reset in progress file health", "err", err)
	}

	// Create metadata-based remote file handler
	metadataRemoteFile := nzbfilesystem.NewMetadataRemoteFile(
		metadataService,
		healthRepo,
		poolManager,
		configGetter,
	)

	// Create filesystem backed by metadata
	return nzbfilesystem.NewNzbFilesystem(metadataRemoteFile)
}

// setupNNTPPool initializes the NNTP connection pool
func setupNNTPPool(_ context.Context, cfg *config.Config, poolManager pool.Manager, logger *slog.Logger) error {
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
	return nil
}

// setupRCloneClient creates an RClone client if enabled
func setupRCloneClient(cfg *config.Config, configManager *config.Manager, logger *slog.Logger) rclonecli.RcloneRcClient {
	if cfg.RClone.RCEnabled != nil && *cfg.RClone.RCEnabled {
		httpClient := &http.Client{}
		rcloneClient := rclonecli.NewRcloneRcClient(configManager, httpClient)

		if cfg.RClone.RCUrl != "" {
			logger.Info("RClone RC client initialized for external server",
				"rc_url", cfg.RClone.RCUrl)
		} else {
			logger.Info("RClone RC client initialized for internal server",
				"rc_port", cfg.RClone.RCPort)
		}
		return rcloneClient
	}

	logger.Info("RClone RC notifications disabled")
	return nil
}

// createFiberApp creates and configures the Fiber application
func createFiberApp(cfg *config.Config, logger *slog.Logger) (*fiber.App, *bool) {
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
	debugMode := cfg.Log.Level == "debug"

	// Create the logger middleware but wrap it to check debug mode
	fiberLogger := fLogger.New()
	app.Use(func(c *fiber.Ctx) error {
		if debugMode {
			return fiberLogger(c)
		}
		return c.Next()
	})

	return app, &debugMode
}

// setupRepositories creates all database repositories
func setupRepositories(db *database.DB, logger *slog.Logger) *repositorySet {
	dbConn := db.Connection()

	return &repositorySet{
		MainRepo:   database.NewRepository(dbConn),
		MediaRepo:  database.NewMediaRepository(dbConn, logger),
		HealthRepo: database.NewHealthRepository(dbConn),
		UserRepo:   database.NewUserRepository(dbConn),
	}
}

// setupAuthService creates and initializes the authentication service
func setupAuthService(userRepo *database.UserRepository, logger *slog.Logger) *auth.Service {
	authConfig := auth.LoadConfigFromEnv()
	if authConfig == nil {
		return nil
	}

	authService, err := auth.NewService(authConfig, userRepo)
	if err != nil {
		logger.Warn("Failed to create authentication service", "err", err)
		return nil
	}

	// Setup OAuth providers
	if err := authService.SetupProviders(authConfig); err != nil {
		logger.Warn("Failed to setup OAuth providers", "err", err)
		return nil
	}

	logger.Info("Authentication service initialized")
	return authService
}

// setupAPIServer creates and configures the API server
func setupAPIServer(
	app *fiber.App,
	repos *repositorySet,
	authService *auth.Service,
	configManager *config.Manager,
	metadataReader *metadata.MetadataReader,
	poolManager pool.Manager,
	importerService *importer.Service,
	arrsService *arrs.Service,
	mountService *rclone.MountService,
	progressBroadcaster *progress.ProgressBroadcaster,
) *api.Server {
	apiConfig := &api.Config{
		Prefix: "/api",
	}

	apiServer := api.NewServer(
		apiConfig,
		repos.MainRepo,
		repos.HealthRepo,
		repos.MediaRepo,
		authService,
		repos.UserRepo,
		configManager,
		metadataReader,
		poolManager,
		importerService,
		arrsService,
		mountService,
		progressBroadcaster,
	)

	apiServer.SetupRoutes(app)

	// Register RClone handlers
	rcloneHandlers := api.NewRCloneHandlers(mountService, configManager.GetConfigGetter())
	api.RegisterRCloneRoutes(app.Group("/api"), rcloneHandlers)

	// Add simple liveness endpoint for Docker health checks
	app.Get("/live", handleFiberHealth)

	return apiServer
}

// setupWebDAV creates and configures the WebDAV handler
func setupWebDAV(
	cfg *config.Config,
	fs *nzbfilesystem.NzbFilesystem,
	authService *auth.Service,
	userRepo *database.UserRepository,
	configManager *config.Manager,
	logger *slog.Logger,
) (*webdav.Handler, error) {
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
		Prefix: "/webdav",
	}, fs, tokenService, webdavUserRepo, configManager.GetConfigGetter())

	if err != nil {
		logger.Error("failed to create webdav handler", "err", err)
		return nil, err
	}

	return webdavHandler, nil
}

// startHealthWorker creates and starts the health monitoring worker
func startHealthWorker(
	ctx context.Context,
	cfg *config.Config,
	healthRepo *database.HealthRepository,
	poolManager pool.Manager,
	configManager *config.Manager,
	rcloneClient rclonecli.RcloneRcClient,
	arrsService *arrs.Service,
	logger *slog.Logger,
) (*health.HealthWorker, *health.LibrarySyncWorker, error) {
	// Create metadata service for health worker
	metadataService := metadata.NewMetadataService(cfg.Metadata.RootPath)

	// Create health checker
	healthChecker := health.NewHealthChecker(
		healthRepo,
		metadataService,
		poolManager,
		configManager.GetConfigGetter(),
		rcloneClient,
		nil, // No event handler for now
	)

	healthWorker := health.NewHealthWorker(
		healthChecker,
		healthRepo,
		metadataService,
		arrsService,
		configManager.GetConfigGetter(),
		logger,
	)

	// Create library sync worker (always create, but only start if enabled)
	librarySyncWorker := health.NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfigGetter(),
		logger,
	)

	// Only start health system if enabled
	if cfg.Health.Enabled != nil && *cfg.Health.Enabled {
		// Start health worker with the main context
		if err := healthWorker.Start(ctx); err != nil {
			logger.Error("Failed to start health worker", "error", err)
			return nil, nil, err
		}

		// Start library sync worker
		librarySyncWorker.StartLibrarySync(ctx)

		logger.Info("Health system started")
	} else {
		logger.Info("Health system disabled - no health monitoring or repairs will occur")
	}

	return healthWorker, librarySyncWorker, nil
}

// startMountService starts the RClone mount service if enabled
func startMountService(ctx context.Context, cfg *config.Config, mountService *rclone.MountService, logger *slog.Logger) error {
	if cfg.RClone.MountEnabled == nil || !*cfg.RClone.MountEnabled {
		logger.Info("RClone mount service is disabled in configuration")
		return nil
	}

	if err := mountService.Start(ctx); err != nil {
		logger.Error("Failed to start mount service", "error", err)
		return err
	}

	logger.Info("RClone mount service started", "mount_point", cfg.MountPath)
	return nil
}

// createHTTPServer creates the HTTP server with routing
func createHTTPServer(app *fiber.App, webdavHandler *webdav.Handler, port int, profilerEnabled bool) *http.Server {
	// Mount WebDAV handler directly (no Fiber adapter needed)
	webdavHTTPHandler := webdavHandler.GetHTTPHandler()

	// Convert Fiber app to HTTP handler for all other routes
	fiberHTTPHandler := adaptor.FiberApp(app)

	// Create a handler that routes between WebDAV and Fiber
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Route profiler requests if enabled
		if profilerEnabled && strings.HasPrefix(path, "/debug/pprof") {
			http.DefaultServeMux.ServeHTTP(w, r)
			return
		}

		// Route WebDAV requests directly to WebDAV handler
		if len(path) >= 7 && path[:7] == "/webdav" {
			webdavHTTPHandler.ServeHTTP(w, r)
			return
		}

		// Route all other requests to Fiber handler
		fiberHTTPHandler.ServeHTTP(w, r)
	})

	// Create and configure the HTTP server
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mainHandler,
		IdleTimeout:  time.Minute * 5,
		WriteTimeout: time.Minute * 30,
		ReadTimeout:  time.Minute * 5,
	}
}
