package api

import (
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestToSABnzbdHistorySlot(t *testing.T) {
	t.Run("relative storage path", func(t *testing.T) {
		storagePath := "/movies/MovieName/Movie.mkv"
		item := &database.ImportQueueItem{
			ID:          1,
			StoragePath: &storagePath,
			NzbPath:     "/config/.nzbs/movies/MovieName.nzb",
			Status:      database.QueueStatusCompleted,
		}
		basePath := "/mnt/usenet"
		
		slot := ToSABnzbdHistorySlot(item, 0, basePath)
		
		expectedPath := filepath.ToSlash(filepath.Join("/mnt/usenet", "movies/MovieName"))
		assert.Equal(t, expectedPath, slot.Path)
	})

	t.Run("absolute storage path with same prefix as basePath", func(t *testing.T) {
		storagePath := "/mnt/usenet/complete/movies/MovieName/Movie.mkv"
		item := &database.ImportQueueItem{
			ID:          1,
			StoragePath: &storagePath,
			NzbPath:     "/config/.nzbs/movies/MovieName.nzb",
			Status:      database.QueueStatusCompleted,
		}
		basePath := "/mnt/usenet"
		
		slot := ToSABnzbdHistorySlot(item, 0, basePath)
		
		// Currently this fails and returns /mnt/usenet/mnt/usenet/complete/movies/MovieName
		// We want it to be /mnt/usenet/complete/movies/MovieName
		expectedPath := "/mnt/usenet/complete/movies/MovieName"
		assert.Equal(t, expectedPath, slot.Path)
	})

	t.Run("absolute storage path with /complete prefix", func(t *testing.T) {
		storagePath := "/complete/movies/MovieName/Movie.mkv"
		item := &database.ImportQueueItem{
			ID:          1,
			StoragePath: &storagePath,
			NzbPath:     "/config/.nzbs/movies/MovieName.nzb",
			Status:      database.QueueStatusCompleted,
		}
		basePath := "/mnt/usenet"
		
		slot := ToSABnzbdHistorySlot(item, 0, basePath)
		
		expectedPath := "/mnt/usenet/complete/movies/MovieName"
		assert.Equal(t, expectedPath, slot.Path)
	})
}
