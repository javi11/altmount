//go:build fuse

package fusefs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pool"
)

// FuseService manages the lifecycle of the FUSE mount
type FuseService struct {
	configManager *config.Manager
	db            *database.DB
	poolManager   pool.Manager
	server        *fuse.Server
	mountpoint    string
	mutex         sync.Mutex
	running       bool
}

// NewFuseService creates a new FuseService
func NewFuseService(configManager *config.Manager, db *database.DB, poolManager pool.Manager) *FuseService {
	return &FuseService{
		configManager: configManager,
		db:            db,
		poolManager:   poolManager,
	}
}



// Stop stops the FUSE mount
func (s *FuseService) Stop(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	slog.Info("Unmounting FUSE filesystem...", "mountpoint", s.mountpoint)
	if err := s.server.Unmount(); err != nil {
		return fmt.Errorf("failed to unmount: %w", err)
	}
	
	// Wait logic is handled in the Start goroutine
	return nil
}

// Restart restarts the FUSE service (e.g. on config change)
func (s *FuseService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		slog.Error("Failed to stop FUSE service during restart", "error", err)
		// Continue trying to start
	}
	// Give it a moment to unmount fully
	time.Sleep(1 * time.Second)
	return s.Start(ctx)
}

// RegisterConfigHandlers registers callbacks for configuration changes
func (s *FuseService) RegisterConfigHandlers(configManager *config.Manager) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Panic in FUSE config handler", "error", r)
			}
		}()

		// Check if FUSE config has changed
		if oldConfig.Fuse.Enabled != newConfig.Fuse.Enabled ||
			oldConfig.Fuse.MountPoint != newConfig.Fuse.MountPoint ||
			oldConfig.Fuse.Readahead != newConfig.Fuse.Readahead ||
			oldConfig.Fuse.UID != newConfig.Fuse.UID ||
			oldConfig.Fuse.GID != newConfig.Fuse.GID {
			
			slog.Info("FUSE configuration changed, restarting service...")
			if err := s.Restart(context.Background()); err != nil {
				slog.Error("Failed to restart FUSE service", "error", err)
			}
		}
	})
}

// Start starts the FUSE mount if enabled in configuration
func (s *FuseService) Start(ctx context.Context) error {
	slog.Debug("Starting FUSE service...")
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		slog.Debug("FUSE service already running")
		return nil
	}

	cfg := s.configManager.GetConfig()
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if !cfg.Fuse.Enabled {
		slog.Info("FUSE mount is disabled")
		return nil
	}

	if cfg.Fuse.MountPoint == "" {
		return fmt.Errorf("FUSE mount enabled but no mount point specified")
	}

	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if s.poolManager == nil {
		return fmt.Errorf("pool manager is nil")
	}

	// Ensure mountpoint exists
	slog.Debug("Creating mountpoint", "path", cfg.Fuse.MountPoint)
	if err := os.MkdirAll(cfg.Fuse.MountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mountpoint %s: %w", cfg.Fuse.MountPoint, err)
	}

	readaheadBytes, err := parseReadAhead(cfg.Fuse.Readahead)
	if err != nil {
		slog.Warn("Invalid readahead value, using default 128K", "value", cfg.Fuse.Readahead, "error", err)
		readaheadBytes = 128 * 1024
	}

	// Initialize filesystem
	slog.Debug("Initializing FUSE filesystem...")
	fsInstance, err := NewNzbFuseFs(cfg, readaheadBytes, uint32(cfg.Fuse.UID), uint32(cfg.Fuse.GID), s.db, s.poolManager)
	if err != nil {
		return fmt.Errorf("failed to initialize FUSE filesystem: %w", err)
	}

	// Mount
	slog.Debug("Mounting FUSE filesystem...")
	server, err := fsInstance.Mount(cfg.Fuse.MountPoint)
	if err != nil {
		return fmt.Errorf("failed to mount FUSE filesystem: %w", err)
	}

	s.server = server
	s.mountpoint = cfg.Fuse.MountPoint
	s.running = true

	// Run server in background
	go func() {
		s.server.Wait()
		s.mutex.Lock()
		s.running = false
		s.server = nil
		s.mutex.Unlock()
		slog.Info("FUSE filesystem unmounted")
	}()

	slog.Info("FUSE service started successfully")
	return nil
}

// parseReadAhead converts a human-readable size string (e.g., "4M", "128K") to bytes
func parseReadAhead(sizeStr string) (int, error) {
	if sizeStr == "" {
		return 128 * 1024, nil // Default 128K
	}

	// Default to bytes if no unit
	value := 0
	unit := ""
	_, err := fmt.Sscanf(sizeStr, "%d%s", &value, &unit)
	if err != nil {
		// Try parsing without unit (plain number)
		_, err := fmt.Sscanf(sizeStr, "%d", &value)
		if err != nil {
			return 0, fmt.Errorf("invalid size format: %s", sizeStr)
		}
		return value, nil
	}

	switch unit {
	case "K", "k":
		return value * 1024, nil
	case "M", "m":
		return value * 1024 * 1024, nil
	case "G", "g":
		return value * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unknown size unit: %s", unit)
	}
}
