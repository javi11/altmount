package postprocessor

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/database"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"google.golang.org/protobuf/proto"
)

// HandleIDMetadataLinks creates ID-based metadata links for nzbdav compatibility
func (c *Coordinator) HandleIDMetadataLinks(ctx context.Context, item *database.ImportQueueItem, resultingPath string) {
	// 1. Check if the queue item itself has a release-level ID in its metadata
	if item.Metadata != nil && *item.Metadata != "" {
		var meta struct {
			NzbdavID string `json:"nzbdav_id"`
		}
		if err := json.Unmarshal([]byte(*item.Metadata), &meta); err == nil && meta.NzbdavID != "" {
			if err := c.createIDMetadataLink(meta.NzbdavID, resultingPath); err != nil {
				c.log.Warn("Failed to create release ID metadata link", "id", meta.NzbdavID, "error", err)
			}
		}
	}

	// 2. Check individual files for IDs
	cfg := c.configGetter()
	metadataPath := filepath.Join(cfg.Metadata.RootPath, strings.TrimPrefix(resultingPath, "/"))

	_ = filepath.WalkDir(metadataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}

		// Read the metadata file to find the ID
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Parse the protobuf metadata to get the ID
		meta := &metapb.FileMetadata{}
		if err := proto.Unmarshal(data, meta); err != nil {
			return nil
		}

		// Check sidecar ID file if not in proto (compatibility mode)
		if meta.NzbdavId == "" {
			if idData, err := os.ReadFile(path + ".id"); err == nil {
				meta.NzbdavId = string(idData)
			}
		}

		if meta.NzbdavId != "" {
			// Calculate the virtual path from the metadata file path
			relPath, err := filepath.Rel(cfg.Metadata.RootPath, path)
			if err != nil {
				return nil
			}
			// Remove .meta extension
			virtualPath := strings.TrimSuffix(relPath, ".meta")

			if err := c.createIDMetadataLink(meta.NzbdavId, virtualPath); err != nil {
				c.log.Warn("Failed to create file ID metadata link", "id", meta.NzbdavId, "error", err)
			}
		}

		return nil
	})
}

// createIDMetadataLink creates a symlink from an ID-based sharded path to the metadata file
func (c *Coordinator) createIDMetadataLink(nzbdavID, resultingPath string) error {
	cfg := c.configGetter()
	metadataRoot := cfg.Metadata.RootPath

	// Calculate sharded path
	// 04db0bde-7ad0-46a3-a2f4-9ef8efd0d7d7 -> .ids/0/4/d/b/0/04db0bde-7ad0-46a3-a2f4-9ef8efd0d7d7.meta
	id := strings.ToLower(nzbdavID)
	if len(id) < 5 {
		return nil // Invalid ID for sharding
	}

	shardPath := filepath.Join(".ids", string(id[0]), string(id[1]), string(id[2]), string(id[3]), string(id[4]))
	fullShardDir := filepath.Join(metadataRoot, shardPath)

	if err := os.MkdirAll(fullShardDir, 0755); err != nil {
		return err
	}

	targetMetaPath := c.metadataService.GetMetadataFilePath(resultingPath)
	linkPath := filepath.Join(fullShardDir, id+".meta")

	// Remove if exists
	os.Remove(linkPath)

	// Create relative symlink if possible
	relTarget, err := filepath.Rel(fullShardDir, targetMetaPath)
	if err != nil {
		return os.Symlink(targetMetaPath, linkPath)
	}

	return os.Symlink(relTarget, linkPath)
}
