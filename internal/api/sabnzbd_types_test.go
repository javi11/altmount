package api

import (
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestToSABnzbdHistorySlot(t *testing.T) {
	t.Run("basic path assignment", func(t *testing.T) {
		item := &database.ImportQueueItem{
			ID:          1,
			NzbPath:     "/config/.nzbs/movies/MovieName.nzb",
			Status:      database.QueueStatusCompleted,
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
			ID:          1,
			NzbPath:     "/config/.nzbs/movies/MovieName.nzb",
			Status:      database.QueueStatusCompleted,
		}
		finalPath := "/mnt/downloads/"
		
		slot := ToSABnzbdHistorySlot(item, 0, finalPath)
		
		assert.Equal(t, finalPath, slot.Path)
		assert.Equal(t, "MovieName", slot.Name)
	})
}
