package api

import (
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
)


func TestToSABnzbdHistorySlot(t *testing.T) {
	t.Run("basic path assignment", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:      1,
			NzbPath: "/config/.nzbs/movies/MovieName.nzb",
			Status:  database.QueueStatusCompleted,
		}

		// The path logic has moved to calculateHistoryStoragePath, so ToSABnzbdHistorySlot
		// just needs to properly assign the finalPath passed into it.
		finalPath := "/mnt/downloads/movies/MovieName"

		slot := ToSABnzbdHistorySlot(item, 0, finalPath)

		assert.Equal(t, finalPath, slot.Path)
		assert.Equal(t, finalPath, slot.Storage)
		assert.Equal(t, "MovieName", slot.Name)
	})

	t.Run("fallback extraction without storagepath", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:      1,
			NzbPath: "/config/.nzbs/movies/MovieName.nzb",
			Status:  database.QueueStatusCompleted,
		}
		finalPath := "/mnt/downloads/"

		slot := ToSABnzbdHistorySlot(item, 0, finalPath)

		assert.Equal(t, finalPath, slot.Path)
		assert.Equal(t, "MovieName", slot.Name)
	})
}

func TestMarkHistorySlotMissing(t *testing.T) {
	t.Run("overrides Completed slot to Failed with reason", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:      42,
			NzbPath: "/config/.nzbs/movies/MovieName.nzb",
			Status:  database.QueueStatusCompleted,
		}
		missingPath := "/mnt/symlink-farm/movies/MovieName"

		slot := ToSABnzbdHistorySlot(item, 0, missingPath)
		// Sanity check: before marking, status reflects QueueStatusCompleted.
		assert.Equal(t, "Completed", slot.Status)
		assert.Equal(t, "Finished", slot.ActionLine)

		markHistorySlotMissing(&slot, missingPath)

		assert.Equal(t, "Failed", slot.Status)
		assert.Equal(t, "Failed: reported path missing on disk", slot.ActionLine)
		assert.Contains(t, slot.Fail_message, missingPath)
		assert.Equal(t, int64(0), slot.Downloaded)
	})

	t.Run("preserves pre-existing fail_message", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:           7,
			NzbPath:      "/config/.nzbs/x.nzb",
			Status:       database.QueueStatusFailed,
			ErrorMessage: strPtr("original error"),
		}
		slot := ToSABnzbdHistorySlot(item, 0, "/missing/path")
		assert.Equal(t, "original error", slot.Fail_message)

		markHistorySlotMissing(&slot, "/missing/path")

		assert.Equal(t, "Failed", slot.Status)
		assert.Equal(t, "original error", slot.Fail_message,
			"existing fail_message should be preserved")
	})

	t.Run("nil slot is a no-op", func(t *testing.T) {
		// Should not panic.
		markHistorySlotMissing(nil, "/anything")
	})
}

func TestCalculateHistoryStoragePath(t *testing.T) {
	cfg := &config.Config{
		MountPath: "/mnt/altmount",
		SABnzbd: config.SABnzbdConfig{
			CompleteDir: "complete",
			Categories: []config.SABnzbdCategory{
				{
					Name: "movies",
					Dir:  "movies/1080p",
				},
				{
					Name: "movies-4k",
					Dir:  "movies/2160p",
				},
			},
		},
		Import: config.ImportConfig{
			ImportStrategy: config.ImportStrategySYMLINK,
			ImportDir:      strPtr("/movies-library"),
		},
	}

	server := &Server{
		configManager: &mockConfigManager{cfg: cfg},
	}

	t.Run("category with mapped dir that has overlapping name", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:          42,
			Category:    strPtr("movies"),
			StoragePath: strPtr("/complete/movies/1080p/ReleaseName/movie.mkv"),
			Status:      database.QueueStatusCompleted,
		}

		path, exists := server.calculateHistoryStoragePath(item, "/movies-library")
		assert.Equal(t, "/movies-library/complete/movies/1080p/ReleaseName/movie.mkv", path)
		assert.True(t, exists)
	})

	t.Run("category-4k mapped dir resolves correctly without duplication", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:          43,
			Category:    strPtr("movies-4k"),
			StoragePath: strPtr("/complete/movies/2160p/ReleaseName/movie.mkv"),
			Status:      database.QueueStatusCompleted,
		}

		path, exists := server.calculateHistoryStoragePath(item, "/movies-library")
		assert.Equal(t, "/movies-library/complete/movies/2160p/ReleaseName/movie.mkv", path)
		assert.True(t, exists)
	})
}

func strPtr(s string) *string { return &s }
