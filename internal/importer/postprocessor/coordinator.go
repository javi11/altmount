// Package postprocessor handles all post-import processing steps including
// symlink creation, STRM file generation, VFS notifications, health check
// scheduling, and ARR notifications.
package postprocessor

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/errors"
	fusecache "github.com/javi11/altmount/internal/fuse/cache"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// Coordinator orchestrates all post-import processing steps
type Coordinator struct {
	configGetter    config.ConfigGetter
	metadataService *metadata.MetadataService
	rcloneClient    rclonecli.RcloneRcClient
	healthRepo      *database.HealthRepository
	arrsService     *arrs.Service
	userRepo        *database.UserRepository
	fuseCache       fusecache.Cache // Optional FUSE metadata cache
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
	c.rcloneClient = client
}

// SetArrsService updates the ARRs service (called after initialization)
func (c *Coordinator) SetArrsService(service *arrs.Service) {
	c.arrsService = service
}

// SetFuseCache sets the FUSE metadata cache for invalidation
func (c *Coordinator) SetFuseCache(cache fusecache.Cache) {
	c.fuseCache = cache
}

// InvalidateFUSECache invalidates cache entries for a path and its parent directory.
// This is called after successful imports to ensure the FUSE mount sees new files.
func (c *Coordinator) InvalidateFUSECache(path string) {
	if c.fuseCache == nil {
		return
	}

	c.fuseCache.Invalidate(path)

	// Also invalidate parent directory so directory listings are refreshed
	parent := filepath.Dir(path)
	if parent != path && parent != "." && parent != "/" {
		c.fuseCache.Invalidate(parent)
	}
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
	result := &ProcessingResult{}

	// 1. Invalidate FUSE metadata cache (ensures visibility immediately)
	c.InvalidateFUSECache(resultingPath)

	// 2. Notify VFS (blocking to ensure visibility)
	c.NotifyVFS(ctx, resultingPath, false)
	result.VFSNotified = true

	// 3. Create symlinks if configured
	if err := c.CreateSymlinks(ctx, item, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to create symlinks",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.SymlinksCreated = true
	}

	// 4. Create ID metadata links
	c.HandleIDMetadataLinks(ctx, item, resultingPath)

	// 5. Create STRM files if configured
	if err := c.CreateStrmFiles(ctx, item, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to create STRM files",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.StrmCreated = true
	}

	// 6. Schedule health check
	if err := c.ScheduleHealthCheck(ctx, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to schedule health check",
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.HealthScheduled = true
	}

	// 7. Notify ARR applications
	if err := c.NotifyARR(ctx, item, resultingPath); err != nil {
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
func (c *Coordinator) HandleFailure(ctx context.Context, item *database.ImportQueueItem, processingErr error) error {
	cfg := c.configGetter()

	// Attempt SABnzbd fallback if configured
	if cfg.SABnzbd.FallbackHost != "" && cfg.SABnzbd.FallbackAPIKey != "" {
		return c.AttemptFallback(ctx, item)
	}

	return errors.ErrFallbackNotConfigured
}
