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
	"github.com/javi11/altmount/internal/httpclient"
	"github.com/javi11/altmount/internal/importer"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
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
func initializeDatabase(ctx context.Context, cfg *config.Config) (*database.DB, error) {
	dbConfig := database.Config{
		DatabasePath: cfg.Database.Path,
	}

	db, err := database.NewDB(dbConfig)
	if err != nil {
		slog.ErrorContext(ctx, "failed to initialize database", "err", err)
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
	ctx context.Context,
	cfg *config.Config,
	metadataService *metadata.MetadataService,
	db *database.DB,
	poolManager pool.Manager,
	rcloneClient rclonecli.RcloneRcClient,
	configGetter config.ConfigGetter,
	broadcaster *progress.ProgressBroadcaster,
	userRepo *database.UserRepository,
	healthRepo *database.HealthRepository,
) (*importer.Service, error) {
	// Set defaults for workers if not configured
	maxProcessorWorkers := cfg.Import.MaxProcessorWorkers
	if maxProcessorWorkers <= 0 {
		maxProcessorWorkers = 2 // Default: 2 parallel workers
	}

	serviceConfig := importer.ServiceConfig{
		Workers: maxProcessorWorkers,
	}

	importerService, err := importer.NewService(serviceConfig, metadataService, db, poolManager, rcloneClient, configGetter, healthRepo, broadcaster, userRepo)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create importer service", "err", err)
		return nil, err
	}

	// Start importer service
	if err := importerService.Start(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to start importer service", "err", err)
		return nil, err
	}

	return importerService, nil
}

// initializeFilesystem creates the NZB filesystem with health tracking
func initializeFilesystem(
	ctx context.Context,
	metadataService *metadata.MetadataService,
	healthRepo *database.HealthRepository,
	poolManager pool.Manager,
	configGetter config.ConfigGetter,
	streamTracker nzbfilesystem.StreamTracker,
	segcacheMgr *segcache.Manager,
) *nzbfilesystem.NzbFilesystem {
	// Reset all in-progress file health checks on start up
	if err := healthRepo.ResetFileAllChecking(ctx); err != nil {
		slog.ErrorContext(ctx, "failed to reset in progress file health", "err", err)
	}

	// Build segment store from segment cache manager (nil if disabled)
	var segmentStore usenet.SegmentStore
	if segcacheMgr != nil {
		segmentStore = segcacheMgr.Cache()
	}

	// Create metadata-based remote file handler
	metadataRemoteFile := nzbfilesystem.NewMetadataRemoteFile(
		metadataService,
		healthRepo,
		poolManager,
		configGetter,
		streamTracker,
		segmentStore,
	)

	// Create filesystem backed by metadata
	return nzbfilesystem.NewNzbFilesystem(metadataRemoteFile)
}

// setupNNTPPool initializes the NNTP connection pool
func setupNNTPPool(ctx context.Context, cfg *config.Config, poolManager pool.Manager) error {
	if len(cfg.Providers) > 0 {
		providers := cfg.ToNNTPProviders()
		if err := poolManager.SetProviders(providers); err != nil {
			slog.ErrorContext(ctx, "failed to create initial NNTP pool", "err", err)
			return err
		}
		slog.InfoContext(ctx, "NNTP connection pool initialized", "provider_count", len(cfg.Providers))
	} else {
		slog.InfoContext(ctx, "Starting server without NNTP providers - configure via API to enable downloads")
	}
	return nil
}

// setupRCloneClient creates an RClone client if enabled
func setupRCloneClient(ctx context.Context, cfg *config.Config, configManager *config.Manager) rclonecli.RcloneRcClient {
	if cfg.RClone.RCEnabled != nil && *cfg.RClone.RCEnabled {
		httpClient := httpclient.NewDefault()
		rcloneClient := rclonecli.NewRcloneRcClient(configManager, httpClient)

		if cfg.RClone.RCUrl != "" {
			slog.InfoContext(ctx, "RClone RC client initialized for external server",
				"rc_url", cfg.RClone.RCUrl)
		} else {
			slog.InfoContext(ctx, "RClone RC client initialized for internal server",
				"rc_port", cfg.RClone.RCPort)
		}
		return rcloneClient
	}

	slog.InfoContext(ctx, "RClone RC notifications disabled")
	return nil
}

