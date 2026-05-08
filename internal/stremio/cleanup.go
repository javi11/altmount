package stremio

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
// along with their associated .meta files, storage directory, and persistent NZB files.
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

func (s *StremioCleanupService) cleanupExpired(ctx context.Context) {
	cfg := s.configGetter()
	ttlHours := cfg.Stremio.NzbTTLHours
	if ttlHours <= 0 {
		return
	}

	items, err := s.queueRepo.GetExpiredStremioQueueItems(ctx, ttlHours)
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
			// Multi-file: remove the entire metadata subtree (including nested .meta files)
			if err := s.metadataService.DeleteDirectory(storagePath); err != nil {
				slog.ErrorContext(ctx, "StremioCleanup: failed to delete storage directory",
					"path", storagePath, "error", err)
			}
			// Remove the persistent NZB file (one source NZB shared by all files in the dir)
			if err := os.Remove(item.NzbPath); err != nil && !os.IsNotExist(err) {
				slog.ErrorContext(ctx, "StremioCleanup: failed to delete persistent NZB",
					"path", item.NzbPath, "error", err)
			}
		} else {
			// Single file: delete .meta + source NZB together
			if err := s.metadataService.DeleteFileMetadataWithSourceNzb(ctx, storagePath, true); err != nil {
				slog.ErrorContext(ctx, "StremioCleanup: failed to delete meta+nzb",
					"path", storagePath, "error", err)
			}
		}
	} else {
		// No storage path recorded — just delete the persistent NZB if it still exists
		if err := os.Remove(item.NzbPath); err != nil && !os.IsNotExist(err) {
			slog.ErrorContext(ctx, "StremioCleanup: failed to delete persistent NZB",
				"path", item.NzbPath, "error", err)
		}
	}

	// Best-effort: prune the per-id parent folder under .nzbs (e.g. /config/.nzbs/Movies/123/)
	// once it is empty. os.Remove only succeeds on empty directories, so this is safe.
	if parent := filepath.Dir(item.NzbPath); parent != "" && parent != "." && parent != "/" {
		_ = os.Remove(parent)
	}

	// Remove queue DB entry regardless of file deletion outcome
	if err := s.queueRepo.RemoveFromQueue(ctx, item.ID); err != nil {
		slog.ErrorContext(ctx, "StremioCleanup: failed to remove queue item", "id", item.ID, "error", err)
	}
}
