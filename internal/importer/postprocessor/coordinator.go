// Package postprocessor handles all post-import processing steps including
// symlink creation, STRM file generation, VFS notifications, health check
// scheduling, and ARR notifications.
package postprocessor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/errors"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// Coordinator orchestrates all post-import processing steps
type Coordinator struct {
	mu              sync.RWMutex
	configGetter    config.ConfigGetter
	metadataService *metadata.MetadataService
	rcloneClient    rclonecli.RcloneRcClient
	healthRepo      *database.HealthRepository
	arrsService     *arrs.Service
	userRepo        *database.UserRepository
	log             *slog.Logger
}

// Config holds configuration for the Coordinator
type Config struct {
	ConfigGetter    config.ConfigGetter
	MetadataService *metadata.MetadataService
	RcloneClient    rclonecli.RcloneRcClient
	HealthRepo      *database.HealthRepository
	ArrsService     *arrs.Service
	UserRepo        *database.UserRepository
}

// NewCoordinator creates a new post-processor coordinator
func NewCoordinator(cfg Config) *Coordinator {
	return &Coordinator{
		configGetter:    cfg.ConfigGetter,
		metadataService: cfg.MetadataService,
		rcloneClient:    cfg.RcloneClient,
		healthRepo:      cfg.HealthRepo,
		arrsService:     cfg.ArrsService,
		userRepo:        cfg.UserRepo,
		log:             slog.Default().With("component", "postprocessor"),
	}
}

// SetRcloneClient updates the rclone client (called when config changes)
func (c *Coordinator) SetRcloneClient(client rclonecli.RcloneRcClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rcloneClient = client
}

// SetArrsService updates the ARRs service (called after initialization)
func (c *Coordinator) SetArrsService(service *arrs.Service) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.arrsService = service
}

// ProcessingResult holds the result of post-processing operations
type ProcessingResult struct {
	SymlinksCreated bool
	StrmCreated     bool
	VFSNotified     bool
	HealthScheduled bool
	ARRNotified     bool
	Errors          []error
}

// HandleSuccess performs all post-processing for successful imports
func (c *Coordinator) HandleSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) (*ProcessingResult, error) {
	c.mu.RLock()
	rcloneClient := c.rcloneClient
	arrsService := c.arrsService
	c.mu.RUnlock()

	result := &ProcessingResult{}

	// 1. Notify VFS (blocking to ensure visibility)
	c.notifyVFSWith(ctx, rcloneClient, resultingPath, false)
	result.VFSNotified = true

	// Small delay to allow FUSE mount propagation through kernel and into other containers
	// This helps prevent race conditions where Sonarr tries to probe the file before it's visible.
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-time.After(1 * time.Second):
		// Continue
	}

	// 2. Create symlinks if configured
	if err := c.CreateSymlinks(ctx, item, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to create symlinks",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.SymlinksCreated = true
	}

	// 3. Create STRM files if configured
	if err := c.CreateStrmFiles(ctx, item, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to create STRM files",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.StrmCreated = true
	}

	// 4. Schedule health check
	if err := c.ScheduleHealthCheck(ctx, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to schedule health check",
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.HealthScheduled = true
	}

	// 5. Notify ARR applications
	if shouldSkipARRNotification(item) {
		c.log.DebugContext(ctx, "ARR notification skipped (requested by caller)",
			"queue_id", item.ID,
			"path", resultingPath)
	} else if err := c.notifyARRWith(ctx, arrsService, item, resultingPath); err != nil {
		c.log.DebugContext(ctx, "ARR notification not sent",
			"path", resultingPath,
			"error", err)
		// Don't add to errors - ARR notification is optional
	} else {
		result.ARRNotified = true
	}

	return result, nil
}

// HandleFailure performs cleanup and fallback for failed imports
func (c *Coordinator) HandleFailure(ctx context.Context, item *database.ImportQueueItem, _ error) error {
	cfg := c.configGetter()

	// Attempt SABnzbd fallback if configured
	if cfg.SABnzbd.FallbackHost != "" && cfg.SABnzbd.FallbackAPIKey != "" {
		return c.AttemptFallback(ctx, item)
	}

	return errors.ErrFallbackNotConfigured
}

// shouldSkipARRNotification returns true when the caller explicitly requested
// that ARR notifications be suppressed.
func shouldSkipARRNotification(item *database.ImportQueueItem) bool {
	return item.SkipArrNotification
}
