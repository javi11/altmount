package api

import (
	"testing"

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

func strPtr(s string) *string { return &s }
