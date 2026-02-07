package postprocessor

import (
	"context"
	"fmt"
	"strings"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pathutil"
)

// NotifyARR notifies ARR applications about imported content
func (c *Coordinator) NotifyARR(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error {
	if c.arrsService == nil || item.Category == nil {
		return nil
	}

	cfg := c.configGetter()

	// Try to trigger scan on the specific instance that manages this file
	fullMountPath := pathutil.JoinAbsPath(cfg.MountPath, resultingPath)

	if err := c.arrsService.TriggerScanForFile(ctx, fullMountPath); err != nil {
		// Fallback: broadcast to all instances of the type
		c.log.DebugContext(ctx, "Could not find specific ARR instance for file, broadcasting scan",
			"path", fullMountPath, "error", err)

		return c.broadcastToARRType(ctx, item)
	}

	return nil
}

// broadcastToARRType broadcasts scan to all instances of the determined ARR type
func (c *Coordinator) broadcastToARRType(ctx context.Context, item *database.ImportQueueItem) error {
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
		c.arrsService.TriggerDownloadScan(ctx, arrType)
		return nil
	}

	return fmt.Errorf("could not determine ARR type for category: %s", categoryName)
}
