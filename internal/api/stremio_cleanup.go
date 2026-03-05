package api

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
)

// StremioCleanupService periodically removes expired Stremio-originated queue items
// along with their associated .meta files and temp NZB files.
type StremioCleanupService struct {
	queueRepo       *database.Repository
	metadataService *metadata.MetadataService
	configGetter    config.ConfigGetter
}

// NewStremioCleanupService creates a new StremioCleanupService.
func NewStremioCleanupService(
	queueRepo *database.Repository,
	metadataService *metadata.MetadataService,
	configGetter config.ConfigGetter,
) *StremioCleanupService {
	return &StremioCleanupService{
		queueRepo:       queueRepo,
		metadataService: metadataService,
		configGetter:    configGetter,
	}
}

// StartCleanup launches a background goroutine that runs cleanup every hour.
// The goroutine stops when ctx is cancelled.
func (s *StremioCleanupService) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanupExpired(ctx)
			}
		}
	}()
}

// tempUploadDir is the directory where Stremio-originated NZB files are stored.
var tempUploadDir = filepath.Join(os.TempDir(), "altmount-uploads")

func (s *StremioCleanupService) cleanupExpired(ctx context.Context) {
	cfg := s.configGetter()
	ttlHours := cfg.Stremio.NzbTTLHours
	if ttlHours <= 0 {
		return
	}

	items, err := s.queueRepo.GetExpiredStremioQueueItems(ctx, ttlHours, tempUploadDir)
	if err != nil {
		slog.ErrorContext(ctx, "StremioCleanup: failed to query expired items", "error", err)
		return
	}

	for _, item := range items {
		s.deleteItem(ctx, item)
	}

	if len(items) > 0 {
		slog.InfoContext(ctx, "StremioCleanup: cleaned up expired items", "count", len(items))
	}
}

func (s *StremioCleanupService) deleteItem(ctx context.Context, item *database.ImportQueueItem) {
	if item.StoragePath != nil && *item.StoragePath != "" {
		storagePath := *item.StoragePath
		if s.metadataService.DirectoryExists(storagePath) {
			// Directory: delete each .meta file inside
			files, err := s.metadataService.ListDirectory(storagePath)
			if err != nil {
				slog.ErrorContext(ctx, "StremioCleanup: failed to list directory", "path", storagePath, "error", err)
			} else {
				for _, filename := range files {
					virtualPath := filepath.Join(storagePath, filename)
					if err := s.metadataService.DeleteFileMetadataWithSourceNzb(ctx, virtualPath, false); err != nil {
						slog.ErrorContext(ctx, "StremioCleanup: failed to delete meta", "path", virtualPath, "error", err)
					}
				}
			}
			// Delete the temp NZB manually (source NZB not tracked per-file in directory case)
			if err := os.Remove(item.NzbPath); err != nil && !os.IsNotExist(err) {
				slog.ErrorContext(ctx, "StremioCleanup: failed to delete temp NZB", "path", item.NzbPath, "error", err)
			}
		} else {
			// Single file: delete .meta + source NZB together
			if err := s.metadataService.DeleteFileMetadataWithSourceNzb(ctx, storagePath, true); err != nil {
				slog.ErrorContext(ctx, "StremioCleanup: failed to delete meta+nzb", "path", storagePath, "error", err)
			}
		}
	} else {
		// No storage path recorded — just delete the temp NZB if it still exists
		if err := os.Remove(item.NzbPath); err != nil && !os.IsNotExist(err) {
			slog.ErrorContext(ctx, "StremioCleanup: failed to delete temp NZB", "path", item.NzbPath, "error", err)
		}
	}

	// Remove queue DB entry regardless of file deletion outcome
	if err := s.queueRepo.RemoveFromQueue(ctx, item.ID); err != nil {
		slog.ErrorContext(ctx, "StremioCleanup: failed to remove queue item", "id", item.ID, "error", err)
	}
}