// createFiberApp creates and configures the Fiber application
func createFiberApp(ctx context.Context, cfg *config.Config) (*fiber.App, *bool) {
	app := fiber.New(fiber.Config{
		RequestMethods: append(
			fiber.DefaultMethods, "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK",
		),
		BodyLimit: 100 * 1024 * 1024, // 100MB limit for uploads (e.g. nzbdav DBs)
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			slog.ErrorContext(ctx, "Fiber error", "path", c.Path(), "method", c.Method(), "error", err)
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
func setupRepositories(ctx context.Context, db *database.DB) *repositorySet {
	dbConn := db.Connection()

	return &repositorySet{
		MainRepo:   database.NewRepository(dbConn),
		MediaRepo:  database.NewMediaRepository(dbConn),
		HealthRepo: database.NewHealthRepository(dbConn),
		UserRepo:   database.NewUserRepository(dbConn),
	}
}

// setupAuthService creates and initializes the authentication service
func setupAuthService(ctx context.Context, userRepo *database.UserRepository) *auth.Service {
	authConfig, err := auth.LoadConfigFromEnv()
	if err != nil {
		slog.WarnContext(ctx, "Failed to load auth configuration", "err", err)
		return nil
	}

	authService, err := auth.NewService(authConfig, userRepo)
	if err != nil {
		slog.WarnContext(ctx, "Failed to create authentication service", "err", err)
		return nil
	}

	// Setup OAuth providers
	if err := authService.SetupProviders(authConfig); err != nil {
		slog.WarnContext(ctx, "Failed to setup OAuth providers", "err", err)
		return nil
	}

	slog.InfoContext(ctx, "Authentication service initialized")
	return authService
}

// setupStreamHandler creates the HTTP stream handler for file streaming
func setupStreamHandler(
	nzbFilesystem *nzbfilesystem.NzbFilesystem,
	userRepo *database.UserRepository,
	streamTracker *api.StreamTracker,
) *api.StreamHandler {
	return api.NewStreamHandler(nzbFilesystem, userRepo, streamTracker)
}

// setupAPIServer creates and configures the API server
func setupAPIServer(
	app *fiber.App,
	repos *repositorySet,
	authService *auth.Service,
	configManager *config.Manager,
	metadataReader *metadata.MetadataReader,
	metadataService *metadata.MetadataService,
	nzbFilesystem *nzbfilesystem.NzbFilesystem,
	poolManager pool.Manager,
	importerService *importer.Service,
	arrsService *arrs.Service,
	mountService *rclone.MountService,
	progressBroadcaster *progress.ProgressBroadcaster,
	streamTracker *api.StreamTracker,
	segcacheMgr *segcache.Manager,
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
		metadataService,
		nzbFilesystem,
		poolManager,
		importerService,
		arrsService,
		mountService,
		progressBroadcaster,
		streamTracker,
		segcacheMgr,
	)

	apiServer.SetupRoutes(app)

	// Register RClone handlers
	rcloneHandlers := api.NewRCloneHandlers(mountService, configManager.GetConfigGetter())
	api.RegisterRCloneRoutes(app.Group("/api"), rcloneHandlers)

	// Add simple liveness endpoint for Docker health checks
	app.Get("/live", handleFiberHealth)

	return apiServer
}

// initializeSegmentCache creates and starts the segment cache manager if enabled
func initializeSegmentCache(ctx context.Context, cfg *config.Config) *segcache.Manager {
	if cfg.SegmentCache.Enabled == nil || !*cfg.SegmentCache.Enabled {
		slog.InfoContext(ctx, "Segment cache disabled")
		return nil
	}

	mgrCfg := segcache.ManagerConfig{
		Enabled:        true,
		CachePath:      cfg.SegmentCache.CachePath,
		MaxSizeBytes:   int64(cfg.SegmentCache.MaxSizeGB) * 1024 * 1024 * 1024,
		ExpiryDuration: time.Duration(cfg.SegmentCache.ExpiryHours) * time.Hour,
	}.WithDefaults()

	mgr, err := segcache.NewManager(mgrCfg, slog.Default().With("component", "segcache"))
	if err != nil {
		slog.WarnContext(ctx, "Failed to create segment cache manager, running without segment cache", "error", err)
		return nil
	}

	mgr.Start(ctx)
	slog.InfoContext(ctx, "Segment cache enabled",
		"cache_path", mgrCfg.CachePath,
		"max_size_bytes", mgrCfg.MaxSizeBytes,
		"expiry_duration", mgrCfg.ExpiryDuration)

	return mgr
}

// setupWebDAV creates and configures the WebDAV handler
func setupWebDAV(
	cfg *config.Config,
	fs *nzbfilesystem.NzbFilesystem,
	authService *auth.Service,
	userRepo *database.UserRepository,
	configManager *config.Manager,
	streamTracker *api.StreamTracker,
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
	}, fs, tokenService, webdavUserRepo, configManager.GetConfigGetter(), streamTracker)

	if err != nil {
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
	)

	healthWorker := health.NewHealthWorker(
		healthChecker,
		healthRepo,
		metadataService,
		arrsService,
		configManager.GetConfigGetter(),
	)

	// Create library sync worker (always create, but only start if enabled)
	librarySyncWorker := health.NewLibrarySyncWorker(
		metadataService,
		healthRepo,
		configManager.GetConfigGetter(),
		configManager,
		rcloneClient,
	)

	// Only start health system if enabled
	if cfg.Health.Enabled != nil && *cfg.Health.Enabled {
		// Start health worker with the main context
		if err := healthWorker.Start(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to start health worker", "error", err)
			return nil, nil, err
		}

		// Start library sync worker
		librarySyncWorker.StartLibrarySync(ctx)

		slog.InfoContext(ctx, "Health system started")
	} else {
		slog.InfoContext(ctx, "Health system disabled - no health monitoring or repairs will occur")
	}

	return healthWorker, librarySyncWorker, nil
}

