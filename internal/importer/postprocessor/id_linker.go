package postprocessor

import (
	"context"
	"encoding/json"

	"github.com/javi11/altmount/internal/database"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// HandleIDMetadataLinks creates ID-based metadata links for nzbdav compatibility
func (c *Coordinator) HandleIDMetadataLinks(ctx context.Context, item *database.ImportQueueItem, resultingPath string) {
	// 1. Check if the queue item itself has a release-level ID in its metadata
	if item.Metadata != nil && *item.Metadata != "" {
		var meta struct {
			NzbdavID string `json:"nzbdav_id"`
		}
		if err := json.Unmarshal([]byte(*item.Metadata), &meta); err == nil && meta.NzbdavID != "" {
			if err := c.metadataService.UpdateIDSymlink(meta.NzbdavID, resultingPath); err != nil {
				c.log.Warn("Failed to create release ID metadata link", "id", meta.NzbdavID, "error", err)
			}
		}
	}

	// 2. Check individual files for IDs using MetadataService walker
	_ = c.metadataService.WalkDirectoryFiles(resultingPath, func(fileVirtualPath string, meta *metapb.FileMetadata) error {
		if meta.NzbdavId != "" {
			if err := c.metadataService.UpdateIDSymlink(meta.NzbdavId, fileVirtualPath); err != nil {
				c.log.Warn("Failed to create file ID metadata link", "id", meta.NzbdavId, "error", err)
			}
		}
		return nil
	})
}
