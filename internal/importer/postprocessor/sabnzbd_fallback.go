package postprocessor

import (
	"context"
	"os"

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

	return nil
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
