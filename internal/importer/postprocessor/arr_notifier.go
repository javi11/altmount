package postprocessor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// NotifyARR notifies ARR applications about imported content
func (c *Coordinator) NotifyARR(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	c.mu.RLock()
	arrsService := c.arrsService
	c.mu.RUnlock()

	return c.notifyARRWith(ctx, arrsService, item, resultingPath)
}

// notifyARRWith notifies ARR using the provided service (avoids re-locking)
func (c *Coordinator) notifyARRWith(ctx context.Context, arrsService *arrs.Service, item *database.ImportQueueItem, resultingPath string) error {
	if arrsService == nil {
		return nil
	}

	// When a forced target path is set, scan that path directly (no category required).
	if item.TargetPath != nil && *item.TargetPath != "" {
		if err := arrsService.TriggerScanForFile(ctx, *item.TargetPath); err != nil {
			c.log.WarnContext(ctx, "Failed to trigger ARR scan for target path",
				"path", *item.TargetPath, "error", err)
		}
		return nil
	}

	if item.Category == nil || *item.Category == "" {
		return nil
	}

	cfg := c.configGetter()

	// Build the path for ARR to scan
	var basePath string
	if cfg.Import.ImportStrategy != config.ImportStrategyNone &&
		cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		basePath = *cfg.Import.ImportDir
	} else {
		basePath = cfg.MountPath
	}

	// 1. Get the internal relative path (relative to FUSE mount)
	relPath := strings.TrimPrefix(resultingPath, "/")

	// 2. Strip any existing /complete or /category prefix from the internal path to start clean
	category := ""
	if item.Category != nil {
		category = strings.Trim(*item.Category, "/")
	}

	if cfg.SABnzbd.CompleteDir != "" {
		completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")
		if after, ok := strings.CutPrefix(relPath, completeDir+"/"); ok {
			relPath = after
		} else if relPath == completeDir {
			relPath = ""
		}
	}
	if category != "" {
		if after, ok := strings.CutPrefix(relPath, category+"/"); ok {
			relPath = after
		} else if relPath == category {
			relPath = ""
		}
	}

	// 3. Build the clean path using the determined base
	pathParts := []string{basePath}
	if cfg.SABnzbd.CompleteDir != "" {
		pathParts = append(pathParts, strings.Trim(cfg.SABnzbd.CompleteDir, "/"))
	}
	if category != "" {
		pathParts = append(pathParts, category)
	}
	pathParts = append(pathParts, relPath)

	pathForARR := filepath.Join(pathParts...)
	pathForARR = filepath.ToSlash(filepath.Clean(pathForARR))

	if err := arrsService.TriggerScanForFile(ctx, pathForARR); err != nil {
		// Fallback: broadcast to all instances of the type
		c.log.DebugContext(ctx, "Could not find specific ARR instance for file, broadcasting scan",
			"path", pathForARR, "error", err)

		return c.broadcastToARRType(ctx, arrsService, item)
	}

	return nil
}

// broadcastToARRType broadcasts scan to all instances of the determined ARR type
func (c *Coordinator) broadcastToARRType(ctx context.Context, arrsService *arrs.Service, item *database.ImportQueueItem) error {
	if item.Category == nil {
		return fmt.Errorf("cannot determine ARR type: category is nil")
	}
	categoryName := *item.Category
	category := strings.ToLower(categoryName)
	arrType := ""

	cfg := c.configGetter()

	// Try to find an explicit mapping in SABnzbd categories
	for _, cat := range cfg.SABnzbd.Categories {
		if strings.EqualFold(cat.Name, categoryName) && cat.Type != "" {
			arrType = strings.ToLower(cat.Type)
			break
		}
	}

	// Fallback to heuristic if no explicit type is mapped
	if arrType == "" {
		if category == "tv" || strings.Contains(category, "tv") || strings.Contains(category, "show") || category == "sonarr" {
			arrType = "sonarr"
		} else if category == "movies" || strings.Contains(category, "movie") || category == "radarr" {
			arrType = "radarr"
		}
	}

	if arrType != "" {
		arrsService.TriggerDownloadScan(ctx, arrType)
		return nil
	}

	return fmt.Errorf("could not determine ARR type for category: %s", categoryName)
}
