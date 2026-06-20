package postprocessor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/httpclient"
	"github.com/javi11/altmount/internal/importer/utils/nzbtrim"
	"github.com/javi11/altmount/internal/nzbfile"
	"github.com/javi11/altmount/internal/sabnzbd"
)

// AttemptFallback tries to send a failed import to external SABnzbd
func (c *Coordinator) AttemptFallback(ctx context.Context, item *database.ImportQueueItem) error {
	cfg := c.configGetter()

	var nzbXML []byte
	var nzbFilename string

	// Try the NZB file on disk first.
	if _, err := os.Stat(item.NzbPath); err == nil {
		rc, err := nzbfile.Open(item.NzbPath)
		if err != nil {
			return fmt.Errorf("failed to open NZB: %w", err)
		}
		nzbXML, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("failed to read NZB: %w", err)
		}
		nzbFilename = nzbfile.PlainFilename(item.NzbPath)
	} else {
		// Fall back to regenerating from the v3 NzbStore.
		if c.metadataService == nil {
			c.log.WarnContext(ctx, "SABnzbd fallback not attempted - NZB missing and no metadata service",
				"queue_id", item.ID,
				"file", item.NzbPath)
			return fmt.Errorf("nzb file not found and metadata service unavailable for item %d", item.ID)
		}
		storePath := nzbtrim.TrimNzbExtension(item.NzbPath) + ".nzbz"
		regenerated, err := c.metadataService.Store().RegenerateNZB(storePath)
		if err != nil {
			c.log.WarnContext(ctx, "SABnzbd fallback: failed to regenerate NZB from store",
				"queue_id", item.ID,
				"store_path", storePath,
				"error", err)
			return fmt.Errorf("nzb not found on disk and store regeneration failed: %w", err)
		}
		if regenerated == nil {
			c.log.WarnContext(ctx, "SABnzbd fallback not attempted - NZB and store both missing",
				"queue_id", item.ID,
				"file", item.NzbPath)
			return fmt.Errorf("nzb file and store both not found for item %d", item.ID)
		}
		nzbXML = regenerated
		nzbFilename = filepath.Base(nzbtrim.TrimNzbExtension(item.NzbPath)) + ".nzb"
	}

	c.log.InfoContext(ctx, "Attempting to send failed import to external SABnzbd",
		"queue_id", item.ID,
		"file", item.NzbPath,
		"fallback_host", cfg.SABnzbd.FallbackHost)

	// Convert priority to SABnzbd format.
	priority := convertPriorityToSABnzbd(item.Priority)

	// Create client and send (proxy-aware per current network config).
	client := sabnzbd.NewSABnzbdClient(httpclient.NewForExternal(cfg.Network, httpclient.LongTimeout))
	nzoID, err := client.SendNZBContent(
		ctx,
		cfg.SABnzbd.FallbackHost,
		cfg.SABnzbd.FallbackAPIKey,
		nzbXML,
		nzbFilename,
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
