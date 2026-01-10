package postprocessor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/sabnzbd"
)

// AttemptFallback tries to send a failed import to external SABnzbd
// Returns the NZO ID assigned by SABnzbd if successful
func (c *Coordinator) AttemptFallback(ctx context.Context, item *database.ImportQueueItem) (string, error) {
	cfg := c.configGetter()

	// Check if the NZB file still exists
	if _, err := os.Stat(item.NzbPath); err != nil {
		c.log.WarnContext(ctx, "SABnzbd fallback not attempted - NZB file not found",
			"queue_id", item.ID,
			"file", item.NzbPath,
			"error", err)
		return "", err
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
		return "", err
	}

	c.log.InfoContext(ctx, "Successfully sent failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost,
		"sabnzbd_nzo_id", nzoID)

	return nzoID, nil
}

// MonitorFallbacks checks the status of fallback items in external SABnzbd and updates AltMount queue accordingly
// Only checks items that have a fallback_nzo_id but no error_message (not yet marked as failed)
func (c *Coordinator) MonitorFallbacks(ctx context.Context) error {
	cfg := c.configGetter()

	// Check if fallback is configured
	if cfg.SABnzbd.FallbackHost == "" || cfg.SABnzbd.FallbackAPIKey == "" {
		return nil // No fallback configured, nothing to monitor
	}

	// Get all items with fallback status that have NZO ID but no error (not yet checked as failed)
	fallbackItems, err := c.database.ListFallbackItemsToMonitor(ctx, 1000)
	if err != nil {
		return fmt.Errorf("failed to get fallback items: %w", err)
	}

	if len(fallbackItems) == 0 {
		return nil // No fallback items to monitor
	}

	// Build a set of NZO IDs we need to check
	nzoIdsToCheck := make(map[string]*database.ImportQueueItem)
	for i := range fallbackItems {
		item := &fallbackItems[i]
		if item.FallbackNzoId != nil && *item.FallbackNzoId != "" {
			nzoIdsToCheck[*item.FallbackNzoId] = item
		}
	}

	if len(nzoIdsToCheck) == 0 {
		return nil
	}

	client := sabnzbd.NewSABnzbdClient()

	// First check the queue for items still downloading
	queueSlots, err := client.GetQueue(ctx, cfg.SABnzbd.FallbackHost, cfg.SABnzbd.FallbackAPIKey)
	if err != nil {
		c.log.WarnContext(ctx, "Failed to get SABnzbd queue for fallback monitoring", "error", err)
		// Continue to check history even if queue fails
	} else {
		// Mark items found in queue as "still downloading" - no action needed
		for _, slot := range queueSlots {
			delete(nzoIdsToCheck, slot.NzoID)
		}
	}

	// If all items are in queue (downloading), nothing more to check
	if len(nzoIdsToCheck) == 0 {
		return nil
	}

	// Get SABnzbd history for items not in queue
	historySlots, err := client.GetHistory(ctx, cfg.SABnzbd.FallbackHost, cfg.SABnzbd.FallbackAPIKey)
	if err != nil {
		c.log.WarnContext(ctx, "Failed to get SABnzbd history for fallback monitoring", "error", err)
		return err
	}

	// Build history map for quick lookup
	historyMap := make(map[string]sabnzbd.SABnzbdHistorySlot)
	for _, slot := range historySlots {
		historyMap[slot.NzoID] = slot
	}

	// Check each remaining item (not in queue)
	for nzoID, item := range nzoIdsToCheck {
		slot, existsInHistory := historyMap[nzoID]

		if !existsInHistory {
			// Item not in queue AND not in history - SABnzbd may have purged it
			// Clear the fallback_nzo_id so we stop monitoring this item
			c.log.InfoContext(ctx, "Fallback item no longer exists in SABnzbd, clearing NZO ID",
				"queue_id", item.ID,
				"nzo_id", nzoID)
			if err := c.database.UpdateQueueItemFallbackNzoId(ctx, item.ID, nil); err != nil {
				c.log.ErrorContext(ctx, "Failed to clear fallback NZO ID", "queue_id", item.ID, "error", err)
			}
			continue
		}

		status := strings.ToLower(slot.Status)

		if status == "completed" {
			// Successfully completed in SABnzbd - clear fallback_nzo_id to stop monitoring
			c.log.InfoContext(ctx, "Fallback item completed in SABnzbd, clearing NZO ID",
				"queue_id", item.ID,
				"nzo_id", nzoID)
			if err := c.database.UpdateQueueItemFallbackNzoId(ctx, item.ID, nil); err != nil {
				c.log.ErrorContext(ctx, "Failed to clear fallback NZO ID", "queue_id", item.ID, "error", err)
			}
		} else if status == "failed" {
			// Failed in SABnzbd - set error message
			errorMsg := "Failed in external SABnzbd"
			if slot.FailMessage != nil && *slot.FailMessage != "" {
				errorMsg = fmt.Sprintf("SABnzbd: %s", *slot.FailMessage)
			}

			if err := c.database.UpdateQueueItemErrorMessage(ctx, item.ID, &errorMsg); err != nil {
				c.log.ErrorContext(ctx, "Failed to update fallback item error message",
					"queue_id", item.ID,
					"nzo_id", nzoID,
					"error", err)
			} else {
				c.log.InfoContext(ctx, "Marked fallback item as failed based on SABnzbd status",
					"queue_id", item.ID,
					"nzo_id", nzoID,
					"fail_message", errorMsg)
			}
		}
		// Other statuses (extracting, repairing, etc) - keep monitoring
	}

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
