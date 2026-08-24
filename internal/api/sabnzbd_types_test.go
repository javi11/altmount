package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
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

func TestValidateSABnzbdCategory(t *testing.T) {
	cfg := &config.Config{
		SABnzbd: config.SABnzbdConfig{
			Categories: []config.SABnzbdCategory{
				{Name: "Movies"},
				{Name: "TV"},
				{Name: "Music"},
				{Name: "Books"},
			},
		},
	}

	server := &Server{
		configManager: &mockConfigManager{cfg: cfg},
	}

	t.Run("empty string maps to default category", func(t *testing.T) {
		cat, err := server.validateSABnzbdCategory("")
		assert.NoError(t, err)
		assert.Equal(t, config.DefaultCategoryName, cat)
	})

	t.Run("asterisk maps to default category", func(t *testing.T) {
		cat, err := server.validateSABnzbdCategory("*")
		assert.NoError(t, err)
		assert.Equal(t, config.DefaultCategoryName, cat)
	})

	t.Run("default case insensitive maps to default category", func(t *testing.T) {
		cat, err := server.validateSABnzbdCategory("dEfAuLt")
		assert.NoError(t, err)
		assert.Equal(t, config.DefaultCategoryName, cat)
	})

	t.Run("case insensitive match for music", func(t *testing.T) {
		cat, err := server.validateSABnzbdCategory("music")
		assert.NoError(t, err)
		assert.Equal(t, "Music", cat)

		catUpper, err := server.validateSABnzbdCategory("MUSIC")
		assert.NoError(t, err)
		assert.Equal(t, "Music", catUpper)
	})

	t.Run("invalid category returns error", func(t *testing.T) {
		_, err := server.validateSABnzbdCategory("invalid_cat")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid category 'invalid_cat'")
	})

	t.Run("no configured categories allows any category", func(t *testing.T) {
		emptyServer := &Server{
			configManager: &mockConfigManager{cfg: &config.Config{}},
		}
		cat, err := emptyServer.validateSABnzbdCategory("custom_category")
		assert.NoError(t, err)
		assert.Equal(t, "custom_category", cat)
	})
}

func TestHandleSABnzbdGetCats(t *testing.T) {
	app := fiber.New()
	keyOverride := "12345678901234567890123456789012"
	sabnzbdEnabled := true

	cfg := &config.Config{
		API: config.APIConfig{
			KeyOverride: keyOverride,
		},
		SABnzbd: config.SABnzbdConfig{
			Enabled: &sabnzbdEnabled,
			Categories: []config.SABnzbdCategory{
				{Name: "Movies"},
				{Name: "TV"},
				{Name: "Music"},
				{Name: "Books"},
				{Name: "Adult"},
			},
		},
	}

	server := &Server{
		configManager: &mockConfigManager{cfg: cfg},
	}

	app.Get("/api", server.handleSABnzbd)

	t.Run("mode=get_cats returns expected category list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api?mode=get_cats&output=json&apikey="+keyOverride, nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		var result SABnzbdCategoriesResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		assert.NoError(t, err)
		assert.Equal(t, []string{"*", "Movies", "TV", "Music", "Books", "Adult", "Default"}, result.Categories)
	})

	t.Run("mode=fullstatus behaves like mode=status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api?mode=fullstatus&output=json&apikey="+keyOverride, nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		var result SABnzbdStatusResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		assert.NoError(t, err)
		assert.True(t, result.Status)
		assert.Equal(t, "4.5.0", result.Version)
	})
}

func strPtr(s string) *string { return &s }
