package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/javi11/altmount/internal/adapters/webdav"
	"github.com/javi11/altmount/internal/api"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/integration"
	"github.com/javi11/nntppool"
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
	config, err := LoadConfig(configFile)
	if err != nil {
		logger.Error("failed to load config", "err", err)
		return err
	}

	// Validate that we have at least one provider
	if len(config.Providers) == 0 {
		return fmt.Errorf("no NNTP providers configured in config file")
	}

	// Convert to nntppool providers
	providers := config.ToNNTPProviders()

	// Create NNTP connection pool
	pool, err := nntppool.NewConnectionPool(nntppool.Config{Providers: providers, Logger: logger})
	if err != nil {
		logger.Error("failed to create NNTP pool", "err", err)
		return err
	}
	defer pool.Quit()

	// Create NZB system with metadata + queue
	nsys, err := integration.NewNzbSystem(integration.NzbConfig{
		QueueDatabasePath:  config.Database.Path,
		MetadataRootPath:   config.Metadata.RootPath,
		MaxRangeSize:       config.Metadata.MaxRangeSize,
		StreamingChunkSize: config.Metadata.StreamingChunkSize,
		MountPath:          config.MountPath,
		NzbDir:             config.NZBDir,
		Password:           config.RClone.Password,
		Salt:               config.RClone.Salt,
		DownloadWorkers:    config.Workers.Download,
		ProcessorWorkers:   config.Workers.Processor,
	}, pool)
	if err != nil {
		logger.Error("failed to init NZB system", "err", err)
		return err
	}
	defer nsys.Close()

	// Create shared HTTP mux
	mux := http.NewServeMux()

	// Create API server if enabled
	if config.API.Enabled {
		// Create repositories for API access
		dbConn := nsys.Database().Connection()
		mainRepo := database.NewRepository(dbConn)
		healthRepo := database.NewHealthRepository(dbConn)
		
		// Create API server configuration
		apiConfig := &api.Config{
			Enabled:  config.API.Enabled,
			Prefix:   config.API.Prefix,
			Username: config.API.Username,
			Password: config.API.Password,
		}
		
		// Create API server with shared mux
		api.NewServer(apiConfig, mainRepo, healthRepo, mux)
		logger.Info("API server enabled", "prefix", config.API.Prefix)
	}
	
	// Create WebDAV server with shared mux
	server, err := webdav.NewServer(&webdav.Config{
		Port:  config.WebDAV.Port,
		User:  config.WebDAV.User,
		Pass:  config.WebDAV.Password,
		Debug: config.WebDAV.Debug || config.Debug,
	}, nsys.FileSystem(), mux)
	if err != nil {
		logger.Error("failed to start webdav", "err", err)
		return err
	}

	logger.Info("Starting AltMount server",
		"webdav_port", config.WebDAV.Port,
		"mount_path", config.MountPath,
		"providers", len(config.Providers),
		"download_workers", config.Workers.Download,
		"processor_workers", config.Workers.Processor)

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
