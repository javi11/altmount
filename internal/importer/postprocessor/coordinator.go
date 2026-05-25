// Package postprocessor handles all post-import processing steps including
// symlink creation, STRM file generation, VFS notifications, health check
// scheduling, and ARR notifications.
package postprocessor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

	// Wait until the imported path is actually reachable through the backing
	// mount before creating symlinks and notifying ARRs. A fixed sleep used to
	// stand in for FUSE/rclone propagation, but it was simultaneously too short
	// under load (Sonarr/Radarr would import a symlink whose target was not yet
	// visible — issue #612) and wastefully long on a fast mount. Polling returns
	// as soon as the path appears and only blocks longer when propagation is
	// genuinely slow.
	if visible, err := c.waitForPathVisible(ctx, resultingPath); err != nil {
		return result, err
	} else if !visible {
		c.log.WarnContext(ctx, "Imported path not visible on mount before notifying downstream; proceeding best-effort",
			"queue_id", item.ID,
			"path", resultingPath)
	}

	// 2 & 3. Create symlinks and STRM files if configured
	if shouldSkipPostImportLinks(item) {
		c.log.DebugContext(ctx, "Skipping symlink/STRM creation (post-import links disabled)",
			"queue_id", item.ID,
			"path", resultingPath)
	} else {
		if err := c.CreateSymlinks(ctx, item, resultingPath); err != nil {
			c.log.WarnContext(ctx, "Failed to create symlinks",
				"queue_id", item.ID,
				"path", resultingPath,
				"error", err)
			result.Errors = append(result.Errors, err)
		} else {
			result.SymlinksCreated = true
		}

		if err := c.CreateStrmFiles(ctx, item, resultingPath); err != nil {
			c.log.WarnContext(ctx, "Failed to create STRM files",
				"queue_id", item.ID,
				"path", resultingPath,
				"error", err)
			result.Errors = append(result.Errors, err)
		} else {
			result.StrmCreated = true
		}
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

// shouldSkipPostImportLinks returns true when the caller explicitly requested
// that post-import link creation (symlinks, STRM files) be suppressed.
func shouldSkipPostImportLinks(item *database.ImportQueueItem) bool {
	return item != nil && item.SkipPostImportLinks
}

const (
	// mountVisibilityTimeout bounds how long HandleSuccess waits for an imported
	// path to become visible on the backing mount before proceeding anyway.
	mountVisibilityTimeout = 10 * time.Second
	// mountVisibilityPoll is the interval between visibility probes.
	mountVisibilityPoll = 100 * time.Millisecond
)

// waitForPathVisible polls the backing mount until resultingPath is reachable,
// the context is cancelled, or mountVisibilityTimeout elapses. It returns
// (true, nil) once the path is visible and (false, nil) if the timeout is hit
// (callers proceed best-effort, matching the previous fixed-sleep behaviour).
// A non-nil error is returned only when the context is cancelled.
//
// When no local mount is configured (e.g. WebDAV-only deployments) there is
// nothing to probe, so it returns immediately.
func (c *Coordinator) waitForPathVisible(ctx context.Context, resultingPath string) (bool, error) {
	cfg := c.configGetter()
	if cfg.MountPath == "" {
		return true, nil
	}
	actualPath := filepath.Join(cfg.MountPath, strings.TrimPrefix(resultingPath, "/"))

	deadline := time.NewTimer(mountVisibilityTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(mountVisibilityPoll)
	defer ticker.Stop()

	for {
		if _, err := os.Stat(actualPath); err == nil {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}
