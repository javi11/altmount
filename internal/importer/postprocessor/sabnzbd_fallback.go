package postprocessor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/sabnzbd"
)

// AttemptFallback tries to send a failed import to external SABnzbd
func (c *Coordinator) AttemptFallback(ctx context.Context, item *database.ImportQueueItem) error {
	cfg := c.configGetter()

	// Check if the NZB file still exists
	if _, err := os.Stat(item.NzbPath); err != nil {
		c.log.WarnContext(ctx, "SABnzbd fallback not attempted - NZB file not found",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", err)
		return err
	}

	c.log.InfoContext(ctx, "Attempting to send failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost)

	// Convert priority to SABnzbd format
	priority := convertPriorityToSABnzbd(item.Priority)

	// Create client and send
	client := sabnzbd.NewSABnzbdClient()
	nzoID, err := client.SendNZBFile(
		ctx,
		cfg.SABnzbd.FallbackHost,
		cfg.SABnzbd.FallbackAPIKey,
		item.NzbPath,
		item.Category,
		&priority,
	)
	if err != nil {
		return err
	}

	c.log.InfoContext(ctx, "Successfully sent failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost,
		"sabnzbd_nzo_id", nzoID)

	// Store Postie tracking metadata if Postie integration is enabled
	if cfg.Postie.Enabled != nil && *cfg.Postie.Enabled {
		originalReleaseName := extractReleaseName(item.NzbPath)
		c.log.InfoContext(ctx, "Postie integration enabled - storing tracking metadata",
			"queue_id", item.ID,
			"original_release_name", originalReleaseName)

		// Update queue item with Postie tracking metadata
		item.OriginalReleaseName = &originalReleaseName
		postiePending := "pending"
		item.PostieUploadStatus = &postiePending

		// Note: The actual database update happens in the calling code
		// (handleProcessingFailure in service.go) after this function returns
	}

	return nil
}

// extractReleaseName extracts the release name from the NZB file path
// This is used for matching with Postie-generated NZBs later
func extractReleaseName(nzbPath string) string {
	// Get the filename without extension
	fileName := filepath.Base(nzbPath)
	releaseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Clean up common patterns that might be added by downloaders
	// e.g., "[usenet4all.info]", " [nzbgeek]", etc.
	cleanPatterns := []string{
		"\\[.*?\\]",        // Anything in square brackets
		"\\(.*?\\)",        // Anything in parentheses
		"\\{.*?\\}",        // Anything in curly braces
		"_[*]?$",           // Trailing underscores
		" -[*]?$",          // Trailing dashes
	}

	for _, pattern := range cleanPatterns {
		re := strings.NewReplacer(
			pattern, "",
		)
		releaseName = strings.TrimSpace(re.Replace(releaseName))
	}

	return releaseName
}

// convertPriorityToSABnzbd converts AltMount queue priority to SABnzbd priority format
func convertPriorityToSABnzbd(priority database.QueuePriority) string {
	switch priority {
	case database.QueuePriorityHigh:
		return "2" // High
	case database.QueuePriorityLow:
		return "0" // Low
	default:
		return "1" // Normal
	}
}