// startMountService starts the RClone mount service if enabled
func startMountService(ctx context.Context, cfg *config.Config, mountService *rclone.MountService, logger *slog.Logger) error {
	if cfg.RClone.MountEnabled == nil || !*cfg.RClone.MountEnabled {
		slog.InfoContext(ctx, "RClone mount service is disabled in configuration")
		return nil
	}

	if err := mountService.Start(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to start mount service", "error", err)
		return err
	}

	slog.InfoContext(ctx, "RClone mount service started", "mount_point", cfg.MountPath)
	return nil
}

// createHTTPServer creates the HTTP server with routing
func createHTTPServer(app *fiber.App, webdavHandler *webdav.Handler, streamHandler *api.StreamHandler, port int, profilerEnabled bool) *http.Server {
	// Mount WebDAV handler directly (no Fiber adapter needed)
	webdavHTTPHandler := webdavHandler.GetHTTPHandler()

	// Mount stream handler directly (no Fiber adapter needed)
	streamHTTPHandler := streamHandler.GetHTTPHandler()

	// Convert Fiber app to HTTP handler for all other routes
	fiberHTTPHandler := adaptor.FiberApp(app)

	// Create a handler that routes between WebDAV, Stream, and Fiber
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Route profiler requests if enabled
		if profilerEnabled && strings.HasPrefix(path, "/debug/pprof") {
			http.DefaultServeMux.ServeHTTP(w, r)
			return
		}

		// Route stream requests directly to stream handler
		if strings.HasPrefix(path, "/api/files/stream") {
			streamHTTPHandler.ServeHTTP(w, r)
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
