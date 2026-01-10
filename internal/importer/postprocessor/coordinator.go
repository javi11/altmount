// Package postprocessor handles all post-import processing steps including
// symlink creation, STRM file generation, VFS notifications, health check
// scheduling, and ARR notifications.
package postprocessor

import (
	"context"
	"log/slog"
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
	configGetter    config.ConfigGetter
	metadataService *metadata.MetadataService
	rcloneClient    rclonecli.RcloneRcClient
	healthRepo      *database.HealthRepository
	arrsService     *arrs.Service
	userRepo        *database.UserRepository
	database        *database.QueueRepository
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
	Database        *database.QueueRepository
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
		database:        cfg.Database,
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

	// 1. Notify VFS (blocking to ensure visibility)
	c.NotifyVFS(ctx, resultingPath, false)
	result.VFSNotified = true

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

	// 3. Create ID metadata links
	c.HandleIDMetadataLinks(ctx, item, resultingPath)

	// 4. Create STRM files if configured
	if err := c.CreateStrmFiles(ctx, item, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to create STRM files",
			"queue_id", item.ID,
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.StrmCreated = true
	}

	// 5. Schedule health check
	if err := c.ScheduleHealthCheck(ctx, resultingPath); err != nil {
		c.log.WarnContext(ctx, "Failed to schedule health check",
			"path", resultingPath,
			"error", err)
		result.Errors = append(result.Errors, err)
	} else {
		result.HealthScheduled = true
	}

	// 6. Notify ARR applications
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
func (c *Coordinator) HandleFailure(ctx context.Context, item *database.ImportQueueItem, processingErr error) (string, error) {
	cfg := c.configGetter()

	// Attempt SABnzbd fallback if configured
	if cfg.SABnzbd.FallbackHost != "" && cfg.SABnzbd.FallbackAPIKey != "" {
		nzoID, err := c.AttemptFallback(ctx, item)
		return nzoID, err
	}

	return "", errors.ErrFallbackNotConfigured
}

// StartMonitoring starts periodic monitoring of fallback items
func (c *Coordinator) StartMonitoring(ctx context.Context) {
	go c.monitorFallbacks(ctx)
}

func (c *Coordinator) monitorFallbacks(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds for quick feedback
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.MonitorFallbacks(ctx); err != nil {
				c.log.WarnContext(ctx, "Fallback monitoring failed", "error", err)
			}
		}
	}
}
